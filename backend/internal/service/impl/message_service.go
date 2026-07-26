package impl

import (
	"strings"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
)

type MessageService struct {
	taskDao    dao.TaskDao
	sessionDao dao.SessionDao
	messageDao dao.MessageDao
}

func NewMessageService(taskDao dao.TaskDao, sessionDao dao.SessionDao, messageDao dao.MessageDao) *MessageService {
	return &MessageService{
		taskDao:    taskDao,
		sessionDao: sessionDao,
		messageDao: messageDao,
	}
}

func (svc *MessageService) ListMessages(taskID, sessionID, mode, primarySessionID string, limit int, beforeID *uint64, paginated bool) (*service.ListMessagesResponse, error) {
	taskID, err := normalizeTaskID(taskID)
	if err != nil {
		return nil, err
	}
	sessionID, mode, primarySessionID, err = normalizeMessageListInput(sessionID, mode, primarySessionID)
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

	if primarySessionID == "" {
		primarySessionID = sessionID
	}
	if mode == "group" && primarySessionID == "" {
		primarySessionID, err = svc.sessionDao.FindPrimaryGroupSessionID(taskID)
		if err != nil {
			return nil, err
		}
	}

	if !paginated {
		messages, err := svc.messageDao.ListByTask(taskID, sessionID, mode, primarySessionID, 0, nil)
		if err != nil {
			return nil, err
		}
		return &service.ListMessagesResponse{Data: messages, HasMore: false}, nil
	}

	if limit <= 0 {
		limit = 20
	}
	messages, err := svc.messageDao.ListByTask(taskID, sessionID, mode, primarySessionID, limit+1, beforeID)
	if err != nil {
		return nil, err
	}

	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[:limit]
	}
	reverseMessages(messages)
	return &service.ListMessagesResponse{Data: messages, HasMore: hasMore}, nil
}

func (svc *MessageService) WindowMessages(taskID, sessionID string) ([]map[string]interface{}, error) {
	taskID, err := normalizeTaskID(taskID)
	if err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, service.ErrBadRequest("session_id is required")
	}
	if len([]rune(sessionID)) > maxSessionIDLen {
		return nil, service.ErrBadRequest("session_id is too long")
	}
	return fetchGroupChatWindow(svc.messageDao, taskID, sessionID), nil
}

func normalizeMessageListInput(sessionID, mode, primarySessionID string) (string, string, string, error) {
	sessionID = strings.TrimSpace(sessionID)
	mode = strings.TrimSpace(mode)
	primarySessionID = strings.TrimSpace(primarySessionID)

	if mode != "" && mode != "group" {
		return sessionID, mode, primarySessionID, service.ErrBadRequest("mode must be group")
	}
	if len([]rune(sessionID)) > maxSessionIDLen || len([]rune(primarySessionID)) > maxSessionIDLen {
		return sessionID, mode, primarySessionID, service.ErrBadRequest("session_id is too long")
	}
	return sessionID, mode, primarySessionID, nil
}

func reverseMessages(messages []model.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}
