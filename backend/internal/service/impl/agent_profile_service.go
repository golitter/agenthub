package impl

import (
	"log/slog"
	"path/filepath"
	"time"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/agentend_client"
)

type AgentProfileService struct {
	sessionDao  dao.SessionDao
	taskDao     dao.TaskDao
	messageDao  dao.MessageDao
	skillDao    dao.SkillDao
	agentClient *agentend_client.Client
}

func NewAgentProfileService(sessionDao dao.SessionDao, taskDao dao.TaskDao, messageDao dao.MessageDao, skillDao dao.SkillDao, agentClient *agentend_client.Client) *AgentProfileService {
	return &AgentProfileService{
		sessionDao:  sessionDao,
		taskDao:     taskDao,
		messageDao:  messageDao,
		skillDao:    skillDao,
		agentClient: agentClient,
	}
}

func (svc *AgentProfileService) GetProfile(sessionID string) (*service.AgentProfileResponse, error) {
	sessionModel, err := svc.sessionDao.GetBySessionID(sessionID)
	if err != nil {
		return nil, err
	}
	if sessionModel == nil {
		return nil, service.ErrNotFound("session not found")
	}

	skills := svc.fetchSkills(sessionModel.AgentType, sessionModel.SessionID)
	return &service.AgentProfileResponse{
		AgentName: sessionModel.AgentName,
		AgentType: sessionModel.AgentType,
		AvatarURL: sessionModel.AvatarURL,
		Status:    sessionModel.Status,
		SessionID: sessionModel.SessionID,
		SoulMD:    sessionModel.SoulMD,
		Skills:    skills,
	}, nil
}

func (svc *AgentProfileService) GetDetail(sessionID string) (*service.AgentDetailResponse, error) {
	sessionModel, err := svc.sessionDao.GetBySessionID(sessionID)
	if err != nil {
		return nil, err
	}
	if sessionModel == nil {
		return nil, service.ErrNotFound("session not found")
	}

	task, err := svc.taskDao.GetByTaskID(sessionModel.TaskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, service.ErrNotFound("task not found")
	}

	messageCount, err := svc.messageDao.CountBySessionID(sessionID)
	if err != nil {
		return nil, err
	}

	return &service.AgentDetailResponse{
		AgentName:     sessionModel.AgentName,
		AgentType:     sessionModel.AgentType,
		AvatarURL:     sessionModel.AvatarURL,
		Status:        sessionModel.Status,
		SessionID:     sessionModel.SessionID,
		TaskID:        sessionModel.TaskID,
		RepoPath:      task.RepoPath,
		WorkspacePath: filepath.Join(filepath.Dir(task.RepoPath), "worktrees", sessionModel.TaskID, sessionModel.SessionID),
		SoulMD:        sessionModel.SoulMD,
		CreatedAt:     sessionModel.CreatedAt,
		MessageCount:  messageCount,
		Skills:        svc.fetchSkills(sessionModel.AgentType, sessionModel.SessionID),
	}, nil
}

func (svc *AgentProfileService) GetSoul(sessionID string) (string, error) {
	sessionModel, err := svc.sessionDao.GetBySessionID(sessionID)
	if err != nil {
		return "", err
	}
	if sessionModel == nil {
		return "", service.ErrNotFound("session not found")
	}
	return sessionModel.SoulMD, nil
}

func (svc *AgentProfileService) UpdateSoul(sessionID, soulMD string) error {
	stripped := stripSpaces(soulMD)
	if len([]rune(stripped)) > 300 {
		return service.ErrBadRequest("soul_md must not exceed 300 characters")
	}

	updated, err := svc.sessionDao.UpdateSoul(sessionID, stripped)
	if err != nil {
		return err
	}
	if !updated {
		return service.ErrNotFound("session not found")
	}
	return nil
}

func (svc *AgentProfileService) fetchSkills(agentType, sessionID string) []service.AgentSkill {
	skillInfos, err := svc.agentClient.FetchSkills(agentType, sessionID)
	if err != nil {
		slog.Debug("fetch skills from agentend failed, fallback to db", "error", err)
		return svc.fetchSkillsFromDB(sessionID)
	}
	if len(skillInfos) == 0 {
		return svc.fetchSkillsFromDB(sessionID)
	}

	skills := make([]service.AgentSkill, 0, len(skillInfos))
	for _, skillInfo := range skillInfos {
		skills = append(skills, service.AgentSkill{
			Name:        skillInfo.Name,
			Description: skillInfo.Description,
			Builtin:     skillInfo.Builtin,
			Source:      skillInfo.Source,
		})
	}

	svc.syncSkillsToDB(agentType, sessionID, skills)
	return skills
}

func (svc *AgentProfileService) syncSkillsToDB(agentType, sessionID string, skills []service.AgentSkill) {
	for _, skillInfo := range skills {
		if err := svc.skillDao.UpsertSkillHub(skillInfo.Name, skillInfo.Description, skillInfo.Builtin); err != nil {
			slog.Warn("failed to upsert skill hub", "skill", skillInfo.Name, "error", err)
			continue
		}
		if skillInfo.Builtin {
			continue
		}
		if err := svc.skillDao.EnsureAgentSkill(sessionID, skillInfo.Name, agentType); err != nil {
			slog.Warn("failed to ensure agent skill relation", "skill", skillInfo.Name, "session_id", sessionID, "error", err)
		}
	}
}

func (svc *AgentProfileService) fetchSkillsFromDB(sessionID string) []service.AgentSkill {
	skills := make([]service.AgentSkill, 0)

	builtins, err := svc.skillDao.ListBuiltinSkills()
	if err == nil {
		for _, skillInfo := range builtins {
			skills = append(skills, service.AgentSkill{
				Name:        skillInfo.Name,
				Description: skillInfo.Description,
				Builtin:     true,
				Source:      "builtin",
			})
		}
	}

	externalSkills, err := svc.skillDao.ListExternalSkillsBySession(sessionID)
	if err == nil {
		for _, skillInfo := range externalSkills {
			skills = append(skills, service.AgentSkill{
				Name:        skillInfo.Name,
				Description: skillInfo.Description,
				Builtin:     false,
				Source:      "hub",
			})
		}
	}

	return skills
}

func stripSpaces(value string) string {
	result := make([]rune, 0, len(value))
	for _, r := range value {
		if r != ' ' {
			result = append(result, r)
		}
	}
	return string(result)
}

var _ = time.Second
