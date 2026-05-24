package handler

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

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

	var contentBuilder strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(c.Writer, "%s\n", line)
		c.Writer.(http.Flusher).Flush()

		// Accumulate agent text content from SSE data lines
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var event generated.StreamEvent
			if err := json.Unmarshal([]byte(data), &event); err == nil {
				if event.Type == generated.EventTypeText {
					if text, ok := event.Content["text"].(string); ok {
						contentBuilder.WriteString(text)
					}
				}
			}
		}
	}

	finalStatus := "completed"
	streamErr := scanner.Err()
	if streamErr != nil {
		slog.Warn("SSE stream error", "task_id", taskID, "session_id", req.SessionID, "error", streamErr)
		finalStatus = "failed"
	}
	db.GetDB().Model(&model.Session{}).Where("session_id = ? AND task_id = ?", req.SessionID, taskID).Update("status", finalStatus)

	// Save agent message (full or partial) to Message table
	agentContent := contentBuilder.String()
	if agentContent != "" {
		agentMsg := model.Message{
			TaskID:    taskID,
			SessionID: req.SessionID,
			Role:      "agent",
			Content:   agentContent,
			AgentType: agentType,
		}
		if err := db.GetDB().Create(&agentMsg).Error; err != nil {
			slog.Warn("failed to save agent message", "task_id", taskID, "error", err)
		}
	}
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
