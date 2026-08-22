package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/internal/stream"
	pkgredis "agenthub/backend/pkg/redis"

	"github.com/redis/go-redis/v9"
)

type StreamService struct {
	messageDao dao.MessageDao
}

const maxStreamMessageIDLen = 36

func NewStreamService(messageDao dao.MessageDao) *StreamService {
	return &StreamService{messageDao: messageDao}
}

func (svc *StreamService) ServeStream(ctx context.Context, taskID, sessionID, messageID string, writer io.Writer, flusher http.Flusher) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || len([]rune(taskID)) > maxTaskIDLen {
		return service.ErrBadRequest("invalid task_id")
	}
	sessionID, messageID, err := normalizeStreamIDs(sessionID, messageID)
	if err != nil {
		return err
	}

	message, err := findMessageWithContext(ctx, svc.messageDao, messageID)
	if err != nil {
		return err
	}
	if message == nil {
		return service.ErrNotFound("message not found")
	}
	if message.TaskID != taskID || message.SessionID != sessionID {
		return service.ErrNotFound("message not found")
	}

	fmt.Fprint(writer, "retry: 1000\n: connected\n\n")
	flusher.Flush()

	switch message.Status {
	case "streaming":
		return svc.serveStreaming(ctx, writer, flusher, message)
	case "failed":
		svc.serveFailed(writer, flusher, message)
	default:
		svc.serveCompleted(writer, flusher, message)
	}
	return nil
}

func normalizeStreamIDs(sessionID, messageID string) (string, string, error) {
	sessionID = strings.TrimSpace(sessionID)
	messageID = strings.TrimSpace(messageID)
	if messageID == "" || sessionID == "" {
		return sessionID, messageID, service.ErrBadRequest("session_id and message_id are required")
	}
	if len([]rune(sessionID)) > maxSessionIDLen {
		return sessionID, messageID, service.ErrBadRequest("session_id is too long")
	}
	if len([]rune(messageID)) > maxStreamMessageIDLen {
		return sessionID, messageID, service.ErrBadRequest("message_id is too long")
	}
	return sessionID, messageID, nil
}

func (svc *StreamService) serveStreaming(ctx context.Context, writer io.Writer, flusher http.Flusher, message *model.Message) error {
	streamKey := pkgredis.StreamKey(message.SessionID, message.MessageID)

	if message.Content != "" {
		chunks := splitContent(message.Content, 500)
		for _, chunk := range chunks {
			fmt.Fprintf(writer, "%s\n\n", stream.FormatSSEWithMeta(chunk, message.AgentType, message.AgentName, message.MessageID, message.GroupID))
			flusher.Flush()
		}
	}

	rdb := pkgredis.GetClient()
	if rdb != nil {
		return svc.serveRedisStreaming(ctx, writer, flusher, message, rdb, streamKey)
	}

	// Redis is a required runtime dependency in normal deployments. Keep the
	// in-memory path only for isolated tests and deliberately Redis-less local
	// callers; a connection never consumes both sources.
	ch, _ := stream.Hub.Subscribe(streamKey)
	if ch != nil {
		defer stream.Hub.Unsubscribe(streamKey, ch)
	}

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
				fmt.Fprintf(writer, "data: {\"type\":\"done\"}\n\n")
				flusher.Flush()
				return nil
			}
			fmt.Fprintf(writer, "%s\n\n", evt.Data)
			flusher.Flush()
		case <-ctx.Done():
			return nil
		case <-heartbeat.C:
			fmt.Fprintf(writer, "data: {\"type\":\"heartbeat\"}\n\n")
			flusher.Flush()
		case <-stale.C:
			if !stream.IsActive(message.MessageID) {
				fresh, err := findMessageWithContext(ctx, svc.messageDao, message.MessageID)
				if err == nil && fresh != nil {
					switch fresh.Status {
					case "completed":
						if fresh.Content != "" && fresh.Content != message.Content {
							remaining := fresh.Content
							if strings.HasPrefix(fresh.Content, message.Content) {
								remaining = fresh.Content[len(message.Content):]
							}
							if remaining != "" {
								chunks := splitContent(remaining, 500)
								for _, chunk := range chunks {
									fmt.Fprintf(writer, "%s\n\n", stream.FormatSSEWithMeta(chunk, fresh.AgentType, fresh.AgentName, fresh.MessageID, fresh.GroupID))
									flusher.Flush()
								}
							}
						}
						fmt.Fprintf(writer, "data: {\"type\":\"done\"}\n\n")
						flusher.Flush()
						return nil
					case "failed":
						fmt.Fprintf(writer, "data: {\"type\":\"error\",\"content\":{\"message\":\"stream failed\"}}\n\n")
						flusher.Flush()
						return nil
					}
				}
				stale.Reset(10 * time.Second)
			} else {
				stale.Reset(10 * time.Second)
			}
		}
	}
}

func (svc *StreamService) serveRedisStreaming(
	ctx context.Context,
	writer io.Writer,
	flusher http.Flusher,
	message *model.Message,
	rdb redisStreamReader,
	streamKey string,
) error {
	lastID := message.LastSeq
	if lastID == "" {
		lastID = "0"
	}
	lastHeartbeat := time.Now()

	for {
		results, err := rdb.XRead(ctx, &redis.XReadArgs{
			Streams: []string{streamKey, lastID},
			Count:   100,
			Block:   time.Second,
		}).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read redis stream: %w", err)
		}

		terminalEvent := false
		for _, result := range results {
			for _, xmsg := range result.Messages {
				data, ok := xmsg.Values["data"].(string)
				lastID = xmsg.ID
				if !ok || data == "" {
					continue
				}
				if _, writeErr := fmt.Fprintf(writer, "%s\n\n", data); writeErr != nil {
					return writeErr
				}
				terminalEvent = terminalEvent || isTerminalSSELine(data)
			}
		}
		if len(results) > 0 {
			flusher.Flush()
			lastHeartbeat = time.Now()
		}
		if terminalEvent {
			return nil
		}

		if time.Since(lastHeartbeat) >= 15*time.Second {
			if _, writeErr := fmt.Fprint(writer, "data: {\"type\":\"heartbeat\"}\n\n"); writeErr != nil {
				return writeErr
			}
			flusher.Flush()
			lastHeartbeat = time.Now()
		}

		if errors.Is(err, redis.Nil) && !stream.IsActive(message.MessageID) {
			fresh, lookupErr := findMessageWithContext(ctx, svc.messageDao, message.MessageID)
			if lookupErr != nil {
				return lookupErr
			}
			if fresh == nil {
				return service.ErrNotFound("message not found")
			}
			switch fresh.Status {
			case "completed":
				fmt.Fprint(writer, "data: {\"type\":\"done\"}\n\n")
				flusher.Flush()
				return nil
			case "failed":
				fmt.Fprint(writer, "data: {\"type\":\"error\",\"content\":{\"message\":\"stream failed\"}}\n\n")
				flusher.Flush()
				return nil
			}
		}
	}
}

type redisStreamReader interface {
	XRead(context.Context, *redis.XReadArgs) *redis.XStreamSliceCmd
}

func isTerminalSSELine(line string) bool {
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" {
		return false
	}
	var event struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return false
	}
	return event.Type == "done" || event.Type == "error"
}

func (svc *StreamService) serveCompleted(writer io.Writer, flusher http.Flusher, message *model.Message) {
	if message.Content != "" {
		chunks := splitContent(message.Content, 500)
		for _, chunk := range chunks {
			fmt.Fprintf(writer, "%s\n\n", stream.FormatSSEWithMeta(chunk, message.AgentType, message.AgentName, message.MessageID, message.GroupID))
			flusher.Flush()
		}
	}
	fmt.Fprintf(writer, "data: {\"type\":\"done\"}\n\n")
	flusher.Flush()
}

func (svc *StreamService) serveFailed(writer io.Writer, flusher http.Flusher, message *model.Message) {
	if message.Content != "" {
		chunks := splitContent(message.Content, 500)
		for _, chunk := range chunks {
			fmt.Fprintf(writer, "%s\n\n", stream.FormatSSEWithMeta(chunk, message.AgentType, message.AgentName, message.MessageID, message.GroupID))
			flusher.Flush()
		}
	}
	fmt.Fprintf(writer, "data: {\"type\":\"error\",\"content\":{\"message\":\"stream failed\"}}\n\n")
	flusher.Flush()
}
