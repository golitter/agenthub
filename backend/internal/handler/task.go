package handler

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

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

	// Validate: orchestrator must not be the only agent
	hasOrchestrator := false
	hasNonOrchestrator := false
	for _, a := range req.Agents {
		if a.Type == "orchestrator" {
			hasOrchestrator = true
		} else {
			hasNonOrchestrator = true
		}
	}
	if hasOrchestrator && !hasNonOrchestrator {
		vo.BadRequest(c, "orchestrator cannot be the only agent in a task")
		return
	}

	var t model.Task
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		t = model.Task{
			TaskID:   uuid.New().String(),
			Title:    req.Title,
			RepoPath: req.RepoPath,
			Status:   "active",
		}
		if err := tx.Create(&t).Error; err != nil {
			return err
		}

		for _, agent := range req.Agents {
			sid := uuid.New().String()
			s := model.Session{
				SessionID: sid,
				TaskID:    t.TaskID,
				AgentType: agent.Type,
				AgentName: agent.Name,
				Status:    "active",
			}
			sa := model.SessionAgent{
				SessionID: sid,
				AgentType: agent.Type,
				AgentName: agent.Name,
			}
			if err := tx.Create(&s).Error; err != nil {
				return err
			}
			if err := tx.Create(&sa).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		vo.InternalError(c, "failed to create task")
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

	// Fetch session_agents for all sessions
	sessionIDs := make([]string, 0, len(sessions))
	for _, s := range sessions {
		sessionIDs = append(sessionIDs, s.SessionID)
	}

	var agents []model.SessionAgent
	if len(sessionIDs) > 0 {
		db.GetDB().Where("session_id IN ?", sessionIDs).Find(&agents)
	}
	agentMap := make(map[string]model.SessionAgent)
	for _, a := range agents {
		agentMap[a.SessionID] = a
	}

	type SessionWithAgent struct {
		model.Session
		AgentType string `json:"agent_type"`
		AgentName string `json:"agent_name"`
		AvatarURL string `json:"avatar_url,omitempty"`
	}
	var result []SessionWithAgent
	for _, s := range sessions {
		swa := SessionWithAgent{Session: s}
		if a, ok := agentMap[s.SessionID]; ok {
			swa.AgentType = a.AgentType
			swa.AgentName = a.AgentName
		}
		// Prefer avatar from sessions table (updated by UpdateSession)
		if s.AvatarURL != "" {
			swa.AvatarURL = s.AvatarURL
		} else if a, ok := agentMap[s.SessionID]; ok {
			swa.AvatarURL = a.AvatarURL
		}
		result = append(result, swa)
	}

	vo.OK(c, gin.H{
		"task":     t,
		"sessions": result,
	})
}

func (h *TaskHandler) DeleteTask(c *gin.Context) {
	taskID := c.Param("taskId")

	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		result := tx.Where("task_id = ?", taskID).Delete(&model.Task{})
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		var sessionIDs []string
		tx.Model(&model.Session{}).Where("task_id = ?", taskID).Pluck("session_id", &sessionIDs)

		if len(sessionIDs) > 0 {
			tx.Where("session_id IN ?", sessionIDs).Delete(&model.Message{})
			tx.Where("session_id IN ?", sessionIDs).Delete(&model.SessionAgent{})
		}
		tx.Where("task_id = ?", taskID).Delete(&model.Session{})
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			vo.NotFound(c, "task not found")
			return
		}
		vo.InternalError(c, "failed to delete task")
		return
	}
	vo.OK(c, nil)
}

type PatchTaskReq struct {
	PinnedAt *string `json:"pinned_at"`
}

func (h *TaskHandler) PatchTask(c *gin.Context) {
	taskID := c.Param("taskId")

	var req PatchTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		vo.BadRequest(c, "invalid request body")
		return
	}

	updates := map[string]interface{}{}
	if req.PinnedAt != nil {
		if *req.PinnedAt == "" {
			updates["pinned_at"] = nil
		} else {
			updates["pinned_at"] = *req.PinnedAt
		}
	}

	if len(updates) == 0 {
		vo.BadRequest(c, "no fields to update")
		return
	}

	result := db.GetDB().Model(&model.Task{}).Where("task_id = ?", taskID).Updates(updates)
	if result.RowsAffected == 0 {
		vo.NotFound(c, "task not found")
		return
	}

	vo.OK(c, gin.H{"task_id": taskID})
}

type RunTaskReq struct {
	Message         string `json:"message" binding:"required"`
	AgentType       string `json:"agent_type"`
	SessionID       string `json:"session_id" binding:"required"`
	Cwd             string `json:"cwd"`
	SkipUserMessage bool   `json:"skip_user_message"`
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

	// Save user message to Message table (skip for internal orchestrator dispatches)
	if !req.SkipUserMessage {
		userMsg := model.Message{
			MessageID: uuid.New().String(),
			TaskID:    taskID,
			SessionID: req.SessionID,
			Role:      "user",
			Content:   req.Message,
		}
		if err := db.GetDB().Create(&userMsg).Error; err != nil {
			vo.InternalError(c, "failed to save user message")
			return
		}
	}

	var session model.Session
	result := db.GetDB().Where(model.Session{SessionID: req.SessionID, TaskID: taskID}).
		Attrs(model.Session{AgentType: agentType, Status: "running"}).
		FirstOrCreate(&session)
	if result.Error != nil {
		vo.InternalError(c, result.Error.Error())
		return
	}
	if result.RowsAffected > 0 {
		db.GetDB().Create(&model.SessionAgent{
			SessionID: req.SessionID,
			AgentType: agentType,
		})
	} else {
		db.GetDB().Model(&session).Update("status", "running")
	}

	// Create agent message with streaming status
	// Look up agent name from session
	var agentSession model.Session
	agentName := ""
	if err := db.GetDB().Where("session_id = ?", req.SessionID).First(&agentSession).Error; err == nil {
		agentName = agentSession.AgentName
	}

	messageID := uuid.New().String()
	agentMsg := model.Message{
		MessageID: messageID,
		TaskID:    taskID,
		SessionID: req.SessionID,
		Role:      "agent",
		Content:   "",
		Status:    "streaming",
		AgentType: agentType,
		AgentName: agentName,
	}
	if err := db.GetDB().Create(&agentMsg).Error; err != nil {
		vo.InternalError(c, fmt.Sprintf("create agent message: %v", err))
		return
	}

	// Launch background goroutine to consume agentend stream
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic in stream goroutine", "task_id", taskID, "session_id", req.SessionID, "panic", r)
				stream.PublishErrorAndFail(messageID, req.SessionID, fmt.Sprintf("internal error: %v", r))
				db.GetDB().Model(&model.Session{}).Where("session_id = ? AND task_id = ?", req.SessionID, taskID).Update("status", "failed")
			}
		}()

		agentReq := &generated.AgentRequest{
			TaskId:            taskID,
			SessionId:         req.SessionID,
			Message:           req.Message,
			AgentType:         generated.AgentType(agentType),
			Stream:            true,
			GroupChatMessages: fetchGroupChatWindow(taskID, req.SessionID),
		}
		if req.Cwd != "" {
			agentReq.WorkspacePath = &req.Cwd
		} else if task.RepoPath != "" {
			agentReq.RepoPath = &task.RepoPath
		}

		// Inject soul_md for non-orchestrator agents into config
		if agentType != "orchestrator" {
			soulMD := ""
			db.GetDB().Model(&model.Session{}).Where("session_id = ?", req.SessionID).Pluck("soul_md", &soulMD)
			soulConfig := map[string]interface{}{"soul_md": soulMD}
			soulConfigIface := interface{}(soulConfig)
			agentReq.Config = &soulConfigIface
		}

		// For orchestrator, inject agents config from sibling sessions
		if agentType == "orchestrator" {
			var siblings []model.Session
			db.GetDB().Where("task_id = ? AND agent_type != ?", taskID, "orchestrator").Find(&siblings)
			orchestratorID := agentName
			if orchestratorID == "" {
				orchestratorID = "orchestrator"
			}
			orchestratorSoul := ""
			db.GetDB().Model(&model.Session{}).Where("session_id = ?", req.SessionID).Pluck("soul_md", &orchestratorSoul)
			var agents []map[string]interface{}
			for _, s := range siblings {
				agentID := s.AgentName
				if agentID == "" {
					agentID = s.AgentType
				}
				agents = append(agents, map[string]interface{}{
					"id":         agentID,
					"type":       s.AgentType,
					"session_id": s.SessionID,
					"name":       agentID,
				})
			}
			config := map[string]interface{}{
				"agents":  agents,
				"task_id": taskID,
				"soul_md": orchestratorSoul,
				"orchestrator": map[string]interface{}{
					"id":         orchestratorID,
					"type":       agentType,
					"session_id": req.SessionID,
					"name":       orchestratorID,
				},
			}
			if task.RepoPath != "" {
				repoPath := task.RepoPath
				if absRepoPath, err := filepath.Abs(task.RepoPath); err == nil {
					repoPath = absRepoPath
				}
				config["repo_path"] = repoPath
				config["shared_dir"] = filepath.Join(filepath.Dir(repoPath), "worktrees", taskID, "shared", ".agent")
			}
			configIface := interface{}(config)
			agentReq.Config = &configIface
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
				line := scanner.Text()
				// Skip SSE event type lines and blank separators —
				// agentend (sse_starlette) sends "event: <type>\ndata: <json>\n\n"
				// per event. Only forward data lines to Redis so each SSE event
				// stays atomic and doesn't get split into separate events.
				if line == "" || strings.HasPrefix(line, "event:") {
					continue
				}
				fn(line)
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

	vo.Accepted(c, gin.H{
		"message_id": messageID,
		"status":     "streaming",
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

// maxGroupChatMsgLen is the maximum number of runes per group chat window message.
const maxGroupChatMsgLen = 2000

// fetchGroupChatWindow queries messages from other sessions since this session's
// last agent message. Used to provide cross-agent context in group chats.
// On error, returns an empty slice (graceful degradation).
func fetchGroupChatWindow(taskID, sessionID string) []map[string]interface{} {
	// 1. Find the timestamp of the last COMPLETED agent message from this session.
	//    Exclude "streaming" to avoid picking up the message just created by RunTask.
	var lastMsg model.Message
	err := db.GetDB().
		Where("task_id = ? AND session_id = ? AND role = ? AND status = ?", taskID, sessionID, "agent", "completed").
		Order("created_at DESC").Limit(1).First(&lastMsg).Error

	// 2. Query window: other sessions' completed/streaming messages after T
	query := db.GetDB().
		Where("task_id = ? AND session_id != ?", taskID, sessionID).
		Where("status IN ?", []string{"completed", "streaming"})
	if err == nil {
		// Found a previous agent message — only include messages after it
		query = query.Where("created_at > ?", lastMsg.CreatedAt)
	}

	var messages []model.Message
	if err := query.Order("created_at ASC").Order("id ASC").Find(&messages).Error; err != nil {
		slog.Warn("group chat window query failed, degrading to empty", "task_id", taskID, "session_id", sessionID, "error", err)
		return []map[string]interface{}{}
	}

	// 3. Convert to map list with truncation
	result := make([]map[string]interface{}, 0, len(messages))
	slog.Info("group chat window query",
		"task_id", taskID,
		"session_id", sessionID,
		"messages_found", len(messages),
	)
	seen := make(map[string]bool, len(messages))
	for _, m := range messages {
		content := m.Content
		if utf8.RuneCountInString(content) > maxGroupChatMsgLen {
			runes := []rune(content)
			content = string(runes[:maxGroupChatMsgLen]) + "\n...[截断]"
		}
		dedupeKey := m.AgentName + "\x00" + m.AgentType + "\x00" + content
		if seen[dedupeKey] {
			continue
		}
		seen[dedupeKey] = true
		result = append(result, map[string]interface{}{
			"role":       m.Role,
			"agent_name": m.AgentName,
			"content":    content,
		})
	}
	return result
}
