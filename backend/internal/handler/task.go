package handler

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/stream"
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

type AgentConfig struct {
	Type string `json:"type" binding:"required"`
	Name string `json:"name"`
}

type CreateTaskReq struct {
	Title    string        `json:"title" binding:"required"`
	RepoPath string        `json:"repo_path"`
	Agents   []AgentConfig `json:"agents"`
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

	for _, agent := range req.Agents {
		s := model.Session{
			SessionID: uuid.New().String(),
			TaskID:    t.TaskID,
			AgentType: agent.Type,
			AgentName: agent.Name,
			Status:    "active",
		}
		if err := db.GetDB().Create(&s).Error; err != nil {
			slog.Warn("failed to create session", "task_id", t.TaskID, "agent_type", agent.Type, "error", err)
		}
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

	// Save user message to Message table
	userMsg := model.Message{
		TaskID:    taskID,
		SessionID: req.SessionID,
		Role:      "user",
		Content:   req.Message,
	}
	if err := db.GetDB().Create(&userMsg).Error; err != nil {
		slog.Warn("failed to save user message", "task_id", taskID, "error", err)
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

	// Create agent message with streaming status
	messageID := uuid.New().String()
	agentMsg := model.Message{
		MessageID: messageID,
		TaskID:    taskID,
		SessionID: req.SessionID,
		Role:      "agent",
		Content:   "",
		Status:    "streaming",
		AgentType: agentType,
	}
	if err := db.GetDB().Create(&agentMsg).Error; err != nil {
		vo.InternalError(c, fmt.Sprintf("create agent message: %v", err))
		return
	}

	// Launch background goroutine to consume agentend stream
	go func() {
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
			slog.Warn("agent stream error", "task_id", taskID, "session_id", req.SessionID, "error", err)
			stream.PublishErrorAndFail(messageID, req.SessionID, fmt.Sprintf("agent service error: %v", err))
			db.GetDB().Model(&model.Session{}).Where("session_id = ? AND task_id = ?", req.SessionID, taskID).Update("status", "failed")
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			slog.Warn("agent returned non-200", "task_id", taskID, "status", resp.StatusCode)
			stream.PublishErrorAndFail(messageID, req.SessionID, fmt.Sprintf("agent returned HTTP %d", resp.StatusCode))
			db.GetDB().Model(&model.Session{}).Where("session_id = ? AND task_id = ?", req.SessionID, taskID).Update("status", "failed")
			return
		}

		sw := stream.NewStreamWriter(context.Background(), taskID, req.SessionID, messageID, agentType)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		sw.Run(func(fn func(string)) {
			for scanner.Scan() {
				fn(scanner.Text())
			}
			if scanner.Err() != nil {
				slog.Warn("SSE scanner error", "task_id", taskID, "error", scanner.Err())
				sw.Fail()
				db.GetDB().Model(&model.Session{}).Where("session_id = ? AND task_id = ?", req.SessionID, taskID).Update("status", "failed")
				return
			}
			db.GetDB().Model(&model.Session{}).Where("session_id = ? AND task_id = ?", req.SessionID, taskID).Update("status", "completed")
		})
	}()

	c.JSON(http.StatusAccepted, gin.H{
		"data": gin.H{
			"message_id": messageID,
			"status":     "streaming",
		},
	})
}

// ValidateRepoPath forwards the validation request to agentend.
type ValidateRepoPathReq struct {
	RepoPath string `json:"repo_path" binding:"required"`
}

func (h *TaskHandler) ValidateRepoPath(c *gin.Context) {
	var req ValidateRepoPathReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "repo_path is required")
		return
	}

	result, err := h.agentClient.ValidateRepoPath(req.RepoPath)
	if err != nil {
		vo.ServiceUnavailable(c, "agent service unavailable")
		return
	}
	vo.OK(c, result)
}
