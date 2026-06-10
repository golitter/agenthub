package impl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/generated"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/internal/stream"
	"agenthub/backend/pkg/agentend_client"

	"github.com/google/uuid"
)

type TaskService struct {
	taskDao     dao.TaskDao
	sessionDao  dao.SessionDao
	messageDao  dao.MessageDao
	diffDao     dao.DiffSnapshotDao
	agentClient *agentend_client.Client
}

func NewTaskService(taskDao dao.TaskDao, sessionDao dao.SessionDao, messageDao dao.MessageDao, diffDao dao.DiffSnapshotDao, agentClient *agentend_client.Client) *TaskService {
	return &TaskService{
		taskDao:     taskDao,
		sessionDao:  sessionDao,
		messageDao:  messageDao,
		diffDao:     diffDao,
		agentClient: agentClient,
	}
}

func (svc *TaskService) CreateTask(input service.CreateTaskInput) (*model.Task, error) {
	hasOrchestrator := false
	hasNonOrchestrator := false
	for _, agent := range input.Agents {
		if agent.Type == "orchestrator" {
			hasOrchestrator = true
		} else {
			hasNonOrchestrator = true
		}
	}
	if hasOrchestrator && !hasNonOrchestrator {
		return nil, service.ErrBadRequest("orchestrator cannot be the only agent in a task")
	}

	task := &model.Task{
		TaskID:   uuid.New().String(),
		Title:    input.Title,
		RepoPath: input.RepoPath,
		Status:   "active",
	}

	sessions := make([]model.Session, 0, len(input.Agents))
	sessionAgents := make([]model.SessionAgent, 0, len(input.Agents))
	for _, agent := range input.Agents {
		sessionID := uuid.New().String()
		sessions = append(sessions, model.Session{
			SessionID: sessionID,
			TaskID:    task.TaskID,
			AgentType: agent.Type,
			AgentName: agent.Name,
			Status:    "active",
		})
		sessionAgents = append(sessionAgents, model.SessionAgent{
			SessionID: sessionID,
			AgentType: agent.Type,
			AgentName: agent.Name,
		})
	}

	if err := svc.taskDao.CreateTaskWithSessions(task, sessions, sessionAgents); err != nil {
		return nil, service.ErrInternal("failed to create task")
	}
	return task, nil
}

func (svc *TaskService) ListTasks() ([]model.Task, error) {
	return svc.taskDao.ListTasks()
}

func (svc *TaskService) GetTask(taskID string) (*service.TaskDetailResponse, error) {
	task, err := svc.taskDao.GetByTaskID(taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, service.ErrNotFound("task not found")
	}

	sessions, err := svc.sessionDao.ListByTaskID(taskID)
	if err != nil {
		return nil, err
	}

	sessionIDs := make([]string, 0, len(sessions))
	for _, sessionModel := range sessions {
		sessionIDs = append(sessionIDs, sessionModel.SessionID)
	}
	agents, err := svc.taskDao.ListSessionAgentsBySessionIDs(sessionIDs)
	if err != nil {
		return nil, err
	}

	agentMap := make(map[string]model.SessionAgent, len(agents))
	for _, agent := range agents {
		agentMap[agent.SessionID] = agent
	}

	enrichedSessions := make([]model.Session, 0, len(sessions))
	for _, sessionModel := range sessions {
		if agent, ok := agentMap[sessionModel.SessionID]; ok {
			if agent.AgentType != "" {
				sessionModel.AgentType = agent.AgentType
			}
			if sessionModel.AgentName == "" {
				sessionModel.AgentName = agent.AgentName
			}
			if sessionModel.AvatarURL == "" {
				sessionModel.AvatarURL = agent.AvatarURL
			}
		}
		enrichedSessions = append(enrichedSessions, sessionModel)
	}

	routeAgents := buildRouteAgents(enrichedSessions)
	routeMap := make(map[string]routeAgent, len(routeAgents))
	for _, routeAgent := range routeAgents {
		routeMap[routeAgent.SessionID] = routeAgent
	}

	result := make([]service.TaskSessionWithAgent, 0, len(enrichedSessions))
	for _, sessionModel := range enrichedSessions {
		item := service.TaskSessionWithAgent{
			Session:   sessionModel,
			AgentType: sessionModel.AgentType,
			AgentName: sessionModel.AgentName,
		}
		if route, ok := routeMap[sessionModel.SessionID]; ok {
			item.RouteID = route.RouteID
			item.MentionLabel = route.MentionLabel
			item.Aliases = route.Aliases
		}
		if sessionModel.AvatarURL != "" {
			item.AvatarURL = sessionModel.AvatarURL
		} else if agent, ok := agentMap[sessionModel.SessionID]; ok {
			item.AvatarURL = agent.AvatarURL
		}
		result = append(result, item)
	}

	return &service.TaskDetailResponse{
		Task:     *task,
		Sessions: result,
	}, nil
}

func (svc *TaskService) DeleteTask(taskID string) error {
	deleted, err := svc.taskDao.DeleteTaskCascade(taskID)
	if err != nil {
		return service.ErrInternal("failed to delete task")
	}
	if !deleted {
		return service.ErrNotFound("task not found")
	}
	return nil
}

func (svc *TaskService) LeaveTask(taskID string) error {
	task, sessionIDs, err := svc.taskDao.GetTaskAndSessionIDs(taskID)
	if err != nil {
		return service.ErrInternal("failed to leave task")
	}
	if task == nil {
		return service.ErrNotFound("task not found")
	}

	for _, sessionID := range sessionIDs {
		if err := svc.agentClient.DestroySession(sessionID); err != nil {
			slog.Warn("destroy session failed (best-effort)", "session_id", sessionID, "error", err)
		}
	}
	if err := svc.agentClient.CleanupByTask(taskID); err != nil {
		slog.Warn("cleanup task workspaces failed (best-effort)", "task_id", taskID, "error", err)
	}
	if task.RepoPath != "" {
		if err := svc.agentClient.CleanupTaskBranches(taskID, task.RepoPath); err != nil {
			slog.Warn("force cleanup task branches failed (best-effort)", "task_id", taskID, "error", err)
		}
	}

	deleted, err := svc.taskDao.DeleteTaskCascade(taskID)
	if err != nil {
		return service.ErrInternal("failed to leave task")
	}
	if !deleted {
		return service.ErrNotFound("task not found")
	}

	slog.Info("task left and cleaned up", "task_id", taskID, "sessions_cleaned", len(sessionIDs))
	return nil
}

func (svc *TaskService) PatchTask(taskID string, input service.PatchTaskInput) error {
	updates := map[string]interface{}{}
	if input.PinnedAt != nil {
		if *input.PinnedAt == "" {
			updates["pinned_at"] = nil
		} else {
			t, err := time.Parse(time.RFC3339, *input.PinnedAt)
			if err != nil {
				return service.ErrBadRequest("invalid pinned_at format, expected RFC3339")
			}
			updates["pinned_at"] = &t
		}
	} else {
		// null → clear pin
		updates["pinned_at"] = nil
	}
	if len(updates) == 0 {
		return service.ErrBadRequest("no fields to update")
	}

	updated, err := svc.taskDao.PatchTask(taskID, updates)
	if err != nil {
		return err
	}
	if !updated {
		return service.ErrNotFound("task not found")
	}
	return nil
}

func (svc *TaskService) RunTask(taskID string, input service.RunTaskInput) (*service.RunTaskResult, error) {
	task, err := svc.taskDao.GetByTaskID(taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, service.ErrNotFound("task not found")
	}

	agentType := input.AgentType
	if agentType == "" {
		agentType = "claude-code"
	}

	sessions, err := svc.sessionDao.ListByTaskID(taskID)
	if err != nil {
		return nil, err
	}
	route, err := resolveMessageRoute(input, sessions)
	if err != nil {
		return nil, service.ErrBadRequest(err.Error())
	}
	input.SessionID = route.SessionID
	input.AgentType = route.AgentType
	input.Message = route.AgentMessage
	agentType = route.AgentType

	if !input.SkipUserMessage {
		if err := svc.messageDao.CreateMessage(model.Message{
			MessageID: uuid.New().String(),
			TaskID:    taskID,
			SessionID: input.SessionID,
			Role:      "user",
			Content:   route.DisplayMessage,
		}); err != nil {
			return nil, service.ErrInternal("failed to save user message")
		}
	}

	_, created, err := svc.taskDao.EnsureSession(input.SessionID, taskID, agentType)
	if err != nil {
		return nil, service.ErrInternal("failed to create session")
	}
	if created {
		if err := svc.taskDao.CreateSessionAgent(model.SessionAgent{
			SessionID: input.SessionID,
			AgentType: agentType,
		}); err != nil {
			return nil, service.ErrInternal("failed to create session")
		}
	}

	agentName := route.AgentName
	if agentName == "" {
		sessionModel, err := svc.sessionDao.GetBySessionID(input.SessionID)
		if err == nil && sessionModel != nil {
			agentName = sessionModel.AgentName
		}
	}

	messageID := uuid.New().String()
	if err := svc.messageDao.CreateMessage(model.Message{
		MessageID: messageID,
		TaskID:    taskID,
		SessionID: input.SessionID,
		Role:      "agent",
		Content:   "",
		Status:    "streaming",
		AgentType: agentType,
		AgentName: agentName,
	}); err != nil {
		return nil, service.ErrInternal("failed to create agent message")
	}

	agentReq := svc.buildAgentRequest(task, input, messageID, agentType, agentName)
	go svc.runStream(agentReq, taskID, input.SessionID, messageID)

	return &service.RunTaskResult{
		MessageID: messageID,
		Status:    "streaming",
		SessionID: input.SessionID,
		AgentType: agentType,
		AgentName: agentName,
		RouteID:   route.RouteID,
		RouteMode: route.Mode,
	}, nil
}

func (svc *TaskService) ReviewTask(taskID string, input service.ReviewTaskInput) (map[string]interface{}, error) {
	if input.Action != "approve" && input.Action != "discuss" && input.Action != "modify" {
		return nil, service.ErrBadRequest("action must be approve, discuss, or modify")
	}
	if (input.Action == "discuss" || input.Action == "modify") && strings.TrimSpace(input.Content) == "" {
		return nil, service.ErrBadRequest("content is required for discuss or modify")
	}

	sessionModel, err := svc.sessionDao.GetByTaskAndSessionID(taskID, input.SessionID)
	if err != nil {
		return nil, err
	}
	if sessionModel == nil {
		return nil, service.ErrNotFound("session not found")
	}

	result, err := svc.agentClient.ReviewAgent(agentend_client.ReviewRequest{
		SessionID: input.SessionID,
		Action:    input.Action,
		Content:   input.Content,
	})
	if err != nil {
		if strings.Contains(err.Error(), "status 404") {
			return nil, service.ErrConflict("no pending plan review for this session")
		}
		return nil, service.ErrServiceUnavailable(err.Error())
	}

	status := "submitted"
	if input.Action == "approve" {
		status = "approved"
	}
	svc.markLatestPlanReviewBlock(taskID, input.SessionID, status)
	_ = svc.sessionDao.UpdateStatusByTask(input.SessionID, taskID, "running")
	return result, nil
}

func (svc *TaskService) FetchGroupChatWindow(taskID, sessionID string) []map[string]interface{} {
	return fetchGroupChatWindow(svc.messageDao, taskID, sessionID)
}

func (svc *TaskService) buildAgentRequest(task *model.Task, input service.RunTaskInput, messageID, agentType, agentName string) *generated.AgentRequest {
	agentReq := &generated.AgentRequest{
		TaskId:            task.TaskID,
		SessionId:         input.SessionID,
		Message:           input.Message,
		AgentType:         generated.AgentType(agentType),
		Stream:            true,
		GroupChatMessages: svc.FetchGroupChatWindow(task.TaskID, input.SessionID),
	}

	if input.Cwd != "" {
		agentReq.WorkspacePath = &input.Cwd
	} else if task.RepoPath != "" {
		agentReq.RepoPath = &task.RepoPath
	}

	if agentType != "orchestrator" {
		soulMD := ""
		if sessionModel, err := svc.sessionDao.GetBySessionID(input.SessionID); err == nil && sessionModel != nil {
			soulMD = sessionModel.SoulMD
		}
		config := map[string]interface{}{"soul_md": soulMD}
		configIface := interface{}(config)
		agentReq.Config = &configIface
	}

	if agentType == "orchestrator" {
		svc.injectOrchestratorConfig(agentReq, task, input, agentType, agentName)
	}
	return agentReq
}

func (svc *TaskService) injectOrchestratorConfig(agentReq *generated.AgentRequest, task *model.Task, input service.RunTaskInput, agentType, agentName string) {
	sessions, _ := svc.sessionDao.ListByTaskID(task.TaskID)
	siblings := make([]model.Session, 0, len(sessions))
	for _, sessionModel := range sessions {
		if sessionModel.AgentType != "orchestrator" {
			siblings = append(siblings, sessionModel)
		}
	}

	orchestratorID := agentName
	if orchestratorID == "" {
		orchestratorID = "orchestrator"
	}
	orchestratorSoul := ""
	if sessionModel, err := svc.sessionDao.GetBySessionID(input.SessionID); err == nil && sessionModel != nil {
		orchestratorSoul = sessionModel.SoulMD
	}

	var agents []map[string]interface{}
	for _, agent := range buildRouteAgents(siblings) {
		agents = append(agents, map[string]interface{}{
			"id":         agent.RouteID,
			"type":       agent.AgentType,
			"session_id": agent.SessionID,
			"name":       agent.MentionLabel,
		})
	}
	config := map[string]interface{}{
		"agents":  agents,
		"task_id": task.TaskID,
		"soul_md": orchestratorSoul,
		"orchestrator": map[string]interface{}{
			"id":         orchestratorID,
			"type":       agentType,
			"session_id": input.SessionID,
			"name":       orchestratorID,
		},
	}
	if task.RepoPath != "" {
		repoPath := task.RepoPath
		if absRepoPath, err := filepath.Abs(task.RepoPath); err == nil {
			repoPath = absRepoPath
		}
		config["repo_path"] = repoPath
		config["shared_dir"] = filepath.Join(filepath.Dir(repoPath), "worktrees", task.TaskID, "shared", ".agent")
	}
	configIface := interface{}(config)
	agentReq.Config = &configIface
}

func (svc *TaskService) runStream(agentReq *generated.AgentRequest, taskID, sessionID, messageID string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in stream goroutine", "task_id", taskID, "session_id", sessionID, "panic", r)
			stream.PublishErrorAndFail(svc.messageDao, messageID, sessionID, fmt.Sprintf("internal error: %v", r))
			_ = svc.sessionDao.UpdateStatusByTask(sessionID, taskID, "failed")
		}
	}()

	resp, err := svc.agentClient.StreamAgent(agentReq)
	if err != nil {
		slog.Warn("agent stream error", "task_id", taskID, "session_id", sessionID, "error", err)
		stream.PublishErrorAndFail(svc.messageDao, messageID, sessionID, fmt.Sprintf("agent service error: %v", err))
		_ = svc.sessionDao.UpdateStatusByTask(sessionID, taskID, "failed")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("agent returned non-200", "task_id", taskID, "status", resp.StatusCode)
		stream.PublishErrorAndFail(svc.messageDao, messageID, sessionID, fmt.Sprintf("agent returned HTTP %d", resp.StatusCode))
		_ = svc.sessionDao.UpdateStatusByTask(sessionID, taskID, "failed")
		return
	}

	sw := stream.NewStreamWriter(context.Background(), taskID, sessionID, messageID, string(agentReq.AgentType), svc.messageDao, svc.sessionDao, svc.diffDao)

	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	outcome := sw.Run(func(fn func(string)) error {
		for {
			line, readErr := reader.ReadString('\n')
			if len(line) > 0 {
				if len(line) > 10*1024*1024 {
					return fmt.Errorf("SSE line exceeds 10MB")
				}
				line = strings.TrimRight(line, "\r\n")
				if line != "" && !strings.HasPrefix(line, "event:") {
					fn(line)
				}
			}
			if readErr != nil {
				if errors.Is(readErr, io.EOF) {
					return nil
				}
				slog.Warn("SSE reader error", "task_id", taskID, "error", readErr)
				return readErr
			}
		}
	})

	switch outcome {
	case stream.RunOutcomeFailed:
		_ = svc.sessionDao.UpdateStatusByTask(sessionID, taskID, "failed")
	default:
		_ = svc.sessionDao.UpdateStatusByTask(sessionID, taskID, "completed")
	}
}

func (svc *TaskService) markLatestPlanReviewBlock(taskID, sessionID, status string) {
	message, err := svc.messageDao.FindLatestPlanReviewMessage(taskID, sessionID)
	if err != nil || message == nil {
		return
	}

	updated := strings.Replace(message.Content, `"status":"pending"`, fmt.Sprintf(`"status":"%s"`, status), 1)
	if updated == message.Content {
		return
	}
	if err := svc.messageDao.UpdateContent(message.MessageID, updated); err != nil {
		slog.Warn("failed to mark plan review block", "message_id", message.MessageID, "error", err)
	}
}
