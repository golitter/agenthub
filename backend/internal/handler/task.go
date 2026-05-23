package handler

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/http"

	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/vo"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/db"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TaskHandler struct {
	agentClient *agentend_client.Client
}

func NewTaskHandler(agentClient *agentend_client.Client) *TaskHandler {
	return &TaskHandler{agentClient: agentClient}
}

type RunTaskReq struct {
	Message   string `json:"message" binding:"required"`
	AgentType string `json:"agent_type"`
}

func (h *TaskHandler) RunTask(c *gin.Context) {
	sessionID := c.Param("sid")

	var session model.Session
	if err := db.GetDB().Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		vo.NotFound(c, "session not found")
		return
	}

	var req RunTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "message is required")
		return
	}

	agentType := req.AgentType
	if agentType == "" {
		agentType = "claude-code"
	}

	taskID := uuid.New().String()
	task := model.Task{
		TaskID:    taskID,
		SessionID: sessionID,
		AgentType: agentType,
		Status:    "running",
		Message:   req.Message,
	}
	if err := db.GetDB().Create(&task).Error; err != nil {
		vo.InternalError(c, err.Error())
		return
	}

	agentReq := &generated.AgentRequest{
		TaskId:    taskID,
		SessionId: sessionID,
		Message:   req.Message,
		AgentType: generated.AgentType(agentType),
		Stream:    true,
	}

	resp, err := h.agentClient.StreamAgent(agentReq)
	if err != nil {
		db.GetDB().Model(&task).Update("status", "failed")
		vo.InternalError(c, fmt.Sprintf("agent stream error: %v", err))
		return
	}
	defer resp.Body.Close()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	c.Writer.WriteHeader(http.StatusOK)

	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(c.Writer, "%s\n", line)
		c.Writer.(http.Flusher).Flush()
	}

	finalStatus := "completed"
	if err := scanner.Err(); err != nil {
		slog.Warn("SSE stream error", "task_id", taskID, "error", err)
		finalStatus = "failed"
	}
	db.GetDB().Model(&model.Task{}).Where("task_id = ?", taskID).Updates(map[string]interface{}{
		"status": finalStatus,
	})
}
