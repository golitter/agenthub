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

const (
	maxTaskTitleLen = 200
	maxTaskIDLen    = 36
	maxRepoPathLen  = 512
	maxAgentTypeLen = 64
	maxAgentNameLen = 128
	maxSessionIDLen = 128

	defaultTaskListLimit = 50
	maxTaskListLimit     = 100
	maxRunMessageLen     = 64 * 1024
	maxRunCwdLen         = 1024
	maxReviewContentLen  = 20 * 1024
	agentStreamTimeout   = 30 * time.Minute
)

const (
	sessionStatusIdle           = string(generated.SessionStateIdle)
	sessionStatusRunning        = string(generated.SessionStateRunning)
	sessionStatusAwaitingReview = string(generated.SessionStateAwaitingReview)
	sessionStatusCompleted      = string(generated.SessionStateCompleted)
	sessionStatusError          = string(generated.SessionStateError)
	sessionStatusInactive       = string(generated.SessionStateInactive)
)

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
	normalized, err := normalizeCreateTaskInput(input)
	if err != nil {
		return nil, err
	}

	hasOrchestrator := false
	hasNonOrchestrator := false
	for _, agent := range normalized.Agents {
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
		Title:    normalized.Title,
		RepoPath: normalized.RepoPath,
		Status:   "active",
	}

	sessions := make([]model.Session, 0, len(normalized.Agents))
	sessionAgents := make([]model.SessionAgent, 0, len(normalized.Agents))
	for _, agent := range normalized.Agents {
		sessionID := uuid.New().String()
		sessions = append(sessions, model.Session{
			SessionID: sessionID,
			TaskID:    task.TaskID,
			AgentType: agent.Type,
			AgentName: agent.Name,
			Status:    sessionStatusIdle,
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

func normalizeCreateTaskInput(input service.CreateTaskInput) (service.CreateTaskInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.RepoPath = strings.TrimSpace(input.RepoPath)
	if input.Title == "" {
		return input, service.ErrBadRequest("title is required")
	}
	if len([]rune(input.Title)) > maxTaskTitleLen {
		return input, service.ErrBadRequest("title is too long")
	}
	if len(input.RepoPath) > maxRepoPathLen {
		return input, service.ErrBadRequest("repo_path is too long")
	}
	if len(input.Agents) == 0 {
		return input, service.ErrBadRequest("at least one agent is required")
	}

	for i := range input.Agents {
		input.Agents[i].Type = strings.TrimSpace(input.Agents[i].Type)
		input.Agents[i].Name = strings.TrimSpace(input.Agents[i].Name)
		if input.Agents[i].Type == "" {
			return input, service.ErrBadRequest("agent type is required")
		}
		if len([]rune(input.Agents[i].Type)) > maxAgentTypeLen {
			return input, service.ErrBadRequest("agent type is too long")
		}
		if !isAllowedAgentType(input.Agents[i].Type) {
			return input, service.ErrBadRequest("invalid agent type")
		}
		if len([]rune(input.Agents[i].Name)) > maxAgentNameLen {
			return input, service.ErrBadRequest("agent name is too long")
		}
	}
	return input, nil
}

func (svc *TaskService) ListTasks(options service.TaskListOptions) (*service.TaskListResponse, error) {
	options, err := normalizeTaskListOptions(options)
	if err != nil {
		return nil, err
	}

	tasks, err := svc.taskDao.ListTasks(options.Limit+1, options.Before)
	if err != nil {
		return nil, err
	}

	hasMore := len(tasks) > options.Limit
	if hasMore {
		tasks = tasks[:options.Limit]
	}
	next := ""
	if hasMore && len(tasks) > 0 {
		next = tasks[len(tasks)-1].TaskID
	}
	return &service.TaskListResponse{
		Items:   tasks,
		HasMore: hasMore,
		Next:    next,
	}, nil
}

func normalizeTaskListOptions(options service.TaskListOptions) (service.TaskListOptions, error) {
	options.Before = strings.TrimSpace(options.Before)
	if options.Limit == 0 {
		options.Limit = defaultTaskListLimit
	}
	if options.Limit < 0 {
		return options, service.ErrBadRequest("limit must be positive")
	}
	if options.Limit > maxTaskListLimit {
		return options, service.ErrBadRequest("limit must not exceed 100")
	}
	return options, nil
}

func (svc *TaskService) GetTask(taskID string) (*service.TaskDetailResponse, error) {
	taskID, err := normalizeTaskID(taskID)
	if err != nil {
		return nil, err
	}
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
	taskID, err := normalizeTaskID(taskID)
	if err != nil {
		return err
	}
	task, sessionIDs, err := svc.taskDao.GetTaskAndSessionIDs(taskID)
	if err != nil {
		return service.ErrInternal("failed to delete task")
	}
	if task == nil {
		return service.ErrNotFound("task not found")
	}

	svc.cleanupTaskExternal(task, sessionIDs, "delete")

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
	taskID, err := normalizeTaskID(taskID)
	if err != nil {
		return err
	}
	task, sessionIDs, err := svc.taskDao.GetTaskAndSessionIDs(taskID)
	if err != nil {
		return service.ErrInternal("failed to leave task")
	}
	if task == nil {
		return service.ErrNotFound("task not found")
	}

	svc.cleanupTaskExternal(task, sessionIDs, "leave")

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

func (svc *TaskService) cleanupTaskExternal(task *model.Task, sessionIDs []string, action string) {
	for _, sessionID := range sessionIDs {
		if err := svc.agentClient.DestroySession(sessionID); err != nil {
			slog.Warn("destroy session failed (best-effort)", "action", action, "task_id", task.TaskID, "session_id", sessionID, "error", err)
		}
	}
	if err := svc.agentClient.CleanupByTask(task.TaskID); err != nil {
		slog.Warn("cleanup task workspaces failed (best-effort)", "action", action, "task_id", task.TaskID, "error", err)
	}
	if task.RepoPath != "" {
		if err := svc.agentClient.CleanupTaskBranches(task.TaskID, task.RepoPath); err != nil {
			slog.Warn("force cleanup task branches failed (best-effort)", "action", action, "task_id", task.TaskID, "error", err)
		}
	}
}

func (svc *TaskService) PatchTask(taskID string, input service.PatchTaskInput) error {
	taskID, err := normalizeTaskID(taskID)
	if err != nil {
		return err
	}
	updates := map[string]interface{}{}
	if input.PinnedAtSet {
		if input.PinnedAt == nil || *input.PinnedAt == "" {
			updates["pinned_at"] = nil
		} else {
			t, err := time.Parse(time.RFC3339, *input.PinnedAt)
			if err != nil {
				return service.ErrBadRequest("invalid pinned_at format, expected RFC3339")
			}
			updates["pinned_at"] = &t
		}
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
	taskID, err := normalizeTaskID(taskID)
	if err != nil {
		return nil, err
	}
	input, err = normalizeRunTaskInput(input)
	if err != nil {
		return nil, err
	}

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
	if _, ok := findSessionByID(sessions, input.SessionID); !ok {
		return nil, service.ErrNotFound("session not found")
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
			Role:      string(generated.MessageRoleUser),
			Content:   route.DisplayMessage,
		}); err != nil {
			return nil, service.ErrInternal("failed to save user message")
		}
	}

	if _, ok := findSessionByID(sessions, input.SessionID); !ok {
		return nil, service.ErrNotFound("session not found")
	}
	if err := svc.sessionDao.UpdateStatusByTask(input.SessionID, taskID, sessionStatusRunning); err != nil {
		return nil, service.ErrInternal("failed to update session status")
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
		Role:      string(generated.MessageRoleAgent),
		Content:   "",
		Status:    string(generated.MessageStatusStreaming),
		AgentType: agentType,
		AgentName: agentName,
	}); err != nil {
		return nil, service.ErrInternal("failed to create agent message")
	}

	agentReq := svc.buildAgentRequest(task, input, messageID, agentType, agentName)
	go svc.runStream(agentReq, taskID, input.SessionID, messageID)

	return &service.RunTaskResult{
		MessageID: messageID,
		Status:    string(generated.MessageStatusStreaming),
		SessionID: input.SessionID,
		AgentType: agentType,
		AgentName: agentName,
		RouteID:   route.RouteID,
		RouteMode: route.Mode,
	}, nil
}

func (svc *TaskService) ReviewTask(taskID string, input service.ReviewTaskInput) (map[string]interface{}, error) {
	taskID, err := normalizeTaskID(taskID)
	if err != nil {
		return nil, err
	}
	input, err = normalizeReviewTaskInput(input)
	if err != nil {
		return nil, err
	}
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
	if sessionModel.Status != sessionStatusAwaitingReview {
		return nil, service.ErrConflict("session is not awaiting review")
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
		slog.Warn("agent plan review failed", "task_id", taskID, "session_id", input.SessionID, "error", err)
		return nil, service.ErrServiceUnavailable("plan review service unavailable")
	}

	status := "submitted"
	if input.Action == "approve" {
		status = "approved"
	}
	svc.markLatestPlanReviewBlock(taskID, input.SessionID, status)
	if err := svc.sessionDao.UpdateStatusByTask(input.SessionID, taskID, sessionStatusRunning); err != nil {
		slog.Warn("failed to mark session running after review", "task_id", taskID, "session_id", input.SessionID, "error", err)
	}
	return result, nil
}

func normalizeTaskID(taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", service.ErrBadRequest("task_id is required")
	}
	if len([]rune(taskID)) > maxTaskIDLen {
		return "", service.ErrBadRequest("task_id is too long")
	}
	return taskID, nil
}

func normalizeRunTaskInput(input service.RunTaskInput) (service.RunTaskInput, error) {
	input.Message = strings.TrimSpace(input.Message)
	input.AgentType = strings.TrimSpace(input.AgentType)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Cwd = strings.TrimSpace(input.Cwd)

	if input.Message == "" {
		return input, service.ErrBadRequest("message is required")
	}
	if len(input.Message) > maxRunMessageLen {
		return input, service.ErrBadRequest("message is too long")
	}
	if input.SessionID == "" {
		return input, service.ErrBadRequest("session_id is required")
	}
	if len([]rune(input.SessionID)) > maxSessionIDLen {
		return input, service.ErrBadRequest("session_id is too long")
	}
	if input.AgentType != "" && !isAllowedAgentType(input.AgentType) {
		return input, service.ErrBadRequest("invalid agent type")
	}
	if len(input.Cwd) > maxRunCwdLen {
		return input, service.ErrBadRequest("cwd is too long")
	}
	if strings.ContainsRune(input.Cwd, 0) {
		return input, service.ErrBadRequest("cwd contains invalid character")
	}
	return input, nil
}

func normalizeReviewTaskInput(input service.ReviewTaskInput) (service.ReviewTaskInput, error) {
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.Action = strings.TrimSpace(input.Action)
	input.Content = strings.TrimSpace(input.Content)

	if input.SessionID == "" {
		return input, service.ErrBadRequest("session_id is required")
	}
	if len([]rune(input.SessionID)) > maxSessionIDLen {
		return input, service.ErrBadRequest("session_id is too long")
	}
	if len(input.Content) > maxReviewContentLen {
		return input, service.ErrBadRequest("content is too long")
	}
	return input, nil
}

func isAllowedAgentType(agentType string) bool {
	switch generated.AgentType(agentType) {
	case generated.AgentTypeClaudeCode, generated.AgentTypeOpencode, generated.AgentTypeOrchestrator, generated.AgentTypeCodex:
		return true
	default:
		return false
	}
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
	streamCtx, cancel := context.WithTimeout(context.Background(), agentStreamTimeout)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in stream goroutine", "task_id", taskID, "session_id", sessionID, "panic", r)
			stream.PublishErrorAndFail(svc.messageDao, messageID, sessionID, "internal stream error")
			if err := svc.sessionDao.UpdateStatusByTask(sessionID, taskID, sessionStatusError); err != nil {
				slog.Warn("failed to mark session failed after stream panic", "task_id", taskID, "session_id", sessionID, "error", err)
			}
		}
	}()

	resp, err := svc.agentClient.StreamAgentWithContext(streamCtx, agentReq)
	if err != nil {
		slog.Warn("agent stream error", "task_id", taskID, "session_id", sessionID, "error", err)
		stream.PublishErrorAndFail(svc.messageDao, messageID, sessionID, "agent service unavailable")
		if err := svc.sessionDao.UpdateStatusByTask(sessionID, taskID, sessionStatusError); err != nil {
			slog.Warn("failed to mark session failed after stream error", "task_id", taskID, "session_id", sessionID, "error", err)
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("agent returned non-200", "task_id", taskID, "status", resp.StatusCode)
		stream.PublishErrorAndFail(svc.messageDao, messageID, sessionID, "agent stream failed")
		if err := svc.sessionDao.UpdateStatusByTask(sessionID, taskID, sessionStatusError); err != nil {
			slog.Warn("failed to mark session failed after non-200 stream response", "task_id", taskID, "session_id", sessionID, "error", err)
		}
		return
	}

	sw := stream.NewStreamWriter(streamCtx, taskID, sessionID, messageID, string(agentReq.AgentType), svc.messageDao, svc.sessionDao, svc.diffDao)

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
		if err := svc.sessionDao.UpdateStatusByTask(sessionID, taskID, sessionStatusError); err != nil {
			slog.Warn("failed to mark session failed after stream outcome", "task_id", taskID, "session_id", sessionID, "error", err)
		}
	case stream.RunOutcomeAwaitingReview:
		svc.markSessionCompletedAfterStream(taskID, sessionID)
	default:
		svc.markSessionCompletedAfterStream(taskID, sessionID)
	}
}

func (svc *TaskService) markSessionCompletedAfterStream(taskID, sessionID string) {
	sessionModel, err := svc.sessionDao.GetByTaskAndSessionID(taskID, sessionID)
	if err != nil {
		slog.Warn("failed to read session status before stream completion", "task_id", taskID, "session_id", sessionID, "error", err)
	} else if sessionModel != nil {
		switch sessionModel.Status {
		case sessionStatusAwaitingReview:
			slog.Info("session is awaiting review; keep status after stream pause", "task_id", taskID, "session_id", sessionID)
			return
		case sessionStatusRunning:
		default:
			slog.Info("session status changed; skip stream completion update", "task_id", taskID, "session_id", sessionID, "status", sessionModel.Status)
			return
		}
	}

	if err := svc.sessionDao.UpdateStatusByTask(sessionID, taskID, sessionStatusCompleted); err != nil {
		slog.Warn("failed to mark session completed after stream outcome", "task_id", taskID, "session_id", sessionID, "error", err)
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
