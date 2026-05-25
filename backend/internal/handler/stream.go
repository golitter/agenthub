package handler

import (
	"context"
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
	// Phase 1: Send MySQL history as text events
	if msg.Content != "" {
		chunks := splitContent(msg.Content, 500)
		for _, chunk := range chunks {
			fmt.Fprintf(c.Writer, "%s\n\n", stream.FormatSSE(chunk))
			c.Writer.Flush()
		}
	}

	// Phase 2: Continue from Redis Stream
	rdb := pkgredis.GetClient()
	streamKey := pkgredis.StreamKey(msg.SessionID, msg.MessageID)
	ctx := c.Request.Context()

	// Start after last_seq
	lastID := msg.LastSeq
	if lastID == "" {
		lastID = "0"
	}

	// First drain any pending messages (non-blocking)
	for {
		results, err := rdb.XRead(ctx, &redis.XReadArgs{
			Streams: []string{streamKey, lastID},
			Count:   100,
			Block:   0,
		}).Result()
		if err != nil || len(results) == 0 || len(results[0].Messages) == 0 {
			break
		}
		for _, xmsg := range results[0].Messages {
			if data, ok := xmsg.Values["data"].(string); ok {
				fmt.Fprintf(c.Writer, "%s\n\n", data)
				c.Writer.Flush()
			}
			lastID = xmsg.ID
		}
	}

	// Then block for real-time events until stream ends
	for {
		// Check if the goroutine is still running
		if !stream.IsActive(msg.MessageID) {
			// Re-fetch message status
			var fresh model.Message
			if err := db.GetDB().Where("message_id = ?", msg.MessageID).First(&fresh).Error; err == nil {
				switch fresh.Status {
				case "completed":
					// Send any remaining content diff
					if fresh.Content != "" && fresh.Content != msg.Content {
						remaining := fresh.Content[len(msg.Content):]
						if remaining != "" {
							chunks := splitContent(remaining, 500)
							for _, chunk := range chunks {
								fmt.Fprintf(c.Writer, "%s\n\n", stream.FormatSSE(chunk))
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
				default:
					// Status is "streaming" but writer not registered yet —
					// the goroutine is still connecting to the agent service.
					// Fall through to the XRead block and wait.
				}
			} else {
				c.Writer.Flush()
				return
			}
		}

		results, err := rdb.XRead(ctx, &redis.XReadArgs{
			Streams: []string{streamKey, lastID},
			Count:   1,
			Block:   5 * time.Second,
		}).Result()
		if err != nil {
			if err == context.Canceled || err == context.DeadlineExceeded {
				return
			}
			continue
		}
		if len(results) == 0 || len(results[0].Messages) == 0 {
			continue
		}
		for _, xmsg := range results[0].Messages {
			if data, ok := xmsg.Values["data"].(string); ok {
				fmt.Fprintf(c.Writer, "%s\n\n", data)
				c.Writer.Flush()
			}
			lastID = xmsg.ID
		}
	}
}

func (h *StreamHandler) serveCompleted(c *gin.Context, msg *model.Message) {
	if msg.Content != "" {
		chunks := splitContent(msg.Content, 500)
		for _, chunk := range chunks {
			fmt.Fprintf(c.Writer, "%s\n\n", stream.FormatSSE(chunk))
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
			fmt.Fprintf(c.Writer, "%s\n\n", stream.FormatSSE(chunk))
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
