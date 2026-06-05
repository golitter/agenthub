package impl

import (
	"context"
	"fmt"
	"io"
	"net/http"
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

func NewStreamService(messageDao dao.MessageDao) *StreamService {
	return &StreamService{messageDao: messageDao}
}

func (svc *StreamService) ServeStream(ctx context.Context, sessionID, messageID string, writer io.Writer, flusher http.Flusher) error {
	if messageID == "" || sessionID == "" {
		return service.ErrBadRequest("session_id and message_id are required")
	}

	message, err := svc.messageDao.FindByMessageID(messageID)
	if err != nil {
		return err
	}
	if message == nil {
		return service.ErrNotFound("message not found")
	}

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

func (svc *StreamService) serveStreaming(ctx context.Context, writer io.Writer, flusher http.Flusher, message *model.Message) error {
	streamKey := pkgredis.StreamKey(message.SessionID, message.MessageID)

	if message.Content != "" {
		chunks := splitContent(message.Content, 500)
		for _, chunk := range chunks {
			fmt.Fprintf(writer, "%s\n\n", stream.FormatSSEWithMeta(chunk, message.AgentType, message.AgentName, message.MessageID, message.GroupID))
			flusher.Flush()
		}
	}

	ch, _ := stream.Hub.Subscribe(streamKey)
	if ch != nil {
		defer stream.Hub.Unsubscribe(streamKey, ch)
	}

	rdb := pkgredis.GetClient()
	if rdb != nil {
		lastID := message.LastSeq
		if lastID == "" {
			lastID = "0"
		}
		for {
			results, err := rdb.XRead(ctx, &redis.XReadArgs{
				Streams: []string{streamKey, lastID},
				Count:   100,
				Block:   -1,
			}).Result()
			if err != nil || len(results) == 0 || len(results[0].Messages) == 0 {
				break
			}
			for _, xmsg := range results[0].Messages {
				if data, ok := xmsg.Values["data"].(string); ok {
					fmt.Fprintf(writer, "%s\n\n", data)
				}
				lastID = xmsg.ID
			}
			flusher.Flush()
		}
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
				fresh, err := svc.messageDao.FindByMessageID(message.MessageID)
				if err == nil && fresh != nil {
					switch fresh.Status {
					case "completed":
						if fresh.Content != "" && fresh.Content != message.Content {
							remaining := fresh.Content[len(message.Content):]
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
