package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"
	pkgredis "agenthub/backend/pkg/redis"

	"github.com/redis/go-redis/v9"
)

const (
	flushInterval    = 2 * time.Second
	flushThreshold   = 500
	maxStreamLen     = 10000
	streamExpireTTL  = 600 * time.Second
	goroutineTimeout = 30 * time.Minute
)

// Registry tracks active StreamWriter goroutines by messageID.
var registry sync.Map

// IsActive returns true if a goroutine is running for the given messageID.
func IsActive(messageID string) bool {
	_, ok := registry.Load(messageID)
	return ok
}

// StreamWriter consumes an agentend SSE stream, publishes events to Redis Stream,
// and batch-flushes text content to MySQL.
type StreamWriter struct {
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	messageID string
	sessionID string
	taskID    string
	agentType string
	streamKey string

	buf       strings.Builder
	bufLen    int
	lastSeq   string
	lastFlush time.Time
	mu        sync.Mutex
}

// NewStreamWriter creates a new StreamWriter and registers it.
func NewStreamWriter(ctx context.Context, taskID, sessionID, messageID, agentType string) *StreamWriter {
	childCtx, cancel := context.WithTimeout(ctx, goroutineTimeout)
	key := pkgredis.StreamKey(sessionID, messageID)
	sw := &StreamWriter{
		ctx:       childCtx,
		cancel:    cancel,
		messageID: messageID,
		sessionID: sessionID,
		taskID:    taskID,
		agentType: agentType,
		streamKey: key,
	}
	registry.Store(messageID, sw)
	return sw
}

// Run consumes the agentend response body (SSE lines), publishes to Redis, and flushes to MySQL.
// This should be called in a goroutine.
func (sw *StreamWriter) Run(scanFunc func(func(line string))) {
	defer sw.finish()

	sw.wg.Add(1)
	go sw.flushLoop()

	sawError := false

	scanFunc(func(line string) {
		if sw.ctx.Err() != nil {
			return
		}
		// Publish every SSE line to Redis Stream
		sw.publishToRedis(line)

		// Detect error/done events to set final status correctly
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var event generated.StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				switch event.Type {
				case generated.EventTypeText:
					if text, ok := event.Content["text"].(string); ok {
						sw.appendText(text)
					}
				case generated.EventTypeDone:
					// normal end
				case generated.EventTypeError:
					sawError = true
				}
			}
		}
	})

	// Final flush
	sw.doFlush()

	if sawError {
		sw.updateStatus("failed")
	} else {
		sw.updateStatus("completed")
	}
}

func (sw *StreamWriter) publishToRedis(line string) {
	rdb := pkgredis.GetClient()
	if rdb == nil {
		return
	}
	seq, err := rdb.XAdd(sw.ctx, &redis.XAddArgs{
		Stream: sw.streamKey,
		MaxLen: maxStreamLen,
		Approx: true,
		Values: map[string]interface{}{
			"data": line,
		},
	}).Result()
	if err != nil {
		slog.Error("redis XADD failed", "key", sw.streamKey, "error", err)
		return
	}
	sw.mu.Lock()
	sw.lastSeq = seq
	sw.mu.Unlock()
}

func (sw *StreamWriter) appendText(text string) {
	sw.mu.Lock()
	sw.buf.WriteString(text)
	sw.bufLen += len(text)
	shouldFlush := sw.bufLen >= flushThreshold
	sw.mu.Unlock()

	if shouldFlush {
		sw.doFlush()
	}
}

func (sw *StreamWriter) flushLoop() {
	defer sw.wg.Done()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sw.mu.Lock()
			hasContent := sw.bufLen > 0
			sw.mu.Unlock()
			if hasContent {
				sw.doFlush()
			}
		case <-sw.ctx.Done():
			return
		}
	}
}

func (sw *StreamWriter) doFlush() {
	sw.mu.Lock()
	content := sw.buf.String()
	seq := sw.lastSeq
	if content == "" {
		sw.mu.Unlock()
		return
	}
	sw.buf.Reset()
	sw.bufLen = 0
	sw.lastFlush = time.Now()
	sw.mu.Unlock()

	err := db.GetDB().Model(&model.Message{}).
		Where("message_id = ?", sw.messageID).
		Updates(map[string]interface{}{
			"content":  content,
			"last_seq": seq,
		}).Error
	if err != nil {
		slog.Error("flush to MySQL failed", "message_id", sw.messageID, "error", err)
	}
}

func (sw *StreamWriter) updateStatus(status string) error {
	err := db.GetDB().Model(&model.Message{}).
		Where("message_id = ?", sw.messageID).
		Updates(map[string]interface{}{
			"status": status,
		}).Error
	if err != nil {
		slog.Error("update message status failed", "message_id", sw.messageID, "error", err)
	}
	return err
}

func (sw *StreamWriter) finish() {
	sw.cancel()
	sw.wg.Wait()

	// Set Redis EXPIRE on the stream
	rdb := pkgredis.GetClient()
	if rdb != nil {
		if err := rdb.Expire(context.Background(), sw.streamKey, streamExpireTTL).Err(); err != nil {
			slog.Warn("redis EXPIRE failed", "key", sw.streamKey, "error", err)
		}
	}

	registry.Delete(sw.messageID)
}

// Fail marks a StreamWriter's message as failed (e.g., on context cancellation).
func (sw *StreamWriter) Fail() {
	sw.doFlush()
	sw.updateStatus("failed")
}

// PublishErrorAndFail writes an error event to Redis Stream, then marks the message as failed.
// Used when agentend fails before/during streaming so the frontend can see the error.
func PublishErrorAndFail(messageID, sessionID, errMsg string) {
	key := pkgredis.StreamKey(sessionID, messageID)
	rdb := pkgredis.GetClient()
	if rdb != nil {
		event := map[string]interface{}{
			"type": "error",
			"content": map[string]string{
				"message": errMsg,
			},
		}
		data, _ := json.Marshal(event)
		rdb.XAdd(context.Background(), &redis.XAddArgs{
			Stream: key,
			MaxLen: maxStreamLen,
			Approx: true,
			Values: map[string]interface{}{
				"data": fmt.Sprintf("data: %s", string(data)),
			},
		}).Result()
		rdb.Expire(context.Background(), key, streamExpireTTL)
	}
	db.GetDB().Model(&model.Message{}).Where("message_id = ?", messageID).Update("status", "failed")
}

// CleanupStaleMessages marks all streaming messages as failed (called at startup).
func CleanupStaleMessages() {
	result := db.GetDB().Model(&model.Message{}).
		Where("status = ?", "streaming").
		Update("status", "failed")
	if result.RowsAffected > 0 {
		slog.Info("cleaned up stale streaming messages", "count", result.RowsAffected)
	}
}

// formatSSE formats a text chunk as an SSE data line matching the StreamEvent contract.
func FormatSSE(text string) string {
	event := map[string]interface{}{
		"type": "text",
		"content": map[string]string{
			"text": text,
		},
	}
	data, _ := json.Marshal(event)
	return fmt.Sprintf("data: %s", string(data))
}
