package impl

import (
	"errors"
	"strings"
	"testing"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
)

type fakeSkillDao struct {
	skill        *model.SkillHub
	content      []byte
	importCount  int64
	hasAgentSkill bool
	createErr    error
	deleteCalled bool
}

func (dao *fakeSkillDao) CountBuiltinByName(name string) (int64, error) { return 0, nil }
func (dao *fakeSkillDao) CountByName(name string) (int64, error)        { return 0, nil }
func (dao *fakeSkillDao) CreateSkill(skill model.SkillHub) error        { return nil }
func (dao *fakeSkillDao) ListSkills() ([]model.SkillHub, error)         { return nil, nil }
func (dao *fakeSkillDao) CountImportsBySkillName(name string) (int64, error) {
	return dao.importCount, nil
}
func (dao *fakeSkillDao) GetSkillByName(name string) (*model.SkillHub, error) {
	return dao.skill, nil
}
func (dao *fakeSkillDao) GetSkillContent(name string) ([]byte, error) {
	return dao.content, nil
}
func (dao *fakeSkillDao) DeleteSkillCascade(name string) error {
	dao.deleteCalled = true
	return nil
}
func (dao *fakeSkillDao) HasAgentSkill(sessionID, skillName string) (bool, error) {
	return dao.hasAgentSkill, nil
}
func (dao *fakeSkillDao) CreateAgentSkill(skill model.AgentSkill) error {
	return dao.createErr
}
func (dao *fakeSkillDao) DeleteAgentSkill(sessionID, skillName string) error { return nil }
func (dao *fakeSkillDao) UpsertSkillHub(name, description string, builtin bool) error {
	return nil
}
func (dao *fakeSkillDao) EnsureAgentSkill(sessionID, skillName, agentType string) error {
	return nil
}
func (dao *fakeSkillDao) ListBuiltinSkills() ([]model.SkillHub, error) { return nil, nil }
func (dao *fakeSkillDao) ListExternalSkillsBySession(sessionID string) ([]model.SkillHub, error) {
	return nil, nil
}

type fakeSkillSessionDao struct {
	session *model.Session
}

func (dao *fakeSkillSessionDao) DeactivateSession(sessionID string) (bool, error) { return false, nil }
func (dao *fakeSkillSessionDao) GetBySessionID(sessionID string) (*model.Session, error) {
	return dao.session, nil
}
func (dao *fakeSkillSessionDao) GetByTaskAndSessionID(taskID, sessionID string) (*model.Session, error) {
	return nil, nil
}
func (dao *fakeSkillSessionDao) ListByTaskID(taskID string) ([]model.Session, error) {
	return nil, nil
}
func (dao *fakeSkillSessionDao) ListAll() ([]model.Session, error) { return nil, nil }
func (dao *fakeSkillSessionDao) FindPrimaryGroupSessionID(taskID string) (string, error) {
	return "", nil
}
func (dao *fakeSkillSessionDao) UpdateFields(sessionID string, updates map[string]interface{}) (bool, error) {
	return false, nil
}
func (dao *fakeSkillSessionDao) UpdateSoul(sessionID, soulMD string) (bool, error) {
	return false, nil
}
func (dao *fakeSkillSessionDao) UpdateStatusByTask(sessionID, taskID, status string) error {
	return nil
}

type fakeSkillAgentClient struct {
	removeErr    error
	installCalls int
	removeCalls  int
}

func (client *fakeSkillAgentClient) InstallSkill(agentType, sessionID, skillName string, zipData []byte) error {
	client.installCalls++
	return nil
}

func (client *fakeSkillAgentClient) RemoveSkill(agentType, sessionID, skillName string) error {
	client.removeCalls++
	return client.removeErr
}

func TestImportSkillRollsBackInstalledFilesWhenDBCreateFails(t *testing.T) {
	svc := NewSkillService(
		&fakeSkillDao{
			skill:     &model.SkillHub{Name: "reviewer"},
			content:   []byte("zip"),
			createErr: errors.New("db down"),
		},
		&fakeSkillSessionDao{session: &model.Session{SessionID: "s1", AgentType: "codex"}},
		&fakeSkillAgentClient{},
	)
	client := svc.agentClient.(*fakeSkillAgentClient)

	_, err := svc.ImportSkill("reviewer", "s1")
	if err == nil {
		t.Fatal("expected import error")
	}
	if client.installCalls != 1 || client.removeCalls != 1 {
		t.Fatalf("install/remove calls = %d/%d, want 1/1", client.installCalls, client.removeCalls)
	}
}

func TestImportSkillReportsRollbackFailure(t *testing.T) {
	svc := NewSkillService(
		&fakeSkillDao{
			skill:     &model.SkillHub{Name: "reviewer"},
			content:   []byte("zip"),
			createErr: errors.New("db down"),
		},
		&fakeSkillSessionDao{session: &model.Session{SessionID: "s1", AgentType: "codex"}},
		&fakeSkillAgentClient{removeErr: errors.New("agentend unavailable")},
	)

	_, err := svc.ImportSkill("reviewer", "s1")
	bizErr, ok := err.(*service.BizError)
	if !ok || bizErr.Code != 500 {
		t.Fatalf("err = %#v, want 500 BizError", err)
	}
	if !strings.Contains(bizErr.Message, "rollback installed files failed") {
		t.Fatalf("message = %q, want rollback failure", bizErr.Message)
	}
}

func TestRemoveSkillRequiresImportedRelationBeforeDeletingFiles(t *testing.T) {
	client := &fakeSkillAgentClient{}
	svc := NewSkillService(
		&fakeSkillDao{hasAgentSkill: false},
		&fakeSkillSessionDao{session: &model.Session{SessionID: "s1", AgentType: "codex"}},
		client,
	)

	_, err := svc.RemoveSkill("reviewer", "s1")
	bizErr, ok := err.(*service.BizError)
	if !ok || bizErr.Code != 404 {
		t.Fatalf("err = %#v, want 404 BizError", err)
	}
	if client.removeCalls != 0 {
		t.Fatalf("remove calls = %d, want 0", client.removeCalls)
	}
}

func TestDeleteSkillBlocksImportedExternalSkill(t *testing.T) {
	skillDao := &fakeSkillDao{
		skill:       &model.SkillHub{Name: "reviewer", Builtin: false},
		importCount: 1,
	}
	svc := NewSkillService(skillDao, &fakeSkillSessionDao{}, &fakeSkillAgentClient{})

	err := svc.DeleteSkill("reviewer")
	bizErr, ok := err.(*service.BizError)
	if !ok || bizErr.Code != 409 {
		t.Fatalf("err = %#v, want 409 BizError", err)
	}
	if skillDao.deleteCalled {
		t.Fatal("DeleteSkillCascade was called for an imported skill")
	}
}

func TestUploadSkillReturnsValidationResultForInvalidZip(t *testing.T) {
	svc := NewSkillService(&fakeSkillDao{}, &fakeSkillSessionDao{}, &fakeSkillAgentClient{})

	result, err := svc.UploadSkill("bad.zip", []byte("not a zip"))
	if err != nil {
		t.Fatalf("UploadSkill error = %v, want validation result", err)
	}
	if result == nil || result.Valid {
		t.Fatalf("result = %#v, want invalid validation result", result)
	}
}
