package impl

import (
	"strings"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/service"
)

type SessionService struct {
	dao dao.SessionDao
}

func NewSessionService(sessionDao dao.SessionDao) *SessionService {
	return &SessionService{dao: sessionDao}
}

func (svc *SessionService) PatchSessionStatus(sessionID, status string) (*service.SessionStatus, error) {
	sessionID = strings.TrimSpace(sessionID)
	status = strings.TrimSpace(status)
	if sessionID == "" {
		return nil, service.ErrBadRequest("session_id is required")
	}
	if len([]rune(sessionID)) > maxSessionIDLen {
		return nil, service.ErrBadRequest("session_id is too long")
	}
	if status != sessionStatusInactive {
		return nil, service.ErrBadRequest("status must be \"inactive\"")
	}

	found, err := svc.dao.DeactivateSession(sessionID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, service.ErrNotFound("session not found")
	}

	return &service.SessionStatus{
		SessionID: sessionID,
		Status:    sessionStatusInactive,
	}, nil
}
