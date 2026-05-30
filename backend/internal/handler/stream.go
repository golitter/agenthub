package handler

import (
	"fmt"
	"net/http"
	"time"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/stream"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/db"
	pkgredis "agenthub/backend/pkg/redis"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type StreamHandler struct{}

func NewStreamHandler() *StreamHandler {
	return &StreamHandler{}
}

func (h *StreamHandler) ServeStream(c *gin.Context) {
	sessionID := c.Query("session_id")
	messageID := c.Query("message_id")
	if messageID == "" || sessionID == "" {
		vo.BadRequest(c, "session_id and message_id are required")
		return
	}

	var msg model.Message
	if err := db.GetDB().Where("message_id = ?", messageID).First(&msg).Error; err != nil {
		vo.NotFound(c, "message not found")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
	c.Writer.Flush()

	switch msg.Status {
	case "streaming":
		h.serveStreaming(c, &msg)
	case "completed":
		h.serveCompleted(c, &msg)
	case "failed":
		h.serveFailed(c, &msg)
	default:
		h.serveCompleted(c, &msg)
	}
}

func (h *StreamHandler) serveStreaming(c *gin.Context, msg *model.Message) {
	streamKey := pkgredis.StreamKey(msg.SessionID, msg.MessageID)
	ctx := c.Request.Context()

	// Phase 1: Send MySQL history as text events with agent metadata
	if msg.Content != "" {
		chunks := splitContent(msg.Content, 500)
		for _, chunk := range chunks {
			fmt.Fprintf(c.Writer, "%s\n\n", stream.FormatSSEWithMeta(chunk, msg.AgentType, msg.AgentName, msg.MessageID))
			c.Writer.Flush()
		}
	}

	// Phase 2: Subscribe hub and replay Redis gap
	ch, _ := stream.Hub.Subscribe(streamKey)

	// Replay Redis gap: events between last_seq and hub's currentSeq
	rdb := pkgredis.GetClient()
	if rdb != nil {
		lastID := msg.LastSeq
		if lastID == "" {
			lastID = "0"
		}
		// Non-blocking drain of any Redis events since last_seq
		for {
			results, err := rdb.XRead(ctx, &redis.XReadArgs{
				Streams: []string{streamKey, lastID},
				Count:   100,
				Block:   -1, // non-blocking: -1 omits BLOCK arg entirely
			}).Result()
			if err != nil || len(results) == 0 || len(results[0].Messages) == 0 {
				break
			}
			for _, xmsg := range results[0].Messages {
				if data, ok := xmsg.Values["data"].(string); ok {
					fmt.Fprintf(c.Writer, "%s\n\n", data)
				}
				lastID = xmsg.ID
			}
			c.Writer.Flush()
		}
	}

	// Phase 3: Consume realtime events from hub channel with heartbeat
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	stale := time.NewTimer(10 * time.Second)
	defer stale.Stop()

	for {
		select {
		case evt, ok := <-ch:
			heartbeat.Reset(15 * time.Second)
			stale.Reset(10 * time.Second)
			if !ok || evt.Done {
				fmt.Fprintf(c.Writer, "data: {\"type\":\"done\"}\n\n")
				c.Writer.Flush()
				return
			}
			fmt.Fprintf(c.Writer, "%s\n\n", evt.Data)
			c.Writer.Flush()

		case <-ctx.Done():
			return

		case <-heartbeat.C:
			// Heartbeat: send lightweight SSE event to keep connection alive
			fmt.Fprintf(c.Writer, "data: {\"type\":\"heartbeat\"}\n\n")
			c.Writer.Flush()

		case <-stale.C:
			// Staleness check: stream inactive for 10s — verify still alive
			if !stream.IsActive(msg.MessageID) {
				var fresh model.Message
				if err := db.GetDB().Where("message_id = ?", msg.MessageID).First(&fresh).Error; err == nil {
					switch fresh.Status {
					case "completed":
						if fresh.Content != "" && fresh.Content != msg.Content {
							remaining := fresh.Content[len(msg.Content):]
							if remaining != "" {
								chunks := splitContent(remaining, 500)
								for _, chunk := range chunks {
									fmt.Fprintf(c.Writer, "%s\n\n", stream.FormatSSEWithMeta(chunk, fresh.AgentType, fresh.AgentName, fresh.MessageID))
									c.Writer.Flush()
								}
							}
						}
						fmt.Fprintf(c.Writer, "data: {\"type\":\"done\"}\n\n")
						c.Writer.Flush()
						return
					case "failed":
						fmt.Fprintf(c.Writer, "data: {\"type\":\"error\",\"content\":{\"message\":\"stream failed\"}}\n\n")
						c.Writer.Flush()
						return
					}
				}
				// Stream not active but status not terminal — reset stale timer
				stale.Reset(10 * time.Second)
			} else {
				stale.Reset(10 * time.Second)
			}
		}
	}
}

func (h *StreamHandler) serveCompleted(c *gin.Context, msg *model.Message) {
	if msg.Content != "" {
		chunks := splitContent(msg.Content, 500)
		for _, chunk := range chunks {
			fmt.Fprintf(c.Writer, "%s\n\n", stream.FormatSSEWithMeta(chunk, msg.AgentType, msg.AgentName, msg.MessageID))
			c.Writer.Flush()
		}
	}
	fmt.Fprintf(c.Writer, "data: {\"type\":\"done\"}\n\n")
	c.Writer.Flush()
}

func (h *StreamHandler) serveFailed(c *gin.Context, msg *model.Message) {
	if msg.Content != "" {
		chunks := splitContent(msg.Content, 500)
		for _, chunk := range chunks {
			fmt.Fprintf(c.Writer, "%s\n\n", stream.FormatSSEWithMeta(chunk, msg.AgentType, msg.AgentName, msg.MessageID))
			c.Writer.Flush()
		}
	}
	fmt.Fprintf(c.Writer, "data: {\"type\":\"error\",\"content\":{\"message\":\"stream failed\"}}\n\n")
	c.Writer.Flush()
}

// splitContent splits text into chunks of approximately maxLen characters,
// trying to break at newline boundaries when possible.
func splitContent(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		end := maxLen
		if end > len(text) {
			end = len(text)
		}
		// Try to break at a newline
		if end < len(text) {
			if idx := lastIndexByte(text[:end], '\n'); idx > end/2 {
				end = idx + 1
			}
		}
		chunks = append(chunks, text[:end])
		text = text[end:]
	}
	return chunks
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
