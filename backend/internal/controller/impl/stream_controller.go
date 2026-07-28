package impl

import (
	"log/slog"
	"net/http"

	"agenthub/backend/internal/service"

	"github.com/gin-gonic/gin"
)

type StreamController struct {
	service service.StreamService
}

func NewStreamController(streamService service.StreamService) *StreamController {
	return &StreamController{service: streamService}
}

func (ctrl *StreamController) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/tasks/:taskId/stream", ctrl.ServeStream)
}

func (ctrl *StreamController) ServeStream(c *gin.Context) {
	if c.Query("message_id") == "" || c.Query("session_id") == "" {
		handleBizError(c, service.ErrBadRequest("session_id and message_id are required"))
		return
	}

	writer := &delayedSSEWriter{c: c}
	if err := ctrl.service.ServeStream(c.Request.Context(), c.Query("session_id"), c.Query("message_id"), writer, writer); err != nil {
		if writer.started {
			slog.Warn("stream ended with error after response started", "task_id", c.Param("taskId"), "session_id", c.Query("session_id"), "message_id", c.Query("message_id"), "error", err)
			return
		}
		handleBizError(c, err)
	}
}

type delayedSSEWriter struct {
	c       *gin.Context
	started bool
}

func (w *delayedSSEWriter) Write(data []byte) (int, error) {
	w.start()
	return w.c.Writer.Write(data)
}

func (w *delayedSSEWriter) Flush() {
	w.start()
	w.c.Writer.Flush()
}

func (w *delayedSSEWriter) start() {
	if w.started {
		return
	}
	w.started = true
	w.c.Header("Content-Type", "text/event-stream")
	w.c.Header("Cache-Control", "no-cache")
	w.c.Header("Connection", "keep-alive")
	w.c.Header("X-Accel-Buffering", "no")
	w.c.Writer.WriteHeader(http.StatusOK)
}
