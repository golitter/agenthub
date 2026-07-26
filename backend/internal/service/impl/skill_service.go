package impl

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
)

type skillAgentClient interface {
	InstallSkill(agentType, sessionID, skillName string, zipData []byte) error
	RemoveSkill(agentType, sessionID, skillName string) error
}

type SkillService struct {
	skillDao    dao.SkillDao
	sessionDao  dao.SessionDao
	agentClient skillAgentClient
}

const maxSkillNameLen = 128

func NewSkillService(skillDao dao.SkillDao, sessionDao dao.SessionDao, agentClient skillAgentClient) *SkillService {
	return &SkillService{
		skillDao:    skillDao,
		sessionDao:  sessionDao,
		agentClient: agentClient,
	}
}

func (svc *SkillService) UploadSkill(filename string, zipData []byte) (*service.ValidationResult, error) {
	result, tmpDir, err := service.ValidateZip(zipData)
	if err != nil {
		if result != nil {
			result.Valid = false
			result.TmpDir = ""
			return result, nil
		}
		return nil, service.ErrInternal("invalid zip file")
	}

	if !result.Valid {
		_ = os.RemoveAll(tmpDir)
		result.TmpDir = ""
		return result, nil
	}

	baseName := filepath.Base(filename)
	zipName := strings.TrimSuffix(baseName, filepath.Ext(baseName))
	if zipName != result.Name {
		_ = os.RemoveAll(tmpDir)
		return nil, service.ErrBadRequest(fmt.Sprintf("zip filename (%s) must match SKILL.md name (%s)", zipName, result.Name))
	}

	count, err := svc.skillDao.CountBuiltinByName(result.Name)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, err
	}
	if count > 0 {
		_ = os.RemoveAll(tmpDir)
		return &service.ValidationResult{
			Valid:  false,
			Errors: []string{"name conflicts with builtin skill"},
		}, nil
	}

	return result, nil
}

func (svc *SkillService) ConfirmSkill(name, _ string, _ int, _ int64, tmpDir string) (*service.SkillImportResult, error) {
	name, err := normalizeSkillName(name)
	if err != nil {
		return nil, err
	}
	metadata, err := service.InspectValidatedSkillDir(name, tmpDir)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	count, err := svc.skillDao.CountByName(name)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, service.ErrConflict("skill name already exists")
	}

	zipData, err := service.PackValidatedSkillDir(name, tmpDir)
	if err != nil {
		return nil, err
	}

	if err := svc.skillDao.CreateSkill(model.SkillHub{
		Name:        name,
		Builtin:     false,
		Description: metadata.Description,
		FileCount:   metadata.FileCount,
		TotalSize:   metadata.TotalSize,
		Content:     zipData,
	}); err != nil {
		return nil, err
	}

	return &service.SkillImportResult{Success: true, Name: name}, nil
}

func (svc *SkillService) ListSkills() ([]service.SkillHubItem, error) {
	skills, err := svc.skillDao.ListSkills()
	if err != nil {
		return nil, err
	}

	items := make([]service.SkillHubItem, 0, len(skills))
	for _, skill := range skills {
		importCount, err := svc.skillDao.CountImportsBySkillName(skill.Name)
		if err != nil {
			return nil, err
		}
		items = append(items, service.SkillHubItem{
			Name:        skill.Name,
			Builtin:     skill.Builtin,
			Description: skill.Description,
			FileCount:   skill.FileCount,
			TotalSize:   skill.TotalSize,
			ImportCount: importCount,
			CreatedAt:   skill.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return items, nil
}

func (svc *SkillService) DeleteSkill(name string) error {
	name, err := normalizeSkillName(name)
	if err != nil {
		return err
	}
	skill, err := svc.skillDao.GetSkillByName(name)
	if err != nil {
		return err
	}
	if skill == nil {
		return service.ErrNotFound("skill not found")
	}
	if skill.Builtin {
		return service.ErrForbidden("cannot delete builtin skill")
	}
	importCount, err := svc.skillDao.CountImportsBySkillName(name)
	if err != nil {
		return err
	}
	if importCount > 0 {
		return service.ErrConflict("skill is imported by active sessions; remove it from sessions first")
	}
	return svc.skillDao.DeleteSkillCascade(name)
}

func (svc *SkillService) ImportSkill(skillName, sessionID string) (*service.SkillImportResult, error) {
	skillName, err := normalizeSkillName(skillName)
	if err != nil {
		return nil, err
	}
	sessionID, err = normalizeProfileSessionID(sessionID)
	if err != nil {
		return nil, err
	}
	session, err := svc.sessionDao.GetBySessionID(sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, service.ErrNotFound("session not found")
	}

	allowedTypes := map[string]bool{"claude-code": true, "opencode": true, "codex": true}
	if !allowedTypes[session.AgentType] {
		return nil, service.ErrForbidden("orchestrator does not support importing external skills")
	}

	skill, err := svc.skillDao.GetSkillByName(skillName)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, service.ErrNotFound("skill not found in hub")
	}

	exists, err := svc.skillDao.HasAgentSkill(sessionID, skillName)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, service.ErrConflict("skill already imported to this session")
	}

	zipData, err := svc.skillDao.GetSkillContent(skillName)
	if err != nil {
		return nil, err
	}
	if len(zipData) == 0 {
		return nil, service.ErrInternal("pack skill files failed: no zip data")
	}

	if err := svc.agentClient.InstallSkill(session.AgentType, sessionID, skillName, zipData); err != nil {
		slog.Warn("install skill to worktree failed", "session_id", sessionID, "skill", skillName, "agent_type", session.AgentType, "error", err)
		return nil, service.ErrServiceUnavailable("install skill to worktree failed")
	}

	if err := svc.skillDao.CreateAgentSkill(model.AgentSkill{
		SessionID:  sessionID,
		SkillName:  skillName,
		AgentType:  session.AgentType,
		ImportedAt: time.Now(),
	}); err != nil {
		rollbackErr := svc.agentClient.RemoveSkill(session.AgentType, sessionID, skillName)
		if errors.Is(err, dao.ErrDuplicate) {
			if rollbackErr != nil {
				slog.Warn("rollback installed skill files failed after duplicate import", "session_id", sessionID, "skill", skillName, "error", rollbackErr)
				return nil, service.ErrInternal("skill already imported to this session; rollback installed files failed")
			}
			return nil, service.ErrConflict("skill already imported to this session")
		}
		if rollbackErr != nil {
			slog.Warn("rollback installed skill files failed after import record failure", "session_id", sessionID, "skill", skillName, "record_error", err, "rollback_error", rollbackErr)
			return nil, service.ErrInternal("record imported skill failed and rollback installed files failed")
		}
		return nil, err
	}

	return &service.SkillImportResult{
		Success: true,
		Skill:   skillName,
		Session: sessionID,
	}, nil
}

func (svc *SkillService) RemoveSkill(skillName, sessionID string) (*service.SkillImportResult, error) {
	skillName, err := normalizeSkillName(skillName)
	if err != nil {
		return nil, err
	}
	sessionID, err = normalizeProfileSessionID(sessionID)
	if err != nil {
		return nil, err
	}
	session, err := svc.sessionDao.GetBySessionID(sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, service.ErrNotFound("session not found")
	}

	exists, err := svc.skillDao.HasAgentSkill(sessionID, skillName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, service.ErrNotFound("skill is not imported to this session")
	}

	if err := svc.agentClient.RemoveSkill(session.AgentType, sessionID, skillName); err != nil {
		slog.Warn("remove skill files from worktree failed", "session_id", sessionID, "skill", skillName, "agent_type", session.AgentType, "error", err)
		return nil, service.ErrServiceUnavailable("remove skill files from worktree failed")
	}

	if err := svc.skillDao.DeleteAgentSkill(sessionID, skillName); err != nil {
		return nil, err
	}

	return &service.SkillImportResult{
		Success: true,
		Skill:   skillName,
		Session: sessionID,
	}, nil
}

func (svc *SkillService) ReportBuiltinSkills(skills []service.BuiltinSkillItem) error {
	for _, skill := range skills {
		name, err := normalizeSkillName(skill.Name)
		if err != nil {
			return err
		}
		description := strings.TrimSpace(skill.Description)
		if err := svc.skillDao.UpsertSkillHub(name, description, true); err != nil {
			return err
		}
	}
	return nil
}

func normalizeSkillName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", service.ErrBadRequest("skill name is required")
	}
	if len([]rune(name)) > maxSkillNameLen {
		return "", service.ErrBadRequest("skill name is too long")
	}
	if strings.ContainsAny(name, "/\\") {
		return "", service.ErrBadRequest("skill name is invalid")
	}
	return name, nil
}
