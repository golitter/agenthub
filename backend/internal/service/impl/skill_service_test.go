package impl

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/package_store"
)

type fakeSkillDao struct {
	skill                  *model.SkillHub
	createdSkill           *model.SkillHub
	content                []byte
	importCount            int64
	hasAgentSkill          bool
	createErr              error
	statusErr              error
	statusUpdates          []string
	backfilledSHA          string
	backfillErr            error
	deleteErr              error
	deleteCalled           bool
	deleteAgentSkillCalled bool
}

func (dao *fakeSkillDao) CountBuiltinByName(name string) (int64, error) { return 0, nil }
func (dao *fakeSkillDao) CountByName(name string) (int64, error)        { return 0, nil }
func (dao *fakeSkillDao) CreateSkill(skill model.SkillHub) error {
	skill.ID = 1
	dao.createdSkill = &skill
	return nil
}
func (dao *fakeSkillDao) ListSkills() ([]model.SkillHub, error) { return nil, nil }
func (dao *fakeSkillDao) CountImportsBySkillName(name string) (int64, error) {
	return dao.importCount, nil
}
func (dao *fakeSkillDao) GetSkillByName(name string) (*model.SkillHub, error) {
	if dao.skill == nil {
		return dao.createdSkill, nil
	}
	return dao.skill, nil
}
func (dao *fakeSkillDao) GetSkillByID(id uint) (*model.SkillHub, error) {
	if dao.skill == nil {
		return dao.createdSkill, nil
	}
	return dao.skill, nil
}
func (dao *fakeSkillDao) GetSkillContent(name string) ([]byte, error) {
	return dao.content, nil
}
func (dao *fakeSkillDao) UpdateSkillContentAndMetadata(id uint, content []byte, sha string, packageSize int64) error {
	dao.backfilledSHA = sha
	return dao.backfillErr
}
func (dao *fakeSkillDao) GetSkillUploadReceipt(uploadID string) (*model.SkillUploadReceipt, error) {
	return nil, nil
}
func (dao *fakeSkillDao) CreateSkillUploadReceipt(receipt model.SkillUploadReceipt) error { return nil }
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
func (dao *fakeSkillDao) UpdateAgentSkillStatus(sessionID, skillName, status string) error {
	dao.statusUpdates = append(dao.statusUpdates, status)
	return dao.statusErr
}
func (dao *fakeSkillDao) UpdateSkillStatus(name, status string) error {
	dao.statusUpdates = append(dao.statusUpdates, status)
	return dao.statusErr
}
func (dao *fakeSkillDao) DeleteAgentSkill(sessionID, skillName string) error {
	dao.deleteAgentSkillCalled = true
	return dao.deleteErr
}
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
	installErr   error
	installCalls int
	removeCalls  int
}

type fakeSkillOperationDao struct {
	jobs []model.SkillOperationJob
}

func (dao *fakeSkillOperationDao) CreateSkillOperationJob(job model.SkillOperationJob) (*model.SkillOperationJob, error) {
	job.ID = uint64(len(dao.jobs) + 1)
	dao.jobs = append(dao.jobs, job)
	return &job, nil
}
func (dao *fakeSkillOperationDao) ClaimSkillOperationJob(uint64, time.Time, time.Duration) (*model.SkillOperationJob, error) {
	return nil, nil
}
func (dao *fakeSkillOperationDao) ClaimDueSkillOperationJob(time.Time, time.Duration) (*model.SkillOperationJob, error) {
	return nil, nil
}
func (dao *fakeSkillOperationDao) CompleteSkillOperationJob(uint64, string) error { return nil }
func (dao *fakeSkillOperationDao) RetrySkillOperationJob(uint64, string, string, time.Time) error {
	return nil
}
func (dao *fakeSkillOperationDao) DeleteSkillOperationJob(uint64, string) error   { return nil }
func (dao *fakeSkillOperationDao) HasPendingObjectOperation(string) (bool, error) { return false, nil }

func (client *fakeSkillAgentClient) InstallSkill(agentType, sessionID, skillName string, zipData []byte) error {
	client.installCalls++
	return client.installErr
}

func (client *fakeSkillAgentClient) RemoveSkill(agentType, sessionID, skillName string) error {
	client.removeCalls++
	return client.removeErr
}

type knownInstallFailure struct{}

func (knownInstallFailure) Error() string      { return "unsafe archive" }
func (knownInstallFailure) KnownFailure() bool { return true }

func TestImportSkillRollsBackExplicitAgentEndRejection(t *testing.T) {
	skillDao := &fakeSkillDao{skill: &model.SkillHub{Name: "reviewer"}, content: []byte("zip")}
	client := &fakeSkillAgentClient{installErr: knownInstallFailure{}}
	svc := NewSkillService(
		skillDao,
		&fakeSkillSessionDao{session: &model.Session{SessionID: "s1", AgentType: "codex"}},
		client,
	)

	_, err := svc.ImportSkill(context.Background(), "reviewer", "s1")
	var bizErr *service.BizError
	if !errors.As(err, &bizErr) || bizErr.Code != 400 {
		t.Fatalf("err = %#v, want 400 BizError", err)
	}
	if !skillDao.deleteAgentSkillCalled {
		t.Fatal("explicit AgentEnd rejection left the install reservation behind")
	}
}

func TestRemoveSkillRestoresReadyOnExplicitAgentEndRejection(t *testing.T) {
	skillDao := &fakeSkillDao{hasAgentSkill: true}
	client := &fakeSkillAgentClient{removeErr: knownInstallFailure{}}
	svc := NewSkillService(
		skillDao,
		&fakeSkillSessionDao{session: &model.Session{SessionID: "s1", AgentType: "codex"}},
		client,
	)

	_, err := svc.RemoveSkill(context.Background(), "reviewer", "s1")
	var bizErr *service.BizError
	if !errors.As(err, &bizErr) || bizErr.Code != 400 {
		t.Fatalf("err = %#v, want 400 BizError", err)
	}
	if client.removeCalls != 1 {
		t.Fatalf("remove calls = %d, want 1", client.removeCalls)
	}
	if len(skillDao.statusUpdates) != 2 || skillDao.statusUpdates[0] != model.AgentSkillStatusRemoving || skillDao.statusUpdates[1] != model.AgentSkillStatusReady {
		t.Fatalf("status updates = %v, want removing then ready", skillDao.statusUpdates)
	}
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

	_, err := svc.ImportSkill(context.Background(), "reviewer", "s1")
	if err == nil {
		t.Fatal("expected import error")
	}
	if client.installCalls != 0 || client.removeCalls != 0 {
		t.Fatalf("install/remove calls = %d/%d, want 0/0", client.installCalls, client.removeCalls)
	}
}

func TestImportSkillReportsStatusUpdateFailure(t *testing.T) {
	svc := NewSkillService(
		&fakeSkillDao{
			skill:     &model.SkillHub{Name: "reviewer"},
			content:   []byte("zip"),
			statusErr: errors.New("db down"),
		},
		&fakeSkillSessionDao{session: &model.Session{SessionID: "s1", AgentType: "codex"}},
		&fakeSkillAgentClient{removeErr: errors.New("agentend unavailable")},
	)

	_, err := svc.ImportSkill(context.Background(), "reviewer", "s1")
	bizErr, ok := err.(*service.BizError)
	if !ok || bizErr.Code != 500 {
		t.Fatalf("err = %#v, want 500 BizError", err)
	}
	if !strings.Contains(bizErr.Message, "status update failed") {
		t.Fatalf("message = %q, want status update failure", bizErr.Message)
	}
}

func TestReadDBSkillPackageBackfillsMissingSHA(t *testing.T) {
	content := []byte("legacy package bytes")
	dao := &fakeSkillDao{skill: &model.SkillHub{ID: 9, Name: "legacy", StorageType: model.SkillStorageDB}, content: content}
	svc := NewSkillService(dao, &fakeSkillSessionDao{}, &fakeSkillAgentClient{})

	data, err := svc.readDBSkillPackage(dao.skill)
	if err != nil {
		t.Fatalf("readDBSkillPackage() error = %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("content = %q, want %q", data, content)
	}
	wantSHA := sha256Hex(content)
	if dao.backfilledSHA != wantSHA {
		t.Fatalf("backfilled SHA = %q, want %q", dao.backfilledSHA, wantSHA)
	}
	if dao.skill.SHA256 != wantSHA {
		t.Fatalf("in-memory SHA = %q, want %q", dao.skill.SHA256, wantSHA)
	}
}

func TestReadDBSkillPackageBackfillsMissingPackageSize(t *testing.T) {
	content := []byte("legacy package with known digest")
	digest := sha256Hex(content)
	dao := &fakeSkillDao{skill: &model.SkillHub{ID: 10, Name: "legacy-size", SHA256: digest, StorageType: model.SkillStorageDB}, content: content}
	svc := NewSkillService(dao, &fakeSkillSessionDao{}, &fakeSkillAgentClient{})

	if _, err := svc.readDBSkillPackage(dao.skill); err != nil {
		t.Fatalf("readDBSkillPackage() error = %v", err)
	}
	if dao.backfilledSHA != digest {
		t.Fatalf("backfilled SHA = %q, want %q", dao.backfilledSHA, digest)
	}
	if dao.skill.PackageSize != int64(len(content)) {
		t.Fatalf("in-memory package size = %d, want %d", dao.skill.PackageSize, len(content))
	}
}

func TestRemoveSkillRequiresImportedRelationBeforeDeletingFiles(t *testing.T) {
	client := &fakeSkillAgentClient{}
	svc := NewSkillService(
		&fakeSkillDao{hasAgentSkill: false},
		&fakeSkillSessionDao{session: &model.Session{SessionID: "s1", AgentType: "codex"}},
		client,
	)

	_, err := svc.RemoveSkill(context.Background(), "reviewer", "s1")
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

	err := svc.DeleteSkill(context.Background(), "reviewer")
	bizErr, ok := err.(*service.BizError)
	if !ok || bizErr.Code != 409 {
		t.Fatalf("err = %#v, want 409 BizError", err)
	}
	if skillDao.deleteCalled {
		t.Fatal("DeleteSkillCascade was called for an imported skill")
	}
}

func TestDeleteSkillMarksStorageErrorWhenPackageStoreUnavailable(t *testing.T) {
	skillDao := &fakeSkillDao{skill: &model.SkillHub{
		Name: "reviewer", Builtin: false, ObjectKey: "skills/reviewer/hash.zip", StorageType: model.SkillStorageMinIO,
	}}
	svc := NewSkillService(skillDao, &fakeSkillSessionDao{}, &fakeSkillAgentClient{})

	err := svc.DeleteSkill(context.Background(), "reviewer")
	bizErr, ok := err.(*service.BizError)
	if !ok || bizErr.Code != 503 {
		t.Fatalf("err = %#v, want 503 BizError", err)
	}
	if len(skillDao.statusUpdates) != 1 || skillDao.statusUpdates[0] != model.SkillStatusStorageError {
		t.Fatalf("status updates = %v, want storage_error", skillDao.statusUpdates)
	}
}

func TestImportSkillRejectsBuiltinSkill(t *testing.T) {
	client := &fakeSkillAgentClient{}
	svc := NewSkillService(
		&fakeSkillDao{skill: &model.SkillHub{Name: "builtin", Builtin: true}},
		&fakeSkillSessionDao{session: &model.Session{SessionID: "s1", AgentType: "codex"}},
		client,
	)

	err := func() error {
		_, importErr := svc.ImportSkill(context.Background(), "builtin", "s1")
		return importErr
	}()
	bizErr, ok := err.(*service.BizError)
	if !ok || bizErr.Code != 403 {
		t.Fatalf("err = %#v, want 403 BizError", err)
	}
	if client.installCalls != 0 {
		t.Fatalf("install calls = %d, want 0", client.installCalls)
	}
}

func TestUploadSkillReturnsValidationResultForInvalidZip(t *testing.T) {
	svc := NewSkillService(&fakeSkillDao{}, &fakeSkillSessionDao{}, &fakeSkillAgentClient{})

	result, err := svc.UploadSkill(context.Background(), "bad.zip", []byte("not a zip"))
	if err != nil {
		t.Fatalf("UploadSkill error = %v, want validation result", err)
	}
	if result == nil || result.Valid {
		t.Fatalf("result = %#v, want invalid validation result", result)
	}
}

func TestNormalizeSkillNameRejectsControlCharacters(t *testing.T) {
	for _, name := range []string{"demo\nname", "demo\x00name", "demo\x7fname"} {
		if _, err := normalizeSkillName(name); err == nil {
			t.Fatalf("normalizeSkillName(%q) accepted a control character", name)
		}
	}
}

func TestAcquireSkillUploadBoundsMultipartAdmission(t *testing.T) {
	svc := NewSkillService(&fakeSkillDao{}, &fakeSkillSessionDao{}, &fakeSkillAgentClient{})
	svc.SetValidationLimits(1, time.Second)
	release, err := svc.AcquireSkillUpload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if nextRelease, err := svc.AcquireSkillUpload(ctx); !errors.Is(err, context.Canceled) || nextRelease != nil {
		if nextRelease != nil {
			nextRelease()
		}
		t.Fatalf("canceled admission returned err=%v, want context.Canceled", err)
	}
}

func TestSkillStorageReadFailureQueuesDurableVerification(t *testing.T) {
	jobs := &fakeSkillOperationDao{}
	svc := NewSkillService(&fakeSkillDao{}, &fakeSkillSessionDao{}, &fakeSkillAgentClient{})
	svc.SetOperationDao(jobs)
	svc.queueSkillObjectVerification(&model.SkillHub{
		ID: 7, Name: "demo", ObjectKey: "skills/demo/hash.zip", SHA256: "hash",
	}, errors.New("object missing"))
	if len(jobs.jobs) != 1 {
		t.Fatalf("verification jobs = %d, want 1", len(jobs.jobs))
	}
	job := jobs.jobs[0]
	if job.Operation != model.SkillOperationVerifyObject || job.SkillID == nil || *job.SkillID != 7 || job.ObjectKey != "skills/demo/hash.zip" {
		t.Fatalf("unexpected verification job: %+v", job)
	}
	if job.IdempotencyKey != "verify-object:7:hash" || job.LastError != "object missing" {
		t.Fatalf("verification job fence/error = %q/%q", job.IdempotencyKey, job.LastError)
	}
}

func TestValidateSkillUploadIDAcceptsOpaqueSafeIDs(t *testing.T) {
	for _, id := range []string{"550e8400-e29b-41d4-a716-446655440000", "01J8Z5Y8W3M4N6P7Q8R9S0T1V2"} {
		if err := service.ValidateSkillUploadID(id); err != nil {
			t.Fatalf("ValidateSkillUploadID(%q): %v", id, err)
		}
	}
	for _, id := range []string{"", "short", "../upload", "upload id", "upload/part"} {
		if err := service.ValidateSkillUploadID(id); err == nil {
			t.Fatalf("ValidateSkillUploadID(%q) accepted unsafe ID", id)
		}
	}
}

func TestUploadAndConfirmSkillUsesIncomingAndContentAddressedObjects(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatalf("create skill zip: %v", err)
	}
	if _, err := entry.Write([]byte("---\nname: demo\ndescription: test\n---\n")); err != nil {
		t.Fatalf("write skill zip: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close skill zip: %v", err)
	}

	store := package_store.NewMemoryStore()
	skillDao := &fakeSkillDao{}
	svc := NewSkillService(skillDao, &fakeSkillSessionDao{}, &fakeSkillAgentClient{}, store)
	ownerCtx := service.WithSkillOwner(context.Background(), "admin-1")
	result, err := svc.UploadSkill(ownerCtx, "demo.zip", buf.Bytes())
	if err != nil {
		t.Fatalf("UploadSkill: %v", err)
	}
	if !result.Valid || result.UploadID == "" || result.TmpDir != "" || result.StorageType != model.SkillStorageMinIO {
		t.Fatalf("unexpected upload result: %+v", result)
	}

	stagingKey := "incoming/" + result.UploadID + ".zip"
	if _, err := store.Stat(context.Background(), stagingKey); err != nil {
		t.Fatalf("staging object missing: %v", err)
	}
	if _, err := svc.ConfirmSkill(ownerCtx, "demo", "client text must be ignored", 999, 999, "minio:"+stagingKey); err != nil {
		t.Fatalf("ConfirmSkill: %v", err)
	}
	if skillDao.createdSkill == nil || skillDao.createdSkill.UploadedBy != "admin-1" {
		t.Fatalf("created Skill UploadedBy = %q, want admin-1", skillDao.createdSkill.UploadedBy)
	}
	if _, err := store.Stat(context.Background(), stagingKey); err == nil {
		t.Fatal("staging object still exists after confirmation")
	}
	finalKey := "skills/demo/" + result.SHA256 + ".zip"
	if info, err := store.Stat(context.Background(), finalKey); err != nil || info.SHA256 != result.SHA256 {
		t.Fatalf("final object = %+v, err = %v", info, err)
	}
}

func TestUploadSkillFileStreamsValidationAndCanonicalPackage(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatalf("create skill zip: %v", err)
	}
	if _, err := entry.Write([]byte("---\nname: streamed\ndescription: file path\n---\n")); err != nil {
		t.Fatalf("write skill zip: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close skill zip: %v", err)
	}
	input := filepath.Join(t.TempDir(), "streamed.zip")
	if err := os.WriteFile(input, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	store := package_store.NewMemoryStore()
	svc := NewSkillService(&fakeSkillDao{}, &fakeSkillSessionDao{}, &fakeSkillAgentClient{}, store)
	result, err := svc.UploadSkillFile(context.Background(), "streamed.zip", input, int64(buf.Len()))
	if err != nil {
		t.Fatalf("UploadSkillFile: %v", err)
	}
	if result == nil || !result.Valid || result.UploadID == "" || result.TmpDir != "" {
		t.Fatalf("unexpected streamed upload result: %+v", result)
	}
	object, err := store.Open(context.Background(), "incoming/"+result.UploadID+".zip")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := io.ReadAll(object)
	_ = object.Close()
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(canonical), int64(len(canonical)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range archive.File {
		if strings.HasPrefix(filepath.Base(entry.Name), ".canonical-") {
			t.Fatalf("canonical output was recursively included: %q", entry.Name)
		}
	}
}

func TestLegacyDBConfirmUsesConfiguredPrivateTempRoot(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entry, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatalf("create skill zip: %v", err)
	}
	if _, err := entry.Write([]byte("---\nname: legacy\ndescription: db path\n---\n")); err != nil {
		t.Fatalf("write skill zip: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close skill zip: %v", err)
	}
	input := filepath.Join(t.TempDir(), "legacy.zip")
	if err := os.WriteFile(input, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write input: %v", err)
	}
	privateRoot := filepath.Join(t.TempDir(), "skill-tmp")
	if err := service.EnsureSkillTempRoot(privateRoot); err != nil {
		t.Fatalf("EnsureSkillTempRoot: %v", err)
	}
	skillDao := &fakeSkillDao{}
	svc := NewSkillService(skillDao, &fakeSkillSessionDao{}, &fakeSkillAgentClient{})
	svc.SetSkillTempDir(privateRoot)
	result, err := svc.UploadSkillFile(context.Background(), "legacy.zip", input, int64(buf.Len()))
	if err != nil {
		t.Fatalf("UploadSkillFile: %v", err)
	}
	if result == nil || !result.Valid || result.TmpDir == "" || !strings.HasPrefix(filepath.Clean(result.TmpDir), filepath.Clean(privateRoot)+string(filepath.Separator)) {
		t.Fatalf("unexpected DB upload result: %+v", result)
	}
	if _, err := svc.ConfirmSkill(context.Background(), result.Name, result.Description, result.FileCount, result.TotalSize, result.TmpDir); err != nil {
		t.Fatalf("ConfirmSkill: %v", err)
	}
	if skillDao.createdSkill == nil || skillDao.createdSkill.StorageType != model.SkillStorageDB {
		t.Fatalf("created Skill = %+v, want DB storage", skillDao.createdSkill)
	}
}

func TestUploadSkillRejectsExecutableByContentPolicy(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	md, err := zw.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := md.Write([]byte("---\nname: executable\n---\n")); err != nil {
		t.Fatal(err)
	}
	header := &zip.FileHeader{Name: "run.sh", Method: zip.Deflate}
	header.SetMode(0o755)
	script, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := script.Write([]byte("#!/bin/sh\necho unsafe\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	svc := NewSkillService(&fakeSkillDao{}, &fakeSkillSessionDao{}, &fakeSkillAgentClient{}, package_store.NewMemoryStore())
	svc.SetContentPolicy(false, true, nil)
	_, err = svc.UploadSkill(context.Background(), "executable.zip", buf.Bytes())
	bizErr, ok := err.(*service.BizError)
	if !ok || bizErr.Code != 400 {
		t.Fatalf("error = %#v, want executable policy 400", err)
	}
}
