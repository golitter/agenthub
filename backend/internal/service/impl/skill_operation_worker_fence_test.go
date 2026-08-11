package impl

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/package_store"
)

type fencedSkillDao struct {
	*fakeSkillDao
	byID  *model.AgentSkill
	byKey *model.AgentSkill
}

type completingSkillDao struct {
	*fencedSkillDao
	deleteCompleted bool
	removeCompleted bool
}

func (dao *completingSkillDao) DeleteSkillCascadeWithOperation(name string, jobID uint64, leaseToken string, event model.SkillAuditEvent) error {
	dao.deleteCompleted = true
	return nil
}

func (dao *completingSkillDao) CompleteAgentSkillRemovalByID(agentSkillID uint, jobID uint64, leaseToken string, event model.SkillAuditEvent) error {
	dao.removeCompleted = true
	return nil
}

func (dao *fencedSkillDao) GetAgentSkillByID(uint) (*model.AgentSkill, error) {
	return dao.byID, nil
}

func (dao *fencedSkillDao) GetAgentSkill(string, string) (*model.AgentSkill, error) {
	return dao.byKey, nil
}

func TestSkillOperationWorkerFencesStaleRemoveByAgentSkillID(t *testing.T) {
	client := &fakeSkillAgentClient{}
	dao := &fencedSkillDao{
		fakeSkillDao: &fakeSkillDao{},
		// The old relation is gone, but a new import now owns the same pair.
		byID:  nil,
		byKey: &model.AgentSkill{SessionID: "session-1", SkillName: "demo", AgentType: "codex", Status: model.AgentSkillStatusInstalling},
	}
	worker := NewSkillOperationWorker(nil, dao, nil, client)
	err := worker.executeRemove(context.Background(), &model.SkillOperationJob{
		SessionID: "session-1", SkillName: "demo", AgentType: "codex", AgentSkillID: uintPtr(1),
	})
	if err != nil {
		t.Fatalf("executeRemove returned error: %v", err)
	}
	if client.removeCalls != 0 {
		t.Fatalf("stale remove called AgentEnd %d times, want 0", client.removeCalls)
	}
}

func TestSkillOperationWorkerMarksSyncErrorWhenRelationDeleteFails(t *testing.T) {
	client := &fakeSkillAgentClient{}
	dao := &fencedSkillDao{
		fakeSkillDao: &fakeSkillDao{deleteErr: errors.New("db unavailable")},
		byID:         &model.AgentSkill{ID: 1, SessionID: "session-1", SkillName: "demo", AgentType: "codex", Status: model.AgentSkillStatusRemoving},
	}
	worker := NewSkillOperationWorker(nil, dao, nil, client)
	err := worker.executeRemove(context.Background(), &model.SkillOperationJob{
		SessionID: "session-1", SkillName: "demo", AgentType: "codex", AgentSkillID: uintPtr(1),
	})
	if err == nil {
		t.Fatal("executeRemove returned nil, want delete failure")
	}
	if len(dao.statusUpdates) != 1 || dao.statusUpdates[0] != model.AgentSkillStatusSyncError {
		t.Fatalf("status updates = %v, want one sync_error update", dao.statusUpdates)
	}
}

func TestSkillOperationWorkerRestoresReadyOnExplicitRemoveRejection(t *testing.T) {
	client := &fakeSkillAgentClient{removeErr: knownInstallFailure{}}
	dao := &fencedSkillDao{
		fakeSkillDao: &fakeSkillDao{},
		byID:         &model.AgentSkill{ID: 1, SessionID: "session-1", SkillName: "demo", AgentType: "codex", Status: model.AgentSkillStatusRemoving},
	}
	worker := NewSkillOperationWorker(nil, dao, nil, client)
	err := worker.executeRemove(context.Background(), &model.SkillOperationJob{
		ID: 9, SessionID: "session-1", SkillName: "demo", AgentType: "codex", AgentSkillID: uintPtr(1),
	})
	if !errors.Is(err, errSkillOperationTerminal) {
		t.Fatalf("executeRemove error = %v, want terminal outcome", err)
	}
	if len(dao.statusUpdates) != 1 || dao.statusUpdates[0] != model.AgentSkillStatusReady {
		t.Fatalf("status updates = %v, want ready restoration", dao.statusUpdates)
	}
}

func TestSkillOperationWorkerDoesNotCompensateNewImport(t *testing.T) {
	client := &fakeSkillAgentClient{}
	dao := &fencedSkillDao{
		fakeSkillDao: &fakeSkillDao{},
		byKey:        &model.AgentSkill{SessionID: "session-1", SkillName: "demo", AgentType: "codex", Status: model.AgentSkillStatusInstalling},
	}
	worker := NewSkillOperationWorker(nil, dao, nil, client)
	reader := &SkillService{skillDao: dao, agentClient: client}
	err := worker.compensateInstallRemoval(context.Background(), reader, &model.SkillOperationJob{
		SessionID: "session-1", SkillName: "demo", AgentType: "codex", AgentSkillID: uintPtr(1),
	})
	if err != nil {
		t.Fatalf("compensateInstallRemoval returned error: %v", err)
	}
	if client.removeCalls != 0 {
		t.Fatalf("compensation removed newer import %d times, want 0", client.removeCalls)
	}
}

func TestSkillOperationWorkerCompensatesTheSameRemovingRelation(t *testing.T) {
	client := &fakeSkillAgentClient{}
	old := &model.AgentSkill{ID: 1, SessionID: "session-1", SkillName: "demo", AgentType: "codex", Status: model.AgentSkillStatusRemoving}
	dao := &fencedSkillDao{
		fakeSkillDao: &fakeSkillDao{},
		byID:         old,
		byKey:        old,
	}
	worker := NewSkillOperationWorker(nil, dao, nil, client)
	reader := &SkillService{skillDao: dao, agentClient: client}
	err := worker.compensateInstallRemoval(context.Background(), reader, &model.SkillOperationJob{
		SessionID: "session-1", SkillName: "demo", AgentType: "codex", AgentSkillID: uintPtr(1),
	})
	if err != nil {
		t.Fatalf("compensateInstallRemoval returned error: %v", err)
	}
	if client.removeCalls != 1 {
		t.Fatalf("remove calls = %d, want 1 for the same removing relation", client.removeCalls)
	}
}

func TestSkillOperationWorkerFencesLegacyDeleteWithoutSkillID(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	objectKey := "skills/demo/" + sha + ".zip"
	store := package_store.NewMemoryStore()
	data := []byte("package")
	if err := store.Put(context.Background(), objectKey, bytes.NewReader(data), int64(len(data)), ""); err != nil {
		t.Fatal(err)
	}
	dao := &fakeSkillDao{skill: &model.SkillHub{
		ID: 7, Name: "demo", ObjectKey: objectKey, StorageType: model.SkillStorageMinIO,
		Status: model.SkillStatusReady,
	}}
	worker := NewSkillOperationWorker(nil, dao, store, &fakeSkillAgentClient{})
	err := worker.executeDelete(context.Background(), &model.SkillOperationJob{
		SkillName: "demo", ObjectKey: objectKey,
	})
	if err != nil {
		t.Fatalf("executeDelete returned error: %v", err)
	}
	if dao.deleteCalled {
		t.Fatal("legacy delete job cascaded a newer ready Skill")
	}
	if _, err := store.Stat(context.Background(), objectKey); err != nil {
		t.Fatalf("legacy delete job removed the newer Skill object: %v", err)
	}
}

func TestSkillOperationWorkerUsesAtomicDeleteCompletion(t *testing.T) {
	objectKey := "skills/demo/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.zip"
	store := package_store.NewMemoryStore()
	if err := store.Put(context.Background(), objectKey, bytes.NewReader([]byte("package")), int64(len("package")), ""); err != nil {
		t.Fatal(err)
	}
	dao := &completingSkillDao{fencedSkillDao: &fencedSkillDao{fakeSkillDao: &fakeSkillDao{skill: &model.SkillHub{
		ID: 7, Name: "demo", ObjectKey: objectKey, StorageType: model.SkillStorageMinIO,
		Status: model.SkillStatusDeleting,
	}}}}
	worker := NewSkillOperationWorker(nil, dao, store, &fakeSkillAgentClient{})
	err := worker.executeDelete(context.Background(), &model.SkillOperationJob{
		ID: 9, SkillID: uintPtr(7), SkillName: "demo", ObjectKey: objectKey,
	})
	if !errors.Is(err, errSkillOperationTerminal) {
		t.Fatalf("executeDelete error = %v, want terminal outcome", err)
	}
	if !dao.deleteCompleted {
		t.Fatal("worker did not use atomic delete completion")
	}
	if dao.deleteCalled {
		t.Fatal("worker used split cascade after atomic completion was available")
	}
}

func TestSkillOperationWorkerUsesAtomicRemoveCompletion(t *testing.T) {
	dao := &completingSkillDao{fencedSkillDao: &fencedSkillDao{
		fakeSkillDao: &fakeSkillDao{},
		byID:         &model.AgentSkill{ID: 1, SessionID: "session-1", SkillName: "demo", AgentType: "codex", Status: model.AgentSkillStatusRemoving},
	}}
	worker := NewSkillOperationWorker(nil, dao, nil, &fakeSkillAgentClient{})
	err := worker.executeRemove(context.Background(), &model.SkillOperationJob{
		ID: 9, LeaseToken: "lease", SessionID: "session-1", SkillName: "demo", AgentType: "codex", AgentSkillID: uintPtr(1),
	})
	if !errors.Is(err, errSkillOperationTerminal) {
		t.Fatalf("executeRemove error = %v, want terminal outcome", err)
	}
	if !dao.removeCompleted {
		t.Fatal("worker did not use atomic remove completion")
	}
	if dao.deleteAgentSkillCalled {
		t.Fatal("worker used split AgentSkill delete after atomic completion was available")
	}
}

type pendingObjectOperationDao struct {
	fakeSkillOperationDao
	pending bool
}

func (dao *pendingObjectOperationDao) HasPendingObjectOperationExcept(string, uint64) (bool, error) {
	return dao.pending, nil
}

func TestSkillOperationWorkerSkipsOrphanDeleteWhileConfirmationIsPending(t *testing.T) {
	objectKey := "skills/demo/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.zip"
	store := package_store.NewMemoryStore()
	data := []byte("package")
	if err := store.Put(context.Background(), objectKey, bytes.NewReader(data), int64(len(data)), ""); err != nil {
		t.Fatal(err)
	}
	jobs := &pendingObjectOperationDao{pending: true}
	worker := NewSkillOperationWorker(jobs, &fakeSkillDao{}, store, &fakeSkillAgentClient{})
	err := worker.executeDelete(context.Background(), &model.SkillOperationJob{ID: 9, ObjectKey: objectKey})
	if err != nil {
		t.Fatalf("executeDelete returned error: %v", err)
	}
	if _, err := store.Stat(context.Background(), objectKey); err != nil {
		t.Fatalf("orphan delete removed a protected object: %v", err)
	}
}

func TestSkillOperationWorkerDoesNotExecuteLiveConfirmationProtection(t *testing.T) {
	objectKey := "skills/demo/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef.zip"
	store := package_store.NewMemoryStore()
	data := []byte("package")
	if err := store.Put(context.Background(), objectKey, bytes.NewReader(data), int64(len(data)), ""); err != nil {
		t.Fatal(err)
	}
	worker := NewSkillOperationWorker(nil, &fakeSkillDao{}, store, &fakeSkillAgentClient{})
	// Attempt 1 is the request-owned protection lease.  A worker that wins the
	// create/claim race must complete this no-op rather than deleting a future
	// formal object before the confirmation transaction commits.
	err := worker.executeDelete(context.Background(), &model.SkillOperationJob{
		IdempotencyKey: "confirm-object:upload-1", Attempts: 1, ObjectKey: objectKey,
	})
	if err != nil {
		t.Fatalf("executeDelete returned error: %v", err)
	}
	if _, err := store.Stat(context.Background(), objectKey); err != nil {
		t.Fatalf("live confirmation protection removed formal object: %v", err)
	}
}

func uintPtr(value uint) *uint { return &value }
