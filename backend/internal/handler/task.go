package handler

import (
	"bufio"
	"errors"
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
	"gorm.io/gorm"
)

type TaskHandler struct {
	agentClient *agentend_client.Client
}

func NewTaskHandler(agentClient *agentend_client.Client) *TaskHandler {
	return &TaskHandler{agentClient: agentClient}
}

type CreateTaskReq struct {
	Title    string `json:"title" binding:"required"`
	RepoPath string `json:"repo_path"`
}

func (h *TaskHandler) CreateTask(c *gin.Context) {
	var req CreateTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "title is required")
		return
	}
	t := model.Task{
		TaskID:   uuid.New().String(),
		Title:    req.Title,
		RepoPath: req.RepoPath,
		Status:   "active",
	}
	if err := db.GetDB().Create(&t).Error; err != nil {
		vo.InternalError(c, err.Error())
		return
	}
	vo.Created(c, t)
}

func (h *TaskHandler) ListTasks(c *gin.Context) {
	var tasks []model.Task
	if err := db.GetDB().Order("created_at DESC").Find(&tasks).Error; err != nil {
		vo.InternalError(c, err.Error())
		return
	}
	vo.OK(c, tasks)
}

func (h *TaskHandler) GetTask(c *gin.Context) {
	var t model.Task
	taskID := c.Param("taskId")
	if err := db.GetDB().Where("task_id = ?", taskID).First(&t).Error; err != nil {
		vo.NotFound(c, "task not found")
		return
	}
	var sessions []model.Session
	db.GetDB().Where("task_id = ?", taskID).Find(&sessions)
	vo.OK(c, gin.H{
		"task":     t,
		"sessions": sessions,
	})
}

func (h *TaskHandler) DeleteTask(c *gin.Context) {
	result := db.GetDB().Where("task_id = ?", c.Param("taskId")).Delete(&model.Task{})
	if result.RowsAffected == 0 {
		vo.NotFound(c, "task not found")
		return
	}
	vo.OK(c, nil)
}

type RunTaskReq struct {
	Message   string `json:"message" binding:"required"`
	AgentType string `json:"agent_type"`
	SessionID string `json:"session_id" binding:"required"`
}

func (h *TaskHandler) RunTask(c *gin.Context) {
	taskID := c.Param("taskId")

	var task model.Task
	if err := db.GetDB().Where("task_id = ?", taskID).First(&task).Error; err != nil {
		vo.NotFound(c, "task not found")
		return
	}

	var req RunTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "message and session_id are required")
		return
	}

	agentType := req.AgentType
	if agentType == "" {
		agentType = "claude-code"
	}

	var session model.Session
	err := db.GetDB().Where("session_id = ? AND task_id = ?", req.SessionID, taskID).First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		session = model.Session{
			SessionID: req.SessionID,
			TaskID:    taskID,
			AgentType: agentType,
			Status:    "running",
		}
		if err := db.GetDB().Create(&session).Error; err != nil {
			vo.InternalError(c, err.Error())
			return
		}
	} else if err != nil {
		vo.InternalError(c, err.Error())
		return
	} else {
		db.GetDB().Model(&session).Update("status", "running")
	}

	agentReq := &generated.AgentRequest{
		TaskId:    taskID,
		SessionId: req.SessionID,
		Message:   req.Message,
		AgentType: generated.AgentType(agentType),
		Stream:    true,
	}
	if task.RepoPath != "" {
		agentReq.RepoPath = &task.RepoPath
	}

	resp, err := h.agentClient.StreamAgent(agentReq)
	if err != nil {
		db.GetDB().Model(&session).Update("status", "failed")
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
		slog.Warn("SSE stream error", "task_id", taskID, "session_id", req.SessionID, "error", err)
		finalStatus = "failed"
	}
	db.GetDB().Model(&model.Session{}).Where("session_id = ? AND task_id = ?", req.SessionID, taskID).Update("status", finalStatus)
}
