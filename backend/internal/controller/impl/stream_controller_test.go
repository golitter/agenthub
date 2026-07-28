package impl

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"agenthub/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func TestDelayedSSEWriterSetsHeadersOnlyOnWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	writer := &delayedSSEWriter{c: c}

	if got := recorder.Header().Get("Content-Type"); got != "" {
		t.Fatalf("Content-Type before write = %q, want empty", got)
	}

	if _, err := writer.Write([]byte(": connected\n\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if got := recorder.Header().Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering = %q, want no", got)
	}
}

func TestServeStreamDoesNotWriteJSONAfterSSEStarted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	ctrl := &StreamController{service: lateErrorStreamService{}}
	router.GET("/api/tasks/:taskId/stream", ctrl.ServeStream)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/task-1/stream?session_id=session-1&message_id=message-1", nil)
	resp := httptest.NewRecorder()

	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	if got := resp.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if body := resp.Body.String(); body != "retry: 1000\n: connected\n\n" {
		t.Fatalf("body = %q, want SSE content only", body)
	}
}

type lateErrorStreamService struct{}

func (lateErrorStreamService) ServeStream(ctx context.Context, sessionID, messageID string, writer io.Writer, flusher http.Flusher) error {
	if _, err := writer.Write([]byte("retry: 1000\n: connected\n\n")); err != nil {
		return err
	}
	flusher.Flush()
	return service.ErrInternal("late stream error")
}
