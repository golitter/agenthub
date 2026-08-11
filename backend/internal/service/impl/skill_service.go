package impl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/package_store"
	"agenthub/backend/pkg/skill_upload_session"

	"github.com/google/uuid"
)

type skillAgentClient interface {
	InstallSkill(agentType, sessionID, skillName string, zipData []byte) error
	RemoveSkill(agentType, sessionID, skillName string) error
}

type contextSkillAgentClient interface {
	InstallSkillWithContext(ctx context.Context, agentType, sessionID, skillName string, zipData []byte) error
	RemoveSkillWithContext(ctx context.Context, agentType, sessionID, skillName string) error
}

type skillReceiptTransactionStore interface {
	CreateSkillWithReceipt(skill model.SkillHub, receipt model.SkillUploadReceipt) (*model.SkillHub, error)
	CreateReceiptForExistingSkill(name string, receipt model.SkillUploadReceipt) (*model.SkillHub, error)
}

// skillReceiptAuditTransactionStore is the production capability used by the
// MinIO confirmation path.  The Hub row, durable receipt, and success audit
// event must commit or roll back together; otherwise a confirmation could
// become visible without the required provenance record.
type skillReceiptAuditTransactionStore interface {
	CreateSkillWithReceiptAndAudit(skill model.SkillHub, receipt model.SkillUploadReceipt, event model.SkillAuditEvent) (*model.SkillHub, error)
	CreateReceiptForExistingSkillAndAudit(name string, receipt model.SkillUploadReceipt, event model.SkillAuditEvent) (*model.SkillHub, error)
}

type skillAuditTransactionStore interface {
	CreateSkillAndAudit(skill model.SkillHub, event model.SkillAuditEvent) error
}

type skillAuditStore interface {
	CreateSkillAuditEvent(event model.SkillAuditEvent) error
}

type skillStatusStore interface {
	UpdateSkillStatus(name, status string) error
}

// skillHashBackfillStore persists the first verified digest for historical
// BLOB rows that predate the storage metadata migration.  It is optional so
// compatibility test DAOs and read-only adapters can still validate bytes;
// the production GORM DAO implements it.
type skillHashBackfillStore interface {
	UpdateSkillContentAndMetadata(id uint, content []byte, sha string, packageSize int64) error
}

type skillInstallRollbackStore interface {
	RollbackAgentSkillInstall(agentSkillID uint, jobID uint64, leaseToken string) error
}

type skillRemoveRollbackStore interface {
	RollbackAgentSkillRemove(agentSkillID uint, jobID uint64, leaseToken string) error
}

// skillRemoveCompletionStore atomically removes the AgentSkill relation, its
// claimed remove outbox row, and the success audit event.  The optional
// capability keeps compatibility DAOs usable while production GORM closes the
// final relation/task crash window in one transaction.
type skillRemoveCompletionStore interface {
	CompleteAgentSkillRemovalByID(agentSkillID uint, jobID uint64, leaseToken string, event model.SkillAuditEvent) error
}

// skillDeleteCompletionStore atomically deletes a Hub row, its receipts and
// the claimed delete task, including the audit event.  Object deletion has
// already completed (and is idempotent) before this transaction is entered.
type skillDeleteCompletionStore interface {
	DeleteSkillCascadeWithOperation(name string, jobID uint64, leaseToken string, event model.SkillAuditEvent) error
}

type agentSkillByIDDeleteStore interface {
	DeleteAgentSkillByID(id uint) error
}

type skillObjectRepairQueue interface {
	CreateSkillOperationJob(job model.SkillOperationJob) (*model.SkillOperationJob, error)
}

type skillOperationReservationStore interface {
	CreateAgentSkillAndOperation(skill model.AgentSkill, job model.SkillOperationJob) (*model.SkillOperationJob, error)
	UpdateAgentSkillStatusAndOperation(sessionID, skillName, status string, job model.SkillOperationJob) (*model.SkillOperationJob, error)
	UpdateSkillStatusAndOperation(name, status string, job model.SkillOperationJob) (*model.SkillOperationJob, error)
}

type agentSkillStatusLookup interface {
	GetAgentSkill(sessionID, skillName string) (*model.AgentSkill, error)
}

type SkillService struct {
	skillDao              dao.SkillDao
	sessionDao            dao.SessionDao
	agentClient           skillAgentClient
	packageStore          package_store.PackageStore
	uploadStore           *skill_upload_session.Store
	operationDao          dao.SkillOperationDao
	tempDir               string
	uploadSem             chan struct{}
	validationSem         chan struct{}
	validationTimeout     time.Duration
	readPreference        string
	shadowWriteBlob       bool
	allowLegacyTmpConfirm bool
	rejectBinaries        bool
	rejectExecutables     bool
	contentScanner        service.SkillContentScanner
	zipLimits             service.ZipLimits
	tempMinFreeBytes      int64
}

const maxSkillNameLen = service.MaxSkillNameLength

func (svc *SkillService) SetUploadSessionStore(store *skill_upload_session.Store) {
	svc.uploadStore = store
}

func (svc *SkillService) SetOperationDao(operationDao dao.SkillOperationDao) {
	svc.operationDao = operationDao
}

func (svc *SkillService) SetSkillTempDir(tempDir string) {
	svc.tempDir = strings.TrimSpace(tempDir)
}

func (svc *SkillService) SetTempMinFreeBytes(minimum int64) {
	if minimum > 0 {
		svc.tempMinFreeBytes = minimum
	}
}

func (svc *SkillService) tempFreeSpaceMinimum() int64 {
	if svc.tempMinFreeBytes > 0 {
		return svc.tempMinFreeBytes
	}
	return service.MinSkillTempFreeBytes
}

func (svc *SkillService) SetValidationLimits(concurrency int, timeout time.Duration) {
	if concurrency > 0 {
		// Admission covers the multipart bytes written by the controller before
		// validation starts.  Keeping it separate from validationSem avoids a
		// request holding the validation slot while it waits for its own upload
		// admission, while still bounding the total staging footprint.
		svc.uploadSem = make(chan struct{}, concurrency)
		svc.validationSem = make(chan struct{}, concurrency)
	}
	svc.validationTimeout = timeout
}

// AcquireSkillUpload limits the complete upload handler, including the
// multipart copy into the private staging volume.  The validator semaphore is
// acquired later by UploadSkillFile; this separate gate prevents several
// legal-size requests from filling the temp volume before validation gets a
// chance to enforce its own quota.
func (svc *SkillService) AcquireSkillUpload(ctx context.Context) (func(), error) {
	if svc == nil || svc.uploadSem == nil {
		return func() {}, nil
	}
	select {
	case svc.uploadSem <- struct{}{}:
		return func() { <-svc.uploadSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// CheckSkillTempSpace exposes the same preflight used by the service so the
// controller can reject a request before writing its raw multipart body.
func (svc *SkillService) CheckSkillTempSpace() error {
	if svc == nil {
		return errors.New("Skill service is unavailable")
	}
	return service.CheckSkillTempFreeSpace(svc.tempDir, svc.tempFreeSpaceMinimum())
}

func (svc *SkillService) SetZipLimits(limits service.ZipLimits) {
	svc.zipLimits = service.NormalizeZipLimits(limits)
}

func (svc *SkillService) SkillUploadLimit() int64 {
	if svc.zipLimits.UploadSize > 0 {
		return svc.zipLimits.UploadSize
	}
	return service.MaxUploadSize
}

func (svc *SkillService) SetStorageReadOptions(readPreference string, shadowWriteBlob bool) {
	if readPreference == "db" || readPreference == "minio" {
		svc.readPreference = readPreference
	}
	svc.shadowWriteBlob = shadowWriteBlob
}

func (svc *SkillService) SetLegacyTmpConfirmAllowed(allowed bool) {
	svc.allowLegacyTmpConfirm = allowed
}

func (svc *SkillService) SetContentPolicy(rejectBinaries, rejectExecutables bool, scanner service.SkillContentScanner) {
	svc.rejectBinaries = rejectBinaries
	svc.rejectExecutables = rejectExecutables
	svc.contentScanner = scanner
}

func (svc *SkillService) enforceContentPolicy(ctx context.Context, result *service.ValidationResult, extractedDir string) error {
	if result == nil {
		return service.ErrBadRequest("invalid Skill validation result")
	}
	if svc.rejectBinaries && result.ContainsBinary {
		return service.ErrBadRequest("Skill package contains a binary file and is rejected by policy")
	}
	if svc.rejectExecutables && result.ContainsExecutable {
		return service.ErrBadRequest("Skill package contains an executable file and is rejected by policy")
	}
	if svc.contentScanner != nil {
		if err := svc.contentScanner.Scan(ctx, extractedDir); err != nil {
			return service.ErrServiceUnavailable("Skill content scan failed")
		}
	}
	return nil
}

func (svc *SkillService) beginValidation(ctx context.Context) (context.Context, func(), error) {
	if svc.validationSem != nil {
		select {
		case svc.validationSem <- struct{}{}:
		case <-ctx.Done():
			return nil, func() {}, ctx.Err()
		}
	}
	release := func() {
		if svc.validationSem != nil {
			<-svc.validationSem
		}
	}
	if svc.validationTimeout > 0 {
		validationCtx, cancel := context.WithTimeout(ctx, svc.validationTimeout)
		return validationCtx, func() { cancel(); release() }, nil
	}
	return ctx, release, nil
}

const skillOperationLease = 5 * time.Minute
const skillStorageOperationTimeout = 2 * time.Minute

func skillStorageContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, skillStorageOperationTimeout)
}

func (svc *SkillService) startOperation(job model.SkillOperationJob) (*model.SkillOperationJob, error) {
	if svc.operationDao == nil {
		return nil, nil
	}
	created, err := svc.operationDao.CreateSkillOperationJob(job)
	if err != nil {
		return nil, err
	}
	return svc.claimOperation(created)
}

func (svc *SkillService) claimOperation(created *model.SkillOperationJob) (*model.SkillOperationJob, error) {
	if created == nil {
		return nil, service.ErrInternal("skill operation job was not created")
	}
	if created.Status == model.SkillJobStatusDone {
		return created, nil
	}
	claimed, err := svc.operationDao.ClaimSkillOperationJob(created.ID, time.Now(), skillOperationLease)
	if err != nil {
		return nil, err
	}
	if claimed == nil {
		return nil, service.ErrAccepted("skill operation is already in progress; retry later")
	}
	return claimed, nil
}

func (svc *SkillService) completeOperation(job *model.SkillOperationJob) {
	if svc.operationDao != nil && job != nil {
		if err := svc.operationDao.CompleteSkillOperationJob(job.ID, job.LeaseToken); err != nil {
			slog.Warn("complete skill operation job failed", "job_id", job.ID, "error", err)
		}
	}
}

func (svc *SkillService) deleteOperationJob(job *model.SkillOperationJob) {
	if svc.operationDao != nil && job != nil {
		if err := svc.operationDao.DeleteSkillOperationJob(job.ID, job.LeaseToken); err != nil {
			slog.Warn("delete completed skill operation job failed", "job_id", job.ID, "error", err)
		}
	}
}

func (svc *SkillService) auditSkill(event model.SkillAuditEvent) {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	if audit, ok := svc.skillDao.(skillAuditStore); ok {
		if err := audit.CreateSkillAuditEvent(event); err != nil {
			slog.Warn("write Skill audit event failed", "action", event.Action, "skill", event.SkillName, "error", err)
		}
	}
}

// auditSkillStrict is used at externally visible mutation boundaries.  Test
// and compatibility DAOs that do not expose an audit store retain their old
// behavior, while the production GORM DAO surfaces a database failure to the
// caller instead of silently acknowledging an unaudited action.
func (svc *SkillService) auditSkillStrict(event model.SkillAuditEvent) error {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	if audit, ok := svc.skillDao.(skillAuditStore); ok {
		return audit.CreateSkillAuditEvent(event)
	}
	return nil
}

// queueSkillObjectVerification records a durable repair attempt whenever the
// authoritative MinIO object cannot be read or fails its integrity check.
// The deterministic key makes repeated import attempts reopen/reuse the same
// verification job instead of creating an unbounded stream of duplicates.
func (svc *SkillService) queueSkillObjectVerification(skill *model.SkillHub, cause error) {
	if svc == nil || skill == nil || svc.operationDao == nil || skill.ID == 0 || skill.ObjectKey == "" {
		return
	}
	queue, ok := svc.operationDao.(skillObjectRepairQueue)
	if !ok {
		return
	}
	skillID := skill.ID
	job := model.SkillOperationJob{
		Operation:      model.SkillOperationVerifyObject,
		IdempotencyKey: fmt.Sprintf("verify-object:%d:%s", skill.ID, skill.SHA256),
		SkillID:        &skillID,
		SkillName:      skill.Name,
		ObjectKey:      skill.ObjectKey,
	}
	if cause != nil {
		job.LastError = cause.Error()
	}
	if _, err := queue.CreateSkillOperationJob(job); err != nil {
		slog.Warn("queue Skill object verification failed", "skill", skill.Name, "object_key", skill.ObjectKey, "error", err)
	}
}

func (svc *SkillService) markSkillStorageReadFailure(skill *model.SkillHub, cause error) {
	if skill == nil {
		return
	}
	svc.queueSkillObjectVerification(skill, cause)
	if statusStore, ok := svc.skillDao.(skillStorageStatusAuditStore); ok {
		if err := statusStore.UpdateSkillStatusIfNotDeletingWithAudit(skill.ID, model.SkillStatusStorageError, model.SkillAuditEvent{
			Action: "reconcile", Outcome: model.SkillStatusStorageError, SkillID: &skill.ID,
			SkillName: skill.Name, ObjectKey: skill.ObjectKey, SHA256: skill.SHA256,
			Error: causeString(cause),
		}); err != nil {
			slog.Warn("mark Skill storage error failed", "skill", skill.Name, "error", err)
		}
		return
	}
	if statusStore, ok := svc.skillDao.(skillStorageErrorStore); ok {
		if err := statusStore.UpdateSkillStatusIfNotDeleting(skill.Name, model.SkillStatusStorageError); err != nil {
			slog.Warn("mark Skill storage error failed", "skill", skill.Name, "error", err)
		}
		return
	}
	if statusStore, ok := svc.skillDao.(skillStatusStore); ok {
		if err := statusStore.UpdateSkillStatus(skill.Name, model.SkillStatusStorageError); err != nil {
			slog.Warn("mark Skill storage error failed", "skill", skill.Name, "error", err)
		}
	}
}

func causeString(cause error) string {
	if cause == nil {
		return "skill package read failed"
	}
	return cause.Error()
}

func skillFilesJSON(files []string) string {
	if len(files) == 0 {
		return ""
	}
	data, err := json.Marshal(files)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeSkillFiles(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var files []string
	if err := json.Unmarshal([]byte(raw), &files); err != nil {
		return nil
	}
	return files
}

func (svc *SkillService) retryOperation(job *model.SkillOperationJob, operationErr error) {
	if svc.operationDao != nil && job != nil {
		if err := svc.operationDao.RetrySkillOperationJob(job.ID, job.LeaseToken, operationErr.Error(), time.Now().Add(time.Minute)); err != nil {
			slog.Warn("retry skill operation job failed", "job_id", job.ID, "error", err)
		}
	}
}

// knownSkillAgentFailure is implemented by the AgentEnd HTTP client for
// deterministic 4xx/application rejections. Network errors and 5xx responses
// deliberately do not implement it because the filesystem result is unknown
// and must remain in sync_error/retry state.
type knownSkillAgentFailure interface {
	KnownFailure() bool
}

func isKnownSkillAgentFailure(err error) bool {
	if err == nil {
		return false
	}
	var marker knownSkillAgentFailure
	return errors.As(err, &marker) && marker.KnownFailure()
}

// rollbackKnownAgentSkillInstall is the terminal path for an explicit
// AgentEnd rejection. Production GORM uses one transaction for the relation
// and claimed job; compatibility DAOs fall back to fenced identity deletion.
func rollbackKnownAgentSkillInstall(skillDao dao.SkillDao, operationDao dao.SkillOperationDao, job *model.SkillOperationJob) error {
	if job == nil {
		return nil
	}
	if job.AgentSkillID != nil {
		if rollback, ok := skillDao.(skillInstallRollbackStore); ok {
			return rollback.RollbackAgentSkillInstall(*job.AgentSkillID, job.ID, job.LeaseToken)
		}
		if deleter, ok := skillDao.(agentSkillByIDDeleteStore); ok {
			if err := deleter.DeleteAgentSkillByID(*job.AgentSkillID); err != nil {
				return err
			}
		} else if err := skillDao.DeleteAgentSkill(job.SessionID, job.SkillName); err != nil {
			return err
		}
	} else if err := skillDao.DeleteAgentSkill(job.SessionID, job.SkillName); err != nil {
		return err
	}
	if operationDao != nil {
		return operationDao.DeleteSkillOperationJob(job.ID, job.LeaseToken)
	}
	return nil
}

// restoreKnownAgentSkillRemove is the terminal path for an explicit AgentEnd
// removal rejection. The relation remains a valid ready reservation and the
// claimed remove task must not be retried against it. Production GORM performs
// both changes in one transaction; compatibility DAOs use the fenced row ID
// when available and otherwise fall back to the session/name pair.
func restoreKnownAgentSkillRemove(skillDao dao.SkillDao, operationDao dao.SkillOperationDao, job *model.SkillOperationJob, sessionID, skillName string) error {
	if job != nil && job.AgentSkillID != nil {
		if rollback, ok := skillDao.(skillRemoveRollbackStore); ok {
			return rollback.RollbackAgentSkillRemove(*job.AgentSkillID, job.ID, job.LeaseToken)
		}
		if statusStore, ok := skillDao.(agentSkillStatusByIDStore); ok {
			if _, err := statusStore.UpdateAgentSkillStatusByID(*job.AgentSkillID, model.AgentSkillStatusReady); err != nil {
				return err
			}
		} else if err := skillDao.UpdateAgentSkillStatus(sessionID, skillName, model.AgentSkillStatusReady); err != nil {
			return err
		}
	} else if err := skillDao.UpdateAgentSkillStatus(sessionID, skillName, model.AgentSkillStatusReady); err != nil {
		return err
	}
	if operationDao != nil && job != nil {
		return operationDao.DeleteSkillOperationJob(job.ID, job.LeaseToken)
	}
	return nil
}

func NewSkillService(skillDao dao.SkillDao, sessionDao dao.SessionDao, agentClient skillAgentClient, stores ...package_store.PackageStore) *SkillService {
	var packageStore package_store.PackageStore
	if len(stores) > 0 {
		packageStore = stores[0]
	}
	return &SkillService{
		skillDao:     skillDao,
		sessionDao:   sessionDao,
		agentClient:  agentClient,
		packageStore: packageStore,
		zipLimits:    service.DefaultZipLimits(),
		// Keep compatibility enabled for directly-constructed services; the
		// application overrides this with the migration gate from config.
		allowLegacyTmpConfirm: true,
	}
}

func (svc *SkillService) UploadSkill(ctx context.Context, filename string, zipData []byte) (*service.ValidationResult, error) {
	validationCtx, release, err := svc.beginValidation(ctx)
	if err != nil {
		return nil, service.ErrServiceUnavailable("skill validation cancelled")
	}
	defer release()
	ctx = validationCtx
	if err := service.CheckSkillTempFreeSpace(svc.tempDir, svc.tempFreeSpaceMinimum()); err != nil {
		return nil, service.ErrServiceUnavailable("Skill temporary volume is low on space")
	}
	limits := service.NormalizeZipLimits(svc.zipLimits)
	if int64(len(zipData)) > limits.UploadSize {
		return &service.ValidationResult{Valid: false, Errors: []string{"file size exceeds configured upload limit"}}, nil
	}
	result, tmpDir, err := service.ValidateZipReaderAtContextWithLimits(validationCtx, bytes.NewReader(zipData), int64(len(zipData)), svc.tempDir, limits)
	if validationCtx.Err() != nil {
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
		return nil, service.ErrServiceUnavailable("skill validation timed out or was cancelled")
	}
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
	normalizedName, nameErr := normalizeSkillName(result.Name)
	if nameErr != nil {
		_ = os.RemoveAll(tmpDir)
		return &service.ValidationResult{Valid: false, Errors: []string{nameErr.Error()}}, nil
	}
	result.Name = normalizedName

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
	if err := svc.enforceContentPolicy(ctx, result, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, err
	}

	if svc.packageStore != nil {
		storageCtx, cancelStorage := skillStorageContext(ctx)
		defer cancelStorage()
		canonicalFile, err := os.CreateTemp(svc.tempDir, ".canonical-*.zip")
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, service.ErrInternal("create canonical skill package failed")
		}
		canonicalPath := canonicalFile.Name()
		_ = canonicalFile.Close()
		defer os.Remove(canonicalPath)
		canonicalSize, sha, err := service.PackValidatedSkillDirToFileInRootContextWithLimit(validationCtx, result.Name, tmpDir, canonicalPath, svc.tempDir, limits.PackageSize)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, err
		}
		if canonicalSize > limits.PackageSize {
			_ = os.RemoveAll(tmpDir)
			return &service.ValidationResult{Valid: false, Errors: []string{"canonical package exceeds 12MB limit"}}, nil
		}
		canonicalCheck, err := os.Open(canonicalPath)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, service.ErrInternal("open canonical skill package for validation failed")
		}
		canonicalValidationErr := service.ValidateCanonicalSkillPackageContext(validationCtx, canonicalCheck, canonicalSize, svc.tempDir, result.Name, limits)
		_ = canonicalCheck.Close()
		if canonicalValidationErr != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, canonicalPackageError(canonicalValidationErr)
		}
		uploadID := uuid.NewString()
		objectKey := "incoming/" + uploadID + ".zip"
		canonicalReader, err := os.Open(canonicalPath)
		if err != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, service.ErrInternal("open canonical skill package failed")
		}
		putErr := svc.packageStore.Put(storageCtx, objectKey, canonicalReader, canonicalSize, sha)
		_ = canonicalReader.Close()
		if putErr != nil {
			_ = os.RemoveAll(tmpDir)
			return nil, service.ErrServiceUnavailable("store skill package failed")
		}
		// Write the durable audit row before publishing the Redis session.  If
		// the audit database is unavailable, no confirmer can observe this
		// upload yet, so deleting the incoming object is race-free.
		if err := svc.auditSkillStrict(model.SkillAuditEvent{
			Action: "upload", Outcome: "accepted", UploadID: uploadID, SkillName: result.Name,
			OwnerID: service.SkillOwnerFromContext(ctx), SHA256: sha, FilesJSON: skillFilesJSON(result.Files),
			ContainsExecutable: result.ContainsExecutable, ContainsBinary: result.ContainsBinary,
		}); err != nil {
			if deleteErr := svc.packageStore.Delete(storageCtx, objectKey); deleteErr != nil && !errors.Is(deleteErr, package_store.ErrNotFound) {
				slog.Warn("cleanup upload object after audit failure", "object_key", objectKey, "error", deleteErr)
			}
			_ = os.RemoveAll(tmpDir)
			return nil, service.ErrServiceUnavailable("record Skill upload audit failed")
		}
		if svc.uploadStore != nil {
			if err := svc.uploadStore.Create(storageCtx, skill_upload_session.Session{
				UploadID: uploadID, OwnerID: service.SkillOwnerFromContext(ctx), ObjectKey: objectKey,
				Name: result.Name, Description: result.Description, SHA256: sha,
				FileCount: result.FileCount, TotalSize: result.TotalSize, PackageSize: canonicalSize,
			}); err != nil {
				if deleteErr := svc.packageStore.Delete(storageCtx, objectKey); deleteErr != nil && !errors.Is(deleteErr, package_store.ErrNotFound) {
					slog.Warn("cleanup upload object after session failure", "object_key", objectKey, "error", deleteErr)
				}
				_ = os.RemoveAll(tmpDir)
				return nil, service.ErrServiceUnavailable("create skill upload session failed")
			}
		}
		_ = os.RemoveAll(tmpDir)
		result.TmpDir = ""
		result.UploadID = uploadID
		result.UploadedBy = service.SkillOwnerFromContext(ctx)
		result.ObjectKey = objectKey
		result.SHA256 = sha
		result.PackageSize = canonicalSize
		result.StorageType = model.SkillStorageMinIO
	}

	if result.UploadedBy == "" {
		result.UploadedBy = service.SkillOwnerFromContext(ctx)
	}
	return result, nil
}

func (svc *SkillService) UploadSkillFile(ctx context.Context, filename, path string, size int64) (*service.ValidationResult, error) {
	validationCtx, release, err := svc.beginValidation(ctx)
	if err != nil {
		return nil, service.ErrServiceUnavailable("skill validation cancelled")
	}
	defer release()
	ctx = validationCtx
	if err := service.CheckSkillTempFreeSpace(svc.tempDir, svc.tempFreeSpaceMinimum()); err != nil {
		return nil, service.ErrServiceUnavailable("Skill temporary volume is low on space")
	}
	limits := service.NormalizeZipLimits(svc.zipLimits)
	if size <= 0 || size > limits.UploadSize {
		return &service.ValidationResult{Valid: false, Errors: []string{"file size exceeds configured upload limit"}}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, service.ErrInternal("open uploaded skill package failed")
	}
	defer file.Close()
	result, tmpDir, validateErr := service.ValidateZipReaderAtContextWithLimits(validationCtx, file, size, svc.tempDir, limits)
	if validationCtx.Err() != nil {
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
		return nil, service.ErrServiceUnavailable("skill validation timed out or was cancelled")
	}
	if validateErr != nil {
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
	normalizedName, nameErr := normalizeSkillName(result.Name)
	if nameErr != nil {
		_ = os.RemoveAll(tmpDir)
		return &service.ValidationResult{Valid: false, Errors: []string{nameErr.Error()}}, nil
	}
	result.Name = normalizedName
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
		return &service.ValidationResult{Valid: false, Errors: []string{"name conflicts with builtin skill"}}, nil
	}
	if err := svc.enforceContentPolicy(validationCtx, result, tmpDir); err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, err
	}
	if svc.packageStore == nil {
		// Legacy DB mode keeps the validated directory for the confirm request;
		// only the original upload file is removed by the controller.
		result.UploadedBy = service.SkillOwnerFromContext(ctx)
		return result, nil
	}
	storageCtx, cancelStorage := skillStorageContext(ctx)
	defer cancelStorage()
	// Keep the destination outside the extracted source tree; otherwise the
	// deterministic pack walk would include the output ZIP in itself.
	canonicalFile, err := os.CreateTemp(svc.tempDir, ".canonical-*.zip")
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, service.ErrInternal("create canonical skill package failed")
	}
	canonicalPath := canonicalFile.Name()
	defer os.Remove(canonicalPath)
	if err := canonicalFile.Close(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, service.ErrInternal("create canonical skill package failed")
	}
	canonicalSize, sha, err := service.PackValidatedSkillDirToFileInRootContextWithLimit(validationCtx, result.Name, tmpDir, canonicalPath, svc.tempDir, limits.PackageSize)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, err
	}
	if canonicalSize > limits.PackageSize {
		_ = os.RemoveAll(tmpDir)
		return &service.ValidationResult{Valid: false, Errors: []string{"canonical package exceeds 12MB limit"}}, nil
	}
	canonicalCheck, err := os.Open(canonicalPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, service.ErrInternal("open canonical skill package for validation failed")
	}
	canonicalValidationErr := service.ValidateCanonicalSkillPackageContext(validationCtx, canonicalCheck, canonicalSize, svc.tempDir, result.Name, limits)
	_ = canonicalCheck.Close()
	if canonicalValidationErr != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, canonicalPackageError(canonicalValidationErr)
	}
	uploadID := uuid.NewString()
	objectKey := "incoming/" + uploadID + ".zip"
	canonical, err := os.Open(canonicalPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, service.ErrInternal("open canonical skill package failed")
	}
	putErr := svc.packageStore.Put(storageCtx, objectKey, canonical, canonicalSize, sha)
	_ = canonical.Close()
	if putErr != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, service.ErrServiceUnavailable("store skill package failed")
	}
	// As in the byte-slice path, persist provenance before a Redis confirmer can
	// see the upload session.  The object is still unreferenced at this point,
	// so rollback cannot delete a concurrently promoted formal object.
	if err := svc.auditSkillStrict(model.SkillAuditEvent{
		Action: "upload", Outcome: "accepted", UploadID: uploadID, SkillName: result.Name,
		OwnerID: service.SkillOwnerFromContext(ctx), SHA256: sha, FilesJSON: skillFilesJSON(result.Files),
		ContainsExecutable: result.ContainsExecutable, ContainsBinary: result.ContainsBinary,
	}); err != nil {
		if deleteErr := svc.packageStore.Delete(storageCtx, objectKey); deleteErr != nil && !errors.Is(deleteErr, package_store.ErrNotFound) {
			slog.Warn("cleanup upload object after audit failure", "object_key", objectKey, "error", deleteErr)
		}
		_ = os.RemoveAll(tmpDir)
		return nil, service.ErrServiceUnavailable("record Skill upload audit failed")
	}
	if svc.uploadStore != nil {
		if err := svc.uploadStore.Create(storageCtx, skill_upload_session.Session{
			UploadID: uploadID, OwnerID: service.SkillOwnerFromContext(ctx), ObjectKey: objectKey,
			Name: result.Name, Description: result.Description, SHA256: sha,
			FileCount: result.FileCount, TotalSize: result.TotalSize, PackageSize: canonicalSize,
		}); err != nil {
			if deleteErr := svc.packageStore.Delete(storageCtx, objectKey); deleteErr != nil && !errors.Is(deleteErr, package_store.ErrNotFound) {
				slog.Warn("cleanup upload object after session failure", "object_key", objectKey, "error", deleteErr)
			}
			_ = os.RemoveAll(tmpDir)
			return nil, service.ErrServiceUnavailable("create skill upload session failed")
		}
	}
	_ = os.RemoveAll(tmpDir)
	result.TmpDir = ""
	result.UploadID = uploadID
	result.UploadedBy = service.SkillOwnerFromContext(ctx)
	result.ObjectKey = objectKey
	result.SHA256 = sha
	result.PackageSize = canonicalSize
	result.StorageType = model.SkillStorageMinIO
	slog.Info("skill upload validated", "upload_id", uploadID, "skill", result.Name, "owner_id", service.SkillOwnerFromContext(ctx), "sha256", sha, "package_size", canonicalSize)
	return result, nil
}

func (svc *SkillService) ConfirmSkill(ctx context.Context, name, _ string, _ int, _ int64, tmpDir string) (*service.SkillImportResult, error) {
	if svc.packageStore != nil && strings.HasPrefix(tmpDir, "minio:") {
		if strings.TrimSpace(name) != "" {
			var err error
			name, err = normalizeSkillName(name)
			if err != nil {
				return nil, err
			}
		}
		return svc.confirmStoredSkill(ctx, name, strings.TrimPrefix(tmpDir, "minio:"))
	}
	if svc.packageStore != nil {
		if !svc.allowLegacyTmpConfirm {
			return nil, service.ErrBadRequest("legacy tmp_dir confirmation is disabled; upload_id is required")
		}
		return svc.confirmLegacyTmpDir(ctx, name, tmpDir)
	}
	name, err := normalizeSkillName(name)
	if err != nil {
		return nil, err
	}
	if svc.packageStore != nil {
		return nil, service.ErrBadRequest("skill upload receipt is required")
	}
	metadata, legacySystemTmp, err := svc.inspectLegacyConfirmDir(name, tmpDir)
	if err != nil {
		return nil, err
	}
	if err := svc.enforceContentPolicy(ctx, metadata, tmpDir); err != nil {
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

	limits := service.NormalizeZipLimits(svc.zipLimits)
	var zipData []byte
	if legacySystemTmp {
		// Compatibility uploads may still live under the historical system
		// temp root, but packing remains request-scoped and cancellable.
		zipData, err = service.PackValidatedSkillDirInRootContextWithLimit(ctx, name, tmpDir, os.TempDir(), limits.PackageSize)
	} else {
		zipData, err = service.PackValidatedSkillDirInRootContextWithLimit(ctx, name, tmpDir, svc.tempDir, limits.PackageSize)
	}
	if err != nil {
		return nil, err
	}
	if int64(len(zipData)) > limits.PackageSize {
		return nil, service.ErrBadRequest("canonical package exceeds configured package limit")
	}
	if err := service.ValidateCanonicalSkillPackageContext(ctx, bytes.NewReader(zipData), int64(len(zipData)), svc.tempDir, name, limits); err != nil {
		return nil, canonicalPackageError(err)
	}

	skillRecord := model.SkillHub{
		Name:               name,
		Builtin:            false,
		Description:        metadata.Description,
		FileCount:          metadata.FileCount,
		TotalSize:          metadata.TotalSize,
		Content:            zipData,
		SHA256:             sha256Hex(zipData),
		PackageSize:        int64(len(zipData)),
		StorageType:        model.SkillStorageDB,
		Status:             model.SkillStatusReady,
		UploadedBy:         service.SkillOwnerFromContext(ctx),
		FilesJSON:          skillFilesJSON(metadata.Files),
		ContainsExecutable: metadata.ContainsExecutable,
		ContainsBinary:     metadata.ContainsBinary,
	}
	confirmAudit := model.SkillAuditEvent{
		Action: "confirm", Outcome: "success", SkillName: name,
		OwnerID: service.SkillOwnerFromContext(ctx), SHA256: sha256Hex(zipData),
		FilesJSON: skillFilesJSON(metadata.Files), ContainsExecutable: metadata.ContainsExecutable,
		ContainsBinary: metadata.ContainsBinary,
	}
	if txStore, ok := svc.skillDao.(skillAuditTransactionStore); ok {
		if err := txStore.CreateSkillAndAudit(skillRecord, confirmAudit); err != nil {
			return nil, err
		}
	} else {
		if err := svc.skillDao.CreateSkill(skillRecord); err != nil {
			return nil, err
		}
		if err := svc.auditSkillStrict(confirmAudit); err != nil {
			return nil, service.ErrServiceUnavailable("record Skill confirmation audit failed")
		}
	}

	return &service.SkillImportResult{Success: true, Name: name}, nil
}

// inspectLegacyConfirmDir validates a compatibility confirmation directory
// against the configured private staging root.  A narrowly scoped fallback to
// the historical system temp root is retained only for uploads created before
// the private-root rollout; new uploads are always created under tempDir.
func (svc *SkillService) inspectLegacyConfirmDir(name, tmpDir string) (*service.ValidationResult, bool, error) {
	configuredRoot := strings.TrimSpace(svc.tempDir)
	if configuredRoot == "" {
		configuredRoot = os.TempDir()
	}
	metadata, err := service.InspectValidatedSkillDirInRoot(name, tmpDir, configuredRoot, svc.zipLimits)
	if err == nil {
		return metadata, false, nil
	}
	if filepath.Clean(configuredRoot) != filepath.Clean(os.TempDir()) {
		legacyMetadata, legacyErr := service.InspectValidatedSkillDirWithLimits(name, tmpDir, svc.zipLimits)
		if legacyErr == nil {
			return legacyMetadata, true, nil
		}
	}
	return nil, false, err
}

// confirmLegacyTmpDir bridges uploads created by a pre-MinIO Backend during
// the rolling-upgrade window.  The directory is never trusted as a final
// object: it is re-inspected, canonically packed, uploaded to incoming/, and
// then confirmed through the same Redis/receipt path as a new upload.
func (svc *SkillService) confirmLegacyTmpDir(ctx context.Context, name, tmpDir string) (*service.SkillImportResult, error) {
	name, err := normalizeSkillName(name)
	if err != nil {
		return nil, err
	}
	// During a rolling upgrade, an older Backend may have created the
	// compatibility directory under the historical system temp root.  Prefer
	// the configured private root, but accept that narrowly validated legacy
	// location as a compatibility bridge; new uploads never use this path.
	legacySystemTmp := false
	metadata, err := service.InspectValidatedSkillDirInRoot(name, tmpDir, svc.tempDir, svc.zipLimits)
	if err != nil && strings.TrimSpace(svc.tempDir) != "" && filepath.Clean(svc.tempDir) != filepath.Clean(os.TempDir()) {
		metadata, err = service.InspectValidatedSkillDirWithLimits(name, tmpDir, svc.zipLimits)
		legacySystemTmp = err == nil
	}
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	storageCtx, cancelStorage := skillStorageContext(ctx)
	defer cancelStorage()
	if err := svc.enforceContentPolicy(ctx, metadata, tmpDir); err != nil {
		return nil, err
	}
	var canonical []byte
	if legacySystemTmp {
		canonical, err = service.PackValidatedSkillDirInRootContextWithLimit(ctx, name, tmpDir, os.TempDir(), service.NormalizeZipLimits(svc.zipLimits).PackageSize)
	} else {
		canonical, err = service.PackValidatedSkillDirInRootContextWithLimit(ctx, name, tmpDir, svc.tempDir, service.NormalizeZipLimits(svc.zipLimits).PackageSize)
	}
	if err != nil {
		return nil, err
	}
	if int64(len(canonical)) > service.NormalizeZipLimits(svc.zipLimits).PackageSize {
		return nil, service.ErrBadRequest("canonical package exceeds configured package limit")
	}
	if err := service.ValidateCanonicalSkillPackageContext(ctx, bytes.NewReader(canonical), int64(len(canonical)), svc.tempDir, name, service.NormalizeZipLimits(svc.zipLimits)); err != nil {
		return nil, canonicalPackageError(err)
	}
	uploadID := uuid.NewString()
	objectKey := "incoming/" + uploadID + ".zip"
	sha := sha256Hex(canonical)
	if err := svc.packageStore.Put(storageCtx, objectKey, bytes.NewReader(canonical), int64(len(canonical)), sha); err != nil {
		return nil, service.ErrServiceUnavailable("store legacy skill package failed")
	}
	if err := svc.auditSkillStrict(model.SkillAuditEvent{
		Action: "upload", Outcome: "accepted", UploadID: uploadID, SkillName: name,
		OwnerID: service.SkillOwnerFromContext(ctx), SHA256: sha, FilesJSON: skillFilesJSON(metadata.Files),
		ContainsExecutable: metadata.ContainsExecutable, ContainsBinary: metadata.ContainsBinary,
	}); err != nil {
		if deleteErr := svc.packageStore.Delete(storageCtx, objectKey); deleteErr != nil && !errors.Is(deleteErr, package_store.ErrNotFound) {
			slog.Warn("cleanup legacy upload object after audit failure", "object_key", objectKey, "error", deleteErr)
		}
		return nil, service.ErrServiceUnavailable("record Skill upload audit failed")
	}
	if svc.uploadStore != nil {
		if err := svc.uploadStore.Create(storageCtx, skill_upload_session.Session{
			UploadID: uploadID, OwnerID: service.SkillOwnerFromContext(ctx), ObjectKey: objectKey,
			Name: name, Description: metadata.Description, SHA256: sha,
			FileCount: metadata.FileCount, TotalSize: metadata.TotalSize, PackageSize: int64(len(canonical)),
		}); err != nil {
			if deleteErr := svc.packageStore.Delete(storageCtx, objectKey); deleteErr != nil && !errors.Is(deleteErr, package_store.ErrNotFound) {
				slog.Warn("cleanup legacy upload object after session failure", "object_key", objectKey, "error", deleteErr)
			}
			return nil, service.ErrServiceUnavailable("create legacy skill upload session failed")
		}
	}
	return svc.confirmStoredSkill(ctx, name, objectKey)
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
			Name:               skill.Name,
			Builtin:            skill.Builtin,
			Description:        skill.Description,
			FileCount:          skill.FileCount,
			TotalSize:          skill.TotalSize,
			ImportCount:        importCount,
			CreatedAt:          skill.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UploadedBy:         skill.UploadedBy,
			SHA256:             skill.SHA256,
			StorageType:        skill.StorageType,
			Status:             skill.Status,
			Files:              decodeSkillFiles(skill.FilesJSON),
			ContainsExecutable: skill.ContainsExecutable,
			ContainsBinary:     skill.ContainsBinary,
		})
	}
	return items, nil
}

func (svc *SkillService) DeleteSkill(ctx context.Context, name string) error {
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
	if skill.Status == model.SkillStatusDeleting {
		return service.ErrAccepted("skill deletion is already in progress")
	}
	importCount, err := svc.skillDao.CountImportsBySkillName(name)
	if err != nil {
		return err
	}
	if importCount > 0 {
		return service.ErrConflict("skill is imported by active sessions; remove it from sessions first")
	}
	objectKey := skill.ObjectKey
	if (objectKey != "" && skill.StorageType != model.SkillStorageMinIO) ||
		(objectKey == "" && skill.StorageType == model.SkillStorageMinIO) {
		return service.ErrInternal("Skill storage metadata is inconsistent")
	}
	var deleteJob *model.SkillOperationJob
	if svc.operationDao != nil {
		skillID := skill.ID
		deleteJobInput := model.SkillOperationJob{
			Operation: model.SkillOperationDeleteObject, IdempotencyKey: "delete:" + name,
			SkillID: &skillID, SkillName: name, ObjectKey: objectKey,
		}
		if coordinator, ok := svc.skillDao.(skillOperationReservationStore); ok {
			deleteJob, err = coordinator.UpdateSkillStatusAndOperation(name, model.SkillStatusDeleting, deleteJobInput)
			if err != nil {
				return service.ErrConflict("skill is already being deleted or is not ready")
			}
			deleteJob, err = svc.claimOperation(deleteJob)
			if err != nil {
				return err
			}
		} else {
			if statusStore, ok := svc.skillDao.(skillStatusStore); ok {
				if err := statusStore.UpdateSkillStatus(name, model.SkillStatusDeleting); err != nil {
					return err
				}
			}
			deleteJob, err = svc.startOperation(deleteJobInput)
			if err != nil {
				if statusStore, ok := svc.skillDao.(skillStatusStore); ok {
					_ = statusStore.UpdateSkillStatus(name, model.SkillStatusReady)
				}
				return err
			}
		}
	}
	if objectKey != "" {
		if svc.packageStore == nil {
			err := service.ErrServiceUnavailable("skill package storage is unavailable")
			if statusStore, ok := svc.skillDao.(skillStatusStore); ok {
				if statusErr := statusStore.UpdateSkillStatus(name, model.SkillStatusStorageError); statusErr != nil {
					slog.Warn("mark Skill storage error failed", "skill", name, "error", statusErr)
				}
			}
			svc.retryOperation(deleteJob, err)
			return err
		}
		storageCtx, cancelStorage := skillStorageContext(ctx)
		err := svc.packageStore.Delete(storageCtx, objectKey)
		cancelStorage()
		if err != nil && !errors.Is(err, package_store.ErrNotFound) {
			slog.Warn("delete skill package object failed", "skill", name, "object_key", objectKey, "error", err)
			if statusStore, ok := svc.skillDao.(skillStatusStore); ok {
				_ = statusStore.UpdateSkillStatus(name, model.SkillStatusStorageError)
			}
			svc.retryOperation(deleteJob, err)
			return service.ErrServiceUnavailable("delete skill package failed")
		}
	}
	auditOwner := service.SkillOwnerFromContext(ctx)
	if auditOwner == "" {
		auditOwner = skill.UploadedBy
	}
	deleteAudit := model.SkillAuditEvent{
		Action: "delete", Outcome: "success", SkillID: &skill.ID, SkillName: name,
		OwnerID: auditOwner, ObjectKey: skill.ObjectKey, SHA256: skill.SHA256,
	}
	// Production GORM atomically removes the Hub row, receipts, and the
	// claimed delete task.  The object deletion above is idempotent, so a
	// transaction failure leaves both the row and task retryable for the Worker.
	if deleteJob != nil {
		if completion, ok := svc.skillDao.(skillDeleteCompletionStore); ok {
			if err := completion.DeleteSkillCascadeWithOperation(name, deleteJob.ID, deleteJob.LeaseToken, deleteAudit); err != nil {
				if statusStore, statusOK := svc.skillDao.(skillStatusStore); statusOK {
					_ = statusStore.UpdateSkillStatus(name, model.SkillStatusStorageError)
				}
				svc.retryOperation(deleteJob, err)
				return err
			}
			slog.Info("skill deleted", "skill", name, "object_key", objectKey)
			return nil
		}
	}
	if err := svc.skillDao.DeleteSkillCascade(name); err != nil {
		if statusStore, ok := svc.skillDao.(skillStatusStore); ok {
			_ = statusStore.UpdateSkillStatus(name, model.SkillStatusStorageError)
		}
		svc.retryOperation(deleteJob, err)
		return err
	}
	if auditStore, ok := svc.skillDao.(skillAuditStore); ok {
		if auditErr := auditStore.CreateSkillAuditEvent(deleteAudit); auditErr != nil {
			// The object and Hub row are already gone, so keep the durable job
			// retryable.  The worker will retry only the audit path for a missing
			// Hub row instead of repeating the destructive delete.
			svc.retryOperation(deleteJob, auditErr)
			return service.ErrServiceUnavailable("record Skill deletion audit failed")
		}
	}
	svc.completeOperation(deleteJob)
	svc.deleteOperationJob(deleteJob)
	slog.Info("skill deleted", "skill", name, "object_key", objectKey)
	return nil
}

func (svc *SkillService) ImportSkill(ctx context.Context, skillName, sessionID string) (*service.SkillImportResult, error) {
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
	if skill.Builtin {
		// Builtin Skills are supplied by AgentEnd's local manifest.  They do not
		// participate in the External Skill object lifecycle and must never be
		// interpreted as DB/MinIO packages by this endpoint.
		return nil, service.ErrForbidden("builtin skills are supplied by the agent and cannot be imported")
	}
	if skill.Status != "" && skill.Status != model.SkillStatusReady {
		return nil, service.ErrConflict("skill is not ready for import")
	}

	exists, err := svc.skillDao.HasAgentSkill(sessionID, skillName)
	if err != nil {
		return nil, err
	}
	if exists {
		// The Hub name is unique and confirmations never replace an existing
		// package with a different SHA.  A ready relation therefore represents
		// the same current Skill package; make a repeated import request
		// idempotently successful instead of forcing the caller to treat a
		// successful prior install as a conflict.
		if lookup, ok := svc.skillDao.(agentSkillStatusLookup); ok {
			current, lookupErr := lookup.GetAgentSkill(sessionID, skillName)
			if lookupErr != nil {
				return nil, lookupErr
			}
			if current != nil {
				switch current.Status {
				case "", model.AgentSkillStatusReady:
					return &service.SkillImportResult{Success: true, Skill: skillName, Session: sessionID}, nil
				case model.AgentSkillStatusInstalling, model.AgentSkillStatusRemoving:
					return nil, service.ErrAccepted("skill installation or removal is already in progress")
				}
			}
		}
		return nil, service.ErrConflict("skill already imported to this session")
	}
	agentSkillReservation := model.AgentSkill{
		SessionID:  sessionID,
		SkillName:  skillName,
		AgentType:  session.AgentType,
		Status:     model.AgentSkillStatusInstalling,
		ImportedAt: time.Now(),
	}
	installJobInput := model.SkillOperationJob{}
	if skillID := skill.ID; svc.operationDao != nil {
		installJobInput = model.SkillOperationJob{
			// A new AgentSkill reservation is a new operation lifecycle.  Do not
			// reuse a completed job from an earlier import of the same Skill after
			// it was removed; a stale done job has no lease to retry on failure.
			Operation: model.SkillOperationInstall, IdempotencyKey: "install:" + sessionID + ":" + skillName + ":" + uuid.NewString(),
			SkillID: &skillID, SkillName: skillName, SessionID: sessionID, AgentType: session.AgentType,
		}
	}
	var installJob *model.SkillOperationJob
	if coordinator, ok := svc.skillDao.(skillOperationReservationStore); ok && svc.operationDao != nil {
		installJob, err = coordinator.CreateAgentSkillAndOperation(agentSkillReservation, installJobInput)
		if err != nil {
			if errors.Is(err, dao.ErrDuplicate) {
				return nil, service.ErrConflict("skill already imported to this session")
			}
			return nil, err
		}
		installJob, err = svc.claimOperation(installJob)
		if err != nil {
			return nil, err
		}
	} else {
		if err := svc.skillDao.CreateAgentSkill(agentSkillReservation); err != nil {
			if errors.Is(err, dao.ErrDuplicate) {
				return nil, service.ErrConflict("skill already imported to this session")
			}
			return nil, err
		}
		if svc.operationDao != nil {
			installJob, err = svc.startOperation(installJobInput)
			if err != nil {
				if cleanupErr := svc.skillDao.DeleteAgentSkill(sessionID, skillName); cleanupErr != nil {
					slog.Warn("cleanup install reservation after job claim failure", "session_id", sessionID, "skill", skillName, "error", cleanupErr)
				}
				return nil, err
			}
		}
	}

	zipData, err := svc.readSkillPackage(ctx, skill)
	if err != nil {
		svc.markSkillStorageReadFailure(skill, err)
		if installJob != nil {
			svc.markAgentSkillInstallError(sessionID, skillName, installJob)
			svc.retryOperation(installJob, err)
			return nil, service.ErrServiceUnavailable("read skill package for install failed")
		}
		if cleanupErr := svc.skillDao.DeleteAgentSkill(sessionID, skillName); cleanupErr != nil {
			slog.Warn("cleanup installing skill reservation failed", "session_id", sessionID, "skill", skillName, "error", cleanupErr)
		}
		return nil, err
	}
	if len(zipData) == 0 {
		if installJob != nil {
			err := service.ErrInternal("pack skill files failed: no zip data")
			svc.markAgentSkillInstallError(sessionID, skillName, installJob)
			svc.retryOperation(installJob, err)
			return nil, err
		}
		_ = svc.skillDao.DeleteAgentSkill(sessionID, skillName)
		return nil, service.ErrInternal("pack skill files failed: no zip data")
	}

	if err := svc.installSkill(ctx, session.AgentType, sessionID, skillName, zipData); err != nil {
		slog.Warn("install skill to worktree failed", "session_id", sessionID, "skill", skillName, "agent_type", session.AgentType, "error", err)
		if isKnownSkillAgentFailure(err) {
			if installJob != nil {
				if rollbackErr := rollbackKnownAgentSkillInstall(svc.skillDao, svc.operationDao, installJob); rollbackErr != nil {
					// If the explicit rejection cannot be durably rolled back, keep
					// the reservation diagnosable and retryable rather than claiming
					// the relation disappeared when the DB operation itself failed.
					svc.markAgentSkillInstallError(sessionID, skillName, installJob)
					svc.retryOperation(installJob, rollbackErr)
					return nil, service.ErrServiceUnavailable("rollback rejected Skill installation failed")
				}
			} else if cleanupErr := svc.skillDao.DeleteAgentSkill(sessionID, skillName); cleanupErr != nil {
				return nil, service.ErrServiceUnavailable("rollback rejected Skill installation failed")
			}
			svc.auditSkill(model.SkillAuditEvent{
				Action: "import", Outcome: "rejected", SkillID: &skill.ID, SkillName: skillName,
				OwnerID: service.SkillOwnerFromContext(ctx), SHA256: skill.SHA256, Error: err.Error(),
			})
			return nil, service.ErrBadRequest("AgentEnd rejected Skill installation")
		}
		if installJob != nil {
			svc.markAgentSkillInstallError(sessionID, skillName, installJob)
			svc.retryOperation(installJob, err)
			return nil, service.ErrServiceUnavailable("install skill to worktree failed")
		}
		if cleanupErr := svc.skillDao.DeleteAgentSkill(sessionID, skillName); cleanupErr != nil {
			slog.Warn("cleanup failed skill reservation failed", "session_id", sessionID, "skill", skillName, "error", cleanupErr)
		}
		return nil, service.ErrServiceUnavailable("install skill to worktree failed")
	}
	installed, finishErr := svc.finishAgentSkillInstall(ctx, session.AgentType, sessionID, skillName, installJob)
	if finishErr != nil {
		slog.Warn("finish installed skill reservation failed", "session_id", sessionID, "skill", skillName, "error", finishErr)
		// AgentEnd may already have committed the worktree while the database
		// status fence failed.  Preserve that uncertainty explicitly so the
		// durable install job can reconcile it instead of leaving an apparently
		// installing reservation with no diagnostic state.
		svc.markAgentSkillInstallError(sessionID, skillName, installJob)
		if installJob != nil {
			svc.retryOperation(installJob, finishErr)
		}
		return nil, service.ErrInternal("skill installed but status update failed")
	}
	if !installed {
		// A concurrent remove took ownership after AgentEnd installed the
		// directory. finishAgentSkillInstall compensates with an idempotent
		// remove; do not report a durable import success for the removed row.
		svc.completeOperation(installJob)
		svc.deleteOperationJob(installJob)
		return nil, service.ErrAccepted("skill removal is already in progress")
	}
	svc.completeOperation(installJob)
	svc.deleteOperationJob(installJob)
	svc.auditSkill(model.SkillAuditEvent{
		Action: "import", Outcome: "success", SkillID: &skill.ID, SkillName: skillName,
		OwnerID: service.SkillOwnerFromContext(ctx), SHA256: skill.SHA256,
	})

	return &service.SkillImportResult{
		Success: true,
		Skill:   skillName,
		Session: sessionID,
	}, nil
}

// finishAgentSkillInstall closes the final race with RemoveSkill. The row is
// read after AgentEnd has finished, then transitioned with a conditional DAO
// update. If removal won meanwhile, the newly installed directory is removed
// idempotently instead of being left behind after the AgentSkill row is gone.
func (svc *SkillService) finishAgentSkillInstall(ctx context.Context, agentType, sessionID, skillName string, installJob *model.SkillOperationJob) (bool, error) {
	fenced := false
	if installJob != nil && installJob.AgentSkillID != nil {
		if lookup, ok := svc.skillDao.(agentSkillIDLookup); ok {
			fenced = true
			reservation, err := lookup.GetAgentSkillByID(*installJob.AgentSkillID)
			if err != nil {
				return false, err
			}
			if reservation == nil || reservation.SessionID != sessionID || reservation.SkillName != skillName || reservation.AgentType != agentType || reservation.Status == model.AgentSkillStatusRemoving {
				if removeErr := svc.compensateInstallRemoval(ctx, agentType, sessionID, skillName, installJob); removeErr != nil {
					return false, removeErr
				}
				return false, nil
			}
			if reservation.Status == model.AgentSkillStatusReady {
				return true, nil
			}
		}
	}
	if !fenced {
		if lookup, ok := svc.skillDao.(agentSkillStatusLookup); ok {
			reservation, err := lookup.GetAgentSkill(sessionID, skillName)
			if err != nil {
				return false, err
			}
			if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving {
				if removeErr := svc.removeSkill(ctx, agentType, sessionID, skillName); removeErr != nil {
					return false, removeErr
				}
				return false, nil
			}
			if reservation.Status == model.AgentSkillStatusReady {
				return true, nil
			}
		}
	}
	if installJob != nil && installJob.AgentSkillID != nil {
		if completion, ok := svc.skillDao.(agentSkillInstallCompletionByIDStore); ok {
			updated, err := completion.CompleteAgentSkillInstallByID(*installJob.AgentSkillID)
			if err != nil {
				return false, err
			}
			if updated {
				return true, nil
			}
			if lookup, ok := svc.skillDao.(agentSkillIDLookup); ok {
				reservation, lookupErr := lookup.GetAgentSkillByID(*installJob.AgentSkillID)
				if lookupErr != nil {
					return false, lookupErr
				}
				if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving {
					if removeErr := svc.compensateInstallRemoval(ctx, agentType, sessionID, skillName, installJob); removeErr != nil {
						return false, removeErr
					}
					return false, nil
				}
				if reservation.Status == model.AgentSkillStatusReady {
					return true, nil
				}
			}
			return false, errors.New("agent skill install status was changed concurrently")
		}
	}
	if completion, ok := svc.skillDao.(agentSkillInstallCompletionStore); ok {
		updated, err := completion.CompleteAgentSkillInstall(sessionID, skillName)
		if err != nil {
			return false, err
		}
		if updated {
			return true, nil
		}
		if lookup, ok := svc.skillDao.(agentSkillStatusLookup); ok {
			reservation, lookupErr := lookup.GetAgentSkill(sessionID, skillName)
			if lookupErr != nil {
				return false, lookupErr
			}
			if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving {
				if removeErr := svc.removeSkill(ctx, agentType, sessionID, skillName); removeErr != nil {
					return false, removeErr
				}
				return false, nil
			}
			if reservation.Status == model.AgentSkillStatusReady {
				return true, nil
			}
		}
		return false, errors.New("agent skill install status was changed concurrently")
	}
	if err := svc.skillDao.UpdateAgentSkillStatus(sessionID, skillName, model.AgentSkillStatusReady); err != nil {
		return false, err
	}
	return true, nil
}

func (svc *SkillService) compensateInstallRemoval(ctx context.Context, agentType, sessionID, skillName string, installJob *model.SkillOperationJob) error {
	if installJob != nil && installJob.AgentSkillID != nil {
		if lookup, ok := svc.skillDao.(agentSkillIDLookup); ok {
			current, err := lookup.GetAgentSkillByID(*installJob.AgentSkillID)
			if err != nil {
				return err
			}
			if current != nil {
				if current.SessionID != sessionID || current.SkillName != skillName || current.AgentType != agentType {
					// The job identity no longer refers to the requested pair; never
					// remove files belonging to an unrelated relation.
					return nil
				}
				// The same relation is still present (normally in removing state),
				// so its worktree is the one that must be compensated.
			} else if pairLookup, pairOK := svc.skillDao.(agentSkillStatusLookup); pairOK {
				newer, pairErr := pairLookup.GetAgentSkill(sessionID, skillName)
				if pairErr != nil {
					return pairErr
				}
				if newer != nil {
					// The old relation disappeared and a newer import now owns the
					// pair; do not remove its workspace files with the old job.
					return nil
				}
			}
		} else if lookup, ok := svc.skillDao.(agentSkillStatusLookup); ok {
			// Compatibility DAOs without ID lookup cannot distinguish an old
			// relation from a newer one, so retain the conservative no-delete
			// behavior when the pair is still present.
			current, err := lookup.GetAgentSkill(sessionID, skillName)
			if err != nil {
				return err
			}
			if current != nil {
				return nil
			}
		}
	}
	return svc.removeSkill(ctx, agentType, sessionID, skillName)
}

func (svc *SkillService) markAgentSkillInstallError(sessionID, skillName string, jobs ...*model.SkillOperationJob) {
	if len(jobs) > 0 && jobs[0] != nil && jobs[0].AgentSkillID != nil {
		if fenced, ok := svc.skillDao.(agentSkillInstallErrorByIDStore); ok {
			_, _ = fenced.MarkAgentSkillInstallErrorByID(*jobs[0].AgentSkillID)
			return
		}
	}
	if fenced, ok := svc.skillDao.(agentSkillInstallErrorStore); ok {
		_, _ = fenced.MarkAgentSkillInstallError(sessionID, skillName)
		return
	}
	_ = svc.skillDao.UpdateAgentSkillStatus(sessionID, skillName, model.AgentSkillStatusSyncError)
}

func (svc *SkillService) RemoveSkill(ctx context.Context, skillName, sessionID string) (*service.SkillImportResult, error) {
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
	if lookup, ok := svc.skillDao.(agentSkillStatusLookup); ok {
		current, lookupErr := lookup.GetAgentSkill(sessionID, skillName)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if current != nil && current.Status == model.AgentSkillStatusRemoving {
			return nil, service.ErrAccepted("skill removal is already in progress")
		}
		if current != nil && current.Status == model.AgentSkillStatusInstalling {
			return nil, service.ErrConflict("skill installation is still in progress")
		}
	}
	var removeJob *model.SkillOperationJob
	removeJobInput := model.SkillOperationJob{
		// Removal also gets a fresh durable operation per AgentSkill row.  The
		// row's unique key and status provide the duplicate-request fence.
		Operation: model.SkillOperationRemove, IdempotencyKey: "remove:" + sessionID + ":" + skillName + ":" + uuid.NewString(),
		SkillName: skillName, SessionID: sessionID, AgentType: session.AgentType,
	}
	if svc.operationDao != nil {
		if coordinator, ok := svc.skillDao.(skillOperationReservationStore); ok {
			removeJob, err = coordinator.UpdateAgentSkillStatusAndOperation(sessionID, skillName, model.AgentSkillStatusRemoving, removeJobInput)
			if err != nil {
				return nil, service.ErrConflict("skill is already being installed or removed")
			}
			removeJob, err = svc.claimOperation(removeJob)
			if err != nil {
				return nil, err
			}
		} else {
			if err := svc.skillDao.UpdateAgentSkillStatus(sessionID, skillName, model.AgentSkillStatusRemoving); err != nil {
				return nil, err
			}
			removeJob, err = svc.startOperation(removeJobInput)
			if err != nil {
				_ = svc.skillDao.UpdateAgentSkillStatus(sessionID, skillName, model.AgentSkillStatusSyncError)
				return nil, err
			}
		}
	} else if err := svc.skillDao.UpdateAgentSkillStatus(sessionID, skillName, model.AgentSkillStatusRemoving); err != nil {
		return nil, err
	}

	if err := svc.removeSkill(ctx, session.AgentType, sessionID, skillName); err != nil {
		slog.Warn("remove skill files from worktree failed", "session_id", sessionID, "skill", skillName, "agent_type", session.AgentType, "error", err)
		if isKnownSkillAgentFailure(err) {
			if rollbackErr := restoreKnownAgentSkillRemove(svc.skillDao, svc.operationDao, removeJob, sessionID, skillName); rollbackErr != nil {
				if statusErr := svc.markAgentSkillStatusErrorForJob(sessionID, skillName, removeJob); statusErr != nil {
					slog.Warn("mark rejected Skill removal sync error failed", "session_id", sessionID, "skill", skillName, "error", statusErr)
				}
				svc.retryOperation(removeJob, rollbackErr)
				return nil, service.ErrServiceUnavailable("rollback rejected Skill removal failed")
			}
			svc.auditSkill(model.SkillAuditEvent{Action: "remove", Outcome: "rejected", SkillName: skillName, OwnerID: service.SkillOwnerFromContext(ctx), Error: err.Error()})
			return nil, service.ErrBadRequest("AgentEnd rejected Skill removal")
		}
		if statusErr := svc.markAgentSkillStatusErrorForJob(sessionID, skillName, removeJob); statusErr != nil {
			slog.Warn("mark removed skill sync error failed", "session_id", sessionID, "skill", skillName, "error", statusErr)
		}
		svc.retryOperation(removeJob, err)
		return nil, service.ErrServiceUnavailable("remove skill files from worktree failed")
	}

	if removeJob != nil && removeJob.AgentSkillID != nil {
		if lookup, ok := svc.skillDao.(agentSkillIDLookup); ok {
			reservation, lookupErr := lookup.GetAgentSkillByID(*removeJob.AgentSkillID)
			if lookupErr != nil {
				svc.retryOperation(removeJob, lookupErr)
				return nil, lookupErr
			}
			if reservation == nil || reservation.SessionID != sessionID || reservation.SkillName != skillName || reservation.AgentType != session.AgentType {
				// A newer import owns the pair; the old remove must not delete it.
				svc.completeOperation(removeJob)
				svc.deleteOperationJob(removeJob)
				return &service.SkillImportResult{Success: true, Skill: skillName, Session: sessionID}, nil
			}
			if completion, ok := svc.skillDao.(skillRemoveCompletionStore); ok {
				event := model.SkillAuditEvent{Action: "remove", Outcome: "success", SkillName: skillName, OwnerID: service.SkillOwnerFromContext(ctx)}
				if err := completion.CompleteAgentSkillRemovalByID(*removeJob.AgentSkillID, removeJob.ID, removeJob.LeaseToken, event); err != nil {
					if statusErr := svc.markAgentSkillStatusErrorForJob(sessionID, skillName, removeJob); statusErr != nil {
						slog.Warn("mark removed Skill sync error failed", "session_id", sessionID, "skill", skillName, "error", statusErr)
					}
					svc.retryOperation(removeJob, err)
					return nil, err
				}
				return &service.SkillImportResult{Success: true, Skill: skillName, Session: sessionID}, nil
			}
			if remover, ok := svc.skillDao.(agentSkillRemovalByIDStore); ok {
				if err := remover.DeleteAgentSkillByID(*removeJob.AgentSkillID); err != nil {
					if statusErr := svc.markAgentSkillStatusErrorForJob(sessionID, skillName, removeJob); statusErr != nil {
						slog.Warn("mark removed skill sync error failed", "session_id", sessionID, "skill", skillName, "error", statusErr)
					}
					svc.retryOperation(removeJob, err)
					return nil, err
				}
				svc.completeOperation(removeJob)
				svc.deleteOperationJob(removeJob)
				svc.auditSkill(model.SkillAuditEvent{Action: "remove", Outcome: "success", SkillName: skillName, OwnerID: service.SkillOwnerFromContext(ctx)})
				return &service.SkillImportResult{Success: true, Skill: skillName, Session: sessionID}, nil
			}
		}
	}
	if err := svc.skillDao.DeleteAgentSkill(sessionID, skillName); err != nil {
		if statusErr := svc.markAgentSkillStatusErrorForJob(sessionID, skillName, removeJob); statusErr != nil {
			slog.Warn("mark removed skill sync error failed", "session_id", sessionID, "skill", skillName, "error", statusErr)
		}
		svc.retryOperation(removeJob, err)
		return nil, err
	}
	svc.completeOperation(removeJob)
	svc.deleteOperationJob(removeJob)
	svc.auditSkill(model.SkillAuditEvent{
		Action: "remove", Outcome: "success", SkillName: skillName,
		OwnerID: service.SkillOwnerFromContext(ctx),
	})

	return &service.SkillImportResult{
		Success: true,
		Skill:   skillName,
		Session: sessionID,
	}, nil
}

func (svc *SkillService) markAgentSkillStatusErrorForJob(sessionID, skillName string, job *model.SkillOperationJob) error {
	if job != nil && job.AgentSkillID != nil {
		if fenced, ok := svc.skillDao.(agentSkillStatusByIDStore); ok {
			_, err := fenced.UpdateAgentSkillStatusByID(*job.AgentSkillID, model.AgentSkillStatusSyncError)
			return err
		}
	}
	return svc.skillDao.UpdateAgentSkillStatus(sessionID, skillName, model.AgentSkillStatusSyncError)
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

func (svc *SkillService) installSkill(ctx context.Context, agentType, sessionID, skillName string, zipData []byte) error {
	if client, ok := svc.agentClient.(contextSkillAgentClient); ok {
		return client.InstallSkillWithContext(ctx, agentType, sessionID, skillName, zipData)
	}
	return svc.agentClient.InstallSkill(agentType, sessionID, skillName, zipData)
}

func (svc *SkillService) removeSkill(ctx context.Context, agentType, sessionID, skillName string) error {
	if client, ok := svc.agentClient.(contextSkillAgentClient); ok {
		return client.RemoveSkillWithContext(ctx, agentType, sessionID, skillName)
	}
	return svc.agentClient.RemoveSkill(agentType, sessionID, skillName)
}

func (svc *SkillService) confirmStoredSkill(ctx context.Context, name, stagingKey string) (*service.SkillImportResult, error) {
	const stagingPrefix = "incoming/"
	uploadID := strings.TrimSuffix(strings.TrimPrefix(stagingKey, stagingPrefix), ".zip")
	if !strings.HasPrefix(stagingKey, stagingPrefix) || !strings.HasSuffix(stagingKey, ".zip") || strings.Contains(stagingKey, "..") {
		return nil, service.ErrBadRequest("invalid skill upload receipt")
	}
	if err := service.ValidateSkillUploadID(uploadID); err != nil {
		return nil, service.ErrBadRequest("invalid skill upload receipt")
	}
	ownerID := service.SkillOwnerFromContext(ctx)
	receiptOwnerID := ownerID
	storageCtx, cancelStorage := skillStorageContext(ctx)
	defer cancelStorage()
	var leaseToken string
	// A formal-object protection job is claimed before Promote so the orphan
	// worker cannot remove a newly-created deterministic object while the Hub
	// transaction is still in flight.  Keep its lifecycle tied to this request:
	// failures before Promote must release it immediately (otherwise retries are
	// blocked behind a stale running lease), while failures after Promote leave a
	// retryable cleanup task for the worker.
	var formalObjectJob *model.SkillOperationJob
	formalObjectPromoted := false
	fail := func(err error, permanent bool) (*service.SkillImportResult, error) {
		if formalObjectJob != nil {
			if formalObjectPromoted {
				svc.retryOperation(formalObjectJob, err)
			} else {
				svc.completeOperation(formalObjectJob)
				svc.deleteOperationJob(formalObjectJob)
			}
			formalObjectJob = nil
		}
		if leaseToken != "" && svc.uploadStore != nil {
			if permanent {
				_ = svc.uploadStore.MarkFailed(ctx, uploadID, leaseToken, err.Error())
			} else {
				_ = svc.uploadStore.MarkPending(ctx, uploadID, leaseToken)
			}
			leaseToken = ""
		}
		return nil, err
	}
	if receipt, err := svc.skillDao.GetSkillUploadReceipt(uploadID); err != nil {
		return nil, service.ErrServiceUnavailable("read skill upload receipt failed")
	} else if receipt != nil {
		if receipt.OwnerID != ownerID && !service.SkillAdminFromContext(ctx) {
			return nil, service.ErrForbidden("skill upload receipt owner mismatch")
		}
		confirmed, err := svc.skillDao.GetSkillByID(receipt.SkillID)
		if err != nil {
			return nil, service.ErrServiceUnavailable("read confirmed Skill failed")
		}
		if confirmed == nil || (receipt.SHA256 != "" && confirmed.SHA256 != "" && !strings.EqualFold(receipt.SHA256, confirmed.SHA256)) {
			return nil, service.ErrInternal("skill upload receipt is inconsistent")
		}
		if name != "" && confirmed.Name != name {
			return nil, service.ErrConflict("skill name does not match upload receipt")
		}
		// A crash may have committed the receipt before the incoming-object
		// cleanup.  Receipt-first retries remain idempotent, but must also
		// re-enqueue/attempt that cleanup instead of waiting for the long orphan
		// grace period.
		svc.cleanupStagedObject(ctx, uploadID, stagingKey)
		return &service.SkillImportResult{Success: true, Name: confirmed.Name}, nil
	}
	var uploadSession *skill_upload_session.Session
	if svc.uploadStore != nil {
		var err error
		uploadSession, err = svc.uploadStore.Get(ctx, uploadID)
		if err != nil {
			if errors.Is(err, skill_upload_session.ErrNotFound) {
				return nil, service.ErrGone("skill upload session expired")
			}
			return nil, service.ErrServiceUnavailable("skill upload session unavailable")
		}
		if uploadSession.ObjectKey != stagingKey {
			return nil, service.ErrBadRequest("skill upload receipt object mismatch")
		}
		if name != "" && name != uploadSession.Name {
			return nil, service.ErrConflict("skill name does not match upload session")
		}
		beginOwner := ownerID
		if service.SkillAdminFromContext(ctx) {
			// Administrators may recover a session created by a previous
			// Backend/user.  Keep the original owner in Redis; the durable
			// receipt still records the administrator who confirmed it.
			beginOwner = uploadSession.OwnerID
		}
		begin, beginErr := svc.uploadStore.BeginConfirm(ctx, uploadID, beginOwner, uuid.NewString(), time.Now())
		if beginErr != nil {
			switch {
			case errors.Is(beginErr, skill_upload_session.ErrConfirmRunning):
				return nil, service.ErrAccepted("skill confirmation is already in progress; retry later")
			case errors.Is(beginErr, skill_upload_session.ErrOwnerMismatch):
				return nil, service.ErrForbidden("skill upload session owner mismatch")
			case errors.Is(beginErr, skill_upload_session.ErrConfirmFailed):
				return nil, service.ErrConflict("skill upload confirmation has failed")
			case errors.Is(beginErr, skill_upload_session.ErrNotFound):
				return nil, service.ErrGone("skill upload session expired")
			default:
				return nil, service.ErrServiceUnavailable("skill upload session unavailable")
			}
		}
		if begin == nil || begin.Session == nil {
			return nil, service.ErrInternal("invalid skill upload session state")
		}
		if begin.Session.State == "confirmed" && begin.Session.ConfirmedSkillID != 0 {
			confirmed, err := svc.skillDao.GetSkillByID(begin.Session.ConfirmedSkillID)
			if err != nil {
				return nil, service.ErrServiceUnavailable("read confirmed Skill failed")
			}
			if confirmed == nil {
				return nil, service.ErrInternal("confirmed skill is missing")
			}
			svc.cleanupStagedObject(ctx, uploadID, stagingKey)
			return &service.SkillImportResult{Success: true, Name: confirmed.Name}, nil
		}
		uploadSession = begin.Session
		name = uploadSession.Name
		// The durable receipt represents the creator of the upload, not an
		// administrator who later recovers it during a rolling upgrade.  The
		// current caller is still checked separately on every receipt lookup.
		if uploadSession.OwnerID != "" {
			receiptOwnerID = uploadSession.OwnerID
		}
		leaseToken = begin.Token
	}
	// Keep a long-running confirmation fenced to its lease owner.  Every Redis
	// mutation still compares the token atomically; a renewal failure is only a
	// diagnostic signal here, and the final MarkConfirmed/MarkPending operation
	// remains the authoritative fence before the request can report success.
	var leaseCancel context.CancelFunc
	var leaseWG sync.WaitGroup
	if svc.uploadStore != nil && leaseToken != "" {
		leaseCtx, cancel := context.WithCancel(ctx)
		leaseCancel = cancel
		interval := svc.uploadStore.LeaseDuration() / 3
		if interval <= 0 {
			interval = 100 * time.Millisecond
		}
		leaseTokenForRenewal := leaseToken
		leaseWG.Add(1)
		go func() {
			defer leaseWG.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-leaseCtx.Done():
					return
				case <-ticker.C:
					renewCtx, renewCancel := context.WithTimeout(leaseCtx, 5*time.Second)
					renewErr := svc.uploadStore.RenewConfirm(renewCtx, uploadID, leaseTokenForRenewal, time.Now())
					renewCancel()
					if renewErr != nil {
						slog.Warn("renew Skill upload confirmation lease failed", "upload_id", uploadID, "error", renewErr)
						return
					}
				}
			}
		}()
		defer func() {
			leaseCancel()
			leaseWG.Wait()
		}()
	}
	if name == "" {
		return fail(service.ErrBadRequest("skill name is required"), true)
	}
	packageLimit := service.NormalizeZipLimits(svc.zipLimits).PackageSize
	info, err := svc.packageStore.Stat(storageCtx, stagingKey)
	if err != nil {
		if errors.Is(err, package_store.ErrNotFound) {
			return fail(service.ErrGone("skill upload expired or not found"), true)
		}
		return fail(service.ErrServiceUnavailable("read skill upload failed"), false)
	}
	if info.Size > packageLimit {
		return fail(service.ErrBadRequest("skill package exceeds configured package limit"), true)
	}
	if uploadSession != nil && uploadSession.PackageSize > 0 && info.Size != uploadSession.PackageSize {
		return fail(service.ErrBadRequest("skill package size does not match upload session"), true)
	}
	rc, err := svc.packageStore.Open(storageCtx, stagingKey)
	if err != nil {
		return fail(service.ErrServiceUnavailable("read skill upload failed"), false)
	}
	data, readErr := io.ReadAll(io.LimitReader(rc, packageLimit+1))
	_ = rc.Close()
	if readErr != nil {
		return fail(service.ErrServiceUnavailable("read skill upload failed"), false)
	}
	if int64(len(data)) != info.Size || int64(len(data)) > packageLimit {
		return fail(service.ErrBadRequest("skill package size mismatch"), true)
	}
	actualSHA := sha256Hex(data)
	if info.SHA256 != "" && !strings.EqualFold(info.SHA256, actualSHA) {
		return fail(service.ErrBadRequest("skill package integrity check failed"), true)
	}
	if uploadSession != nil && uploadSession.SHA256 != "" && !strings.EqualFold(uploadSession.SHA256, actualSHA) {
		return fail(service.ErrBadRequest("skill package hash does not match upload session"), true)
	}
	if err := service.CheckSkillTempFreeSpace(svc.tempDir, svc.tempFreeSpaceMinimum()); err != nil {
		return fail(service.ErrServiceUnavailable("Skill temporary volume is low on space"), false)
	}
	// Re-validate the immutable staging object with the same configured limits
	// used during upload.  The object is private, but Redis/MinIO state can be
	// lost or tampered with between upload and confirm; confirmation must not
	// silently fall back to the validator defaults or ignore request cancellation.
	validationCtx, releaseValidation, validationErr := svc.beginValidation(ctx)
	if validationErr != nil {
		return fail(service.ErrServiceUnavailable("skill validation cancelled"), false)
	}
	metadata, tmpDir, validateErr := service.ValidateZipReaderAtContextWithLimits(
		validationCtx,
		bytes.NewReader(data),
		int64(len(data)),
		svc.tempDir,
		service.NormalizeZipLimits(svc.zipLimits),
	)
	validationCanceled := validationCtx.Err()
	releaseValidation()
	if validationCanceled != nil {
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
		return fail(service.ErrServiceUnavailable("skill validation timed out or was cancelled"), false)
	}
	if validateErr != nil || metadata == nil || !metadata.Valid {
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
		if metadata != nil && len(metadata.Errors) > 0 {
			return fail(service.ErrBadRequest(strings.Join(metadata.Errors, "; ")), true)
		}
		return fail(service.ErrBadRequest("invalid skill package"), true)
	}
	if policyErr := svc.enforceContentPolicy(ctx, metadata, tmpDir); policyErr != nil {
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
		permanent := false
		if bizErr, ok := policyErr.(*service.BizError); ok {
			permanent = bizErr.Code == 400
		}
		return fail(policyErr, permanent)
	}
	if tmpDir != "" {
		_ = os.RemoveAll(tmpDir)
	}
	if metadata.Name != name {
		return fail(service.ErrBadRequest("skill name does not match upload"), true)
	}
	count, err := svc.skillDao.CountByName(name)
	if err != nil {
		return fail(service.ErrServiceUnavailable("check Skill name failed"), false)
	}
	if count > 0 {
		existing, getErr := svc.skillDao.GetSkillByName(name)
		if getErr != nil {
			return fail(service.ErrServiceUnavailable("read existing Skill failed"), false)
		}
		if existing == nil || existing.Builtin || (existing.Status != "" && existing.Status != model.SkillStatusReady) || existing.SHA256 == "" || !strings.EqualFold(existing.SHA256, actualSHA) {
			return fail(service.ErrConflict("skill name already exists"), true)
		}
		if _, readErr := svc.readSkillPackage(ctx, existing); readErr != nil {
			// A duplicate confirmation must not bless a metadata row whose
			// authoritative package has disappeared or been corrupted.
			svc.markSkillStorageReadFailure(existing, readErr)
			return fail(service.ErrServiceUnavailable("existing Skill package integrity check failed"), false)
		}
		receipt := model.SkillUploadReceipt{UploadID: uploadID, SkillID: existing.ID, SHA256: actualSHA, OwnerID: receiptOwnerID, CreatedAt: time.Now()}
		confirmAudit := model.SkillAuditEvent{
			Action: "confirm", Outcome: "success", UploadID: uploadID, SkillID: &existing.ID,
			SkillName: existing.Name, OwnerID: ownerID, SHA256: actualSHA,
			FilesJSON: skillFilesJSON(metadata.Files), ContainsExecutable: metadata.ContainsExecutable,
			ContainsBinary: metadata.ContainsBinary,
		}
		var receiptErr error
		transactionalAudit := false
		if txStore, ok := svc.skillDao.(skillReceiptAuditTransactionStore); ok {
			transactionalAudit = true
			_, receiptErr = txStore.CreateReceiptForExistingSkillAndAudit(name, receipt, confirmAudit)
		} else if txStore, ok := svc.skillDao.(skillReceiptTransactionStore); ok {
			_, receiptErr = txStore.CreateReceiptForExistingSkill(name, receipt)
		} else {
			receiptErr = svc.skillDao.CreateSkillUploadReceipt(receipt)
		}
		if receiptErr != nil {
			if stored, getErr := svc.skillDao.GetSkillUploadReceipt(uploadID); getErr == nil && stored != nil {
				if stored.OwnerID != ownerID && !service.SkillAdminFromContext(ctx) {
					return fail(service.ErrForbidden("skill upload receipt owner mismatch"), true)
				}
				if stored.SkillID == existing.ID && strings.EqualFold(stored.SHA256, actualSHA) {
					if svc.uploadStore != nil {
						if markErr := svc.uploadStore.MarkConfirmed(ctx, uploadID, leaseToken, existing.ID, actualSHA); markErr != nil {
							leaseToken = ""
							return nil, service.ErrServiceUnavailable("save skill upload confirmation state failed")
						}
						leaseToken = ""
					}
					svc.cleanupStagedObject(ctx, uploadID, stagingKey)
					return &service.SkillImportResult{Success: true, Name: existing.Name}, nil
				}
			}
			return fail(service.ErrServiceUnavailable("save skill upload receipt failed"), false)
		}
		if !transactionalAudit {
			if err := svc.auditSkillStrict(confirmAudit); err != nil {
				return fail(service.ErrServiceUnavailable("record Skill confirmation audit failed"), false)
			}
		}
		if svc.uploadStore != nil {
			if err := svc.uploadStore.MarkConfirmed(ctx, uploadID, leaseToken, existing.ID, actualSHA); err != nil {
				leaseToken = ""
				return nil, service.ErrServiceUnavailable("save skill upload confirmation state failed")
			}
			leaseToken = ""
		}
		svc.cleanupStagedObject(ctx, uploadID, stagingKey)
		return &service.SkillImportResult{Success: true, Name: existing.Name}, nil
	}
	finalKey := "skills/" + name + "/" + actualSHA + ".zip"
	// Protect the deterministic formal object while confirmation is between
	// Promote and the metadata transaction.  Without a durable in-flight job,
	// the orphan worker could observe the object before its SkillHub row exists,
	// race a successful confirmation, and delete the newly referenced object.
	// A claimed delete job expires after a crashed confirmer; its worker
	// rechecks object references immediately before deleting, so a committed
	// row wins over cleanup.
	if svc.operationDao != nil {
		formalObjectJob, err = svc.startOperation(model.SkillOperationJob{
			Operation:      model.SkillOperationDeleteObject,
			IdempotencyKey: "confirm-object:" + uploadID,
			ObjectKey:      finalKey,
		})
		if err != nil {
			var bizErr *service.BizError
			if !errors.As(err, &bizErr) {
				err = service.ErrServiceUnavailable("queue confirmed Skill object protection failed")
			}
			return fail(err, false)
		}
	}
	if err := svc.packageStore.Promote(storageCtx, stagingKey, finalKey, package_store.ObjectInfo{
		Key:    stagingKey,
		Size:   info.Size,
		SHA256: actualSHA,
	}); err != nil {
		if errors.Is(err, package_store.ErrTargetConflict) {
			return fail(service.ErrConflict("skill package object conflict"), true)
		}
		return fail(service.ErrServiceUnavailable("promote skill package failed"), false)
	}
	formalObjectPromoted = true
	skillRecord := model.SkillHub{
		Name:        name,
		Builtin:     false,
		Description: metadata.Description,
		FileCount:   metadata.FileCount,
		TotalSize:   metadata.TotalSize,
		ObjectKey:   finalKey,
		SHA256:      actualSHA,
		PackageSize: info.Size,
		StorageType: model.SkillStorageMinIO,
		Status:      model.SkillStatusReady,
		// Keep the Hub source attribution tied to the upload creator. An admin
		// may recover a confirmation lease, but must not rewrite who uploaded
		// the package; the audit event below records the confirming actor.
		UploadedBy:         receiptOwnerID,
		FilesJSON:          skillFilesJSON(metadata.Files),
		ContainsExecutable: metadata.ContainsExecutable,
		ContainsBinary:     metadata.ContainsBinary,
	}
	if svc.shadowWriteBlob {
		skillRecord.Content = append([]byte(nil), data...)
	}
	receipt := model.SkillUploadReceipt{
		UploadID:  uploadID,
		SHA256:    actualSHA,
		OwnerID:   receiptOwnerID,
		CreatedAt: time.Now(),
	}
	var confirmed *model.SkillHub
	confirmAudit := model.SkillAuditEvent{
		Action: "confirm", Outcome: "success", UploadID: uploadID,
		SkillName: name, OwnerID: ownerID, SHA256: actualSHA,
		FilesJSON: skillFilesJSON(metadata.Files), ContainsExecutable: metadata.ContainsExecutable,
		ContainsBinary: metadata.ContainsBinary,
	}
	transactionalAudit := false
	if txStore, ok := svc.skillDao.(skillReceiptAuditTransactionStore); ok {
		transactionalAudit = true
		confirmed, err = txStore.CreateSkillWithReceiptAndAudit(skillRecord, receipt, confirmAudit)
	} else if txStore, ok := svc.skillDao.(skillReceiptTransactionStore); ok {
		confirmed, err = txStore.CreateSkillWithReceipt(skillRecord, receipt)
		if err != nil {
			// A different upload_id may have won the unique Skill name race.
			// Re-read the committed row and create this upload's independent
			// receipt when the content hash agrees; concurrent same-upload
			// confirmation is handled by the receipt lookup in the helper.
			if recovered, recoverErr := svc.recoverConcurrentConfirmation(ctx, name, uploadID, ownerID, receiptOwnerID, actualSHA, metadata, leaseToken, stagingKey); recovered != nil || recoverErr != nil {
				if recoverErr != nil {
					var bizErr *service.BizError
					if !errors.As(recoverErr, &bizErr) {
						return fail(service.ErrServiceUnavailable("recover skill confirmation failed"), false)
					}
				}
				if recovered != nil {
					svc.finishFormalObjectProtection(formalObjectJob)
				}
				return recovered, recoverErr
			}
			if existing, lookupErr := svc.skillDao.GetSkillByName(name); lookupErr != nil {
				return fail(service.ErrServiceUnavailable("read concurrent Skill confirmation failed"), false)
			} else if existing != nil {
				return fail(service.ErrConflict("skill name already exists"), true)
			}
			return fail(service.ErrServiceUnavailable("save Skill metadata and receipt failed"), false)
		}
	} else {
		if err := svc.skillDao.CreateSkill(skillRecord); err != nil {
			return fail(service.ErrServiceUnavailable("save Skill metadata failed"), false)
		}
		confirmed, err = svc.skillDao.GetSkillByName(name)
		if err != nil || confirmed == nil {
			if err == nil {
				err = errors.New("created skill cannot be reloaded")
			}
			return fail(service.ErrServiceUnavailable("read saved Skill metadata failed"), false)
		}
		receipt.SkillID = confirmed.ID
		if err := svc.skillDao.CreateSkillUploadReceipt(receipt); err != nil {
			return fail(service.ErrServiceUnavailable("save skill upload receipt failed"), false)
		}
	}
	if !transactionalAudit {
		if confirmed != nil {
			confirmAudit.SkillID = &confirmed.ID
		}
		if err := svc.auditSkillStrict(confirmAudit); err != nil {
			return fail(service.ErrServiceUnavailable("record Skill confirmation audit failed"), false)
		}
	}
	// The metadata/receipt transaction now protects the formal object.  Close
	// the temporary protection task before acknowledging Redis; if this cleanup
	// itself is interrupted, a later worker run will recheck the committed row
	// and safely finish the task without deleting the object.
	svc.finishFormalObjectProtection(formalObjectJob)
	if svc.uploadStore != nil {
		if err := svc.uploadStore.MarkConfirmed(ctx, uploadID, leaseToken, confirmed.ID, actualSHA); err != nil {
			// The receipt is durable and will make a retry idempotent.  Do not
			// mark the session failed merely because Redis was lost after the
			// MySQL commit.
			leaseToken = ""
			return nil, service.ErrServiceUnavailable("save skill upload confirmation state failed")
		}
		leaseToken = ""
	}
	svc.cleanupStagedObject(ctx, uploadID, stagingKey)
	slog.Info("skill upload confirmed", "upload_id", uploadID, "skill", name, "owner_id", ownerID, "sha256", actualSHA)
	return &service.SkillImportResult{Success: true, Name: name}, nil
}

func (svc *SkillService) finishFormalObjectProtection(job *model.SkillOperationJob) {
	if job == nil {
		return
	}
	svc.completeOperation(job)
	svc.deleteOperationJob(job)
}

// recoverConcurrentConfirmation converts a unique-name/receipt race into the
// specified idempotent result.  It returns (nil, nil) when the original error
// was not a recoverable duplicate so the caller can preserve its retry path.
func (svc *SkillService) recoverConcurrentConfirmation(ctx context.Context, name, uploadID, ownerID, receiptOwnerID, actualSHA string, metadata *service.ValidationResult, leaseToken, stagingKey string) (*service.SkillImportResult, error) {
	existing, err := svc.skillDao.GetSkillByName(name)
	if err != nil || existing == nil || existing.Builtin || (existing.Status != "" && existing.Status != model.SkillStatusReady) || existing.SHA256 == "" || !strings.EqualFold(existing.SHA256, actualSHA) {
		return nil, err
	}
	receipt, err := svc.skillDao.GetSkillUploadReceipt(uploadID)
	if err != nil {
		return nil, err
	}
	if receipt != nil {
		if receipt.OwnerID != ownerID && !service.SkillAdminFromContext(ctx) {
			return nil, service.ErrForbidden("skill upload receipt owner mismatch")
		}
		if receipt.SkillID != existing.ID || !strings.EqualFold(receipt.SHA256, actualSHA) {
			return nil, service.ErrInternal("skill upload receipt is inconsistent")
		}
	} else {
		newReceipt := model.SkillUploadReceipt{UploadID: uploadID, SkillID: existing.ID, SHA256: actualSHA, OwnerID: receiptOwnerID, CreatedAt: time.Now()}
		confirmAudit := model.SkillAuditEvent{
			Action: "confirm", Outcome: "success", UploadID: uploadID, SkillID: &existing.ID,
			SkillName: existing.Name, OwnerID: ownerID, SHA256: actualSHA,
			FilesJSON: skillFilesJSON(metadata.Files), ContainsExecutable: metadata.ContainsExecutable,
			ContainsBinary: metadata.ContainsBinary,
		}
		var createErr error
		transactionalAudit := false
		if txStore, ok := svc.skillDao.(skillReceiptAuditTransactionStore); ok {
			transactionalAudit = true
			_, createErr = txStore.CreateReceiptForExistingSkillAndAudit(name, newReceipt, confirmAudit)
		} else if txStore, ok := svc.skillDao.(skillReceiptTransactionStore); ok {
			_, createErr = txStore.CreateReceiptForExistingSkill(name, newReceipt)
		} else {
			createErr = svc.skillDao.CreateSkillUploadReceipt(newReceipt)
		}
		if createErr != nil {
			receipt, err = svc.skillDao.GetSkillUploadReceipt(uploadID)
			if err != nil || receipt == nil {
				return nil, createErr
			}
		}
		if !transactionalAudit && createErr == nil {
			if err := svc.auditSkillStrict(confirmAudit); err != nil {
				return nil, service.ErrServiceUnavailable("record Skill confirmation audit failed")
			}
		}
		if receipt != nil && (receipt.SkillID != existing.ID || !strings.EqualFold(receipt.SHA256, actualSHA)) {
			return nil, service.ErrInternal("skill upload receipt is inconsistent")
		}
	}
	if svc.uploadStore != nil {
		if err := svc.uploadStore.MarkConfirmed(ctx, uploadID, leaseToken, existing.ID, actualSHA); err != nil {
			return nil, service.ErrServiceUnavailable("save skill upload confirmation state failed")
		}
	}
	svc.cleanupStagedObject(ctx, uploadID, stagingKey)
	return &service.SkillImportResult{Success: true, Name: existing.Name}, nil
}

func (svc *SkillService) cleanupStagedObject(ctx context.Context, uploadID, objectKey string) {
	if svc.packageStore == nil {
		return
	}
	storageCtx, cancelStorage := skillStorageContext(ctx)
	defer cancelStorage()
	var job *model.SkillOperationJob
	if svc.operationDao != nil {
		var err error
		job, err = svc.startOperation(model.SkillOperationJob{
			Operation: model.SkillOperationDeleteObject, IdempotencyKey: "incoming-delete:" + uploadID,
			ObjectKey: objectKey,
		})
		if err != nil {
			slog.Warn("enqueue staged Skill object cleanup failed", "upload_id", uploadID, "object_key", objectKey, "error", err)
		}
	}
	if err := svc.packageStore.Delete(storageCtx, objectKey); err != nil && !errors.Is(err, package_store.ErrNotFound) {
		slog.Warn("delete staged skill package failed", "upload_id", uploadID, "object_key", objectKey, "error", err)
		svc.retryOperation(job, err)
		return
	}
	svc.completeOperation(job)
	svc.deleteOperationJob(job)
}

func (svc *SkillService) readSkillPackage(ctx context.Context, skill *model.SkillHub) ([]byte, error) {
	if skill == nil {
		return nil, service.ErrNotFound("skill not found")
	}
	if skill.Builtin {
		return nil, service.ErrForbidden("builtin skills do not have an External Skill package")
	}
	if (skill.ObjectKey != "" && skill.StorageType != model.SkillStorageMinIO) ||
		(skill.ObjectKey == "" && skill.StorageType == model.SkillStorageMinIO) {
		return nil, service.ErrInternal("Skill storage metadata is inconsistent")
	}
	if svc.readPreference == "db" {
		return svc.readDBSkillPackage(skill)
	}
	if skill.ObjectKey == "" {
		if skill.StorageType == model.SkillStorageMinIO {
			return nil, service.ErrInternal("MinIO Skill is missing its object key")
		}
		return svc.readDBSkillPackage(skill)
	}
	if skill.StorageType != model.SkillStorageMinIO {
		return nil, service.ErrInternal("Skill storage metadata is inconsistent")
	}
	if svc.packageStore == nil || skill.ObjectKey == "" {
		return nil, service.ErrServiceUnavailable("skill package storage is unavailable")
	}
	if strings.TrimSpace(skill.SHA256) == "" {
		// A MinIO-backed row without its authoritative content hash cannot be
		// safely imported or repaired.  Do not treat a successful byte read as
		// proof of integrity when the database has no expected digest.
		return nil, service.ErrInternal("MinIO Skill is missing its SHA-256")
	}
	storageCtx, cancelStorage := skillStorageContext(ctx)
	defer cancelStorage()
	packageLimit := service.NormalizeZipLimits(svc.zipLimits).PackageSize
	info, err := svc.packageStore.Stat(storageCtx, skill.ObjectKey)
	if err != nil {
		return nil, service.ErrServiceUnavailable("read skill package metadata failed")
	}
	if info.Size > packageLimit {
		return nil, service.ErrInternal("skill package exceeds configured limit")
	}
	if skill.PackageSize > 0 && info.Size != skill.PackageSize {
		return nil, service.ErrInternal("skill package size mismatch")
	}
	// Object metadata is only a fast filter.  Older objects or compatible S3
	// gateways may omit the user metadata; the authoritative check below hashes
	// the bounded object bytes themselves.  A present, conflicting metadata hash
	// is still rejected early.
	if skill.SHA256 != "" && info.SHA256 != "" && !strings.EqualFold(info.SHA256, skill.SHA256) {
		return nil, service.ErrInternal("skill package integrity check failed")
	}
	rc, err := svc.packageStore.Open(storageCtx, skill.ObjectKey)
	if err != nil {
		return nil, service.ErrServiceUnavailable("read skill package failed")
	}
	data, err := io.ReadAll(io.LimitReader(rc, packageLimit+1))
	_ = rc.Close()
	if err != nil {
		return nil, service.ErrServiceUnavailable("read skill package failed")
	}
	if int64(len(data)) != info.Size || int64(len(data)) > packageLimit {
		return nil, service.ErrInternal("skill package size mismatch")
	}
	actualSHA := sha256Hex(data)
	if info.SHA256 != "" && !strings.EqualFold(info.SHA256, actualSHA) {
		return nil, service.ErrInternal("skill package integrity check failed")
	}
	if skill.SHA256 != "" && !strings.EqualFold(skill.SHA256, actualSHA) {
		return nil, service.ErrInternal("skill package integrity check failed")
	}
	return data, nil
}

func (svc *SkillService) readDBSkillPackage(skill *model.SkillHub) ([]byte, error) {
	packageLimit := service.NormalizeZipLimits(svc.zipLimits).PackageSize
	if skill.PackageSize > packageLimit {
		return nil, service.ErrInternal("skill package exceeds configured limit")
	}
	var zipData []byte
	var err error
	if bounded, ok := svc.skillDao.(interface {
		GetSkillContentLimited(name string, maxBytes int64) ([]byte, error)
	}); ok {
		zipData, err = bounded.GetSkillContentLimited(skill.Name, packageLimit)
	} else {
		// Compatibility DAOs used by older deployments/tests may not expose the
		// bounded projection yet; retain the existing contract for them and
		// enforce the length immediately after the read.
		zipData, err = svc.skillDao.GetSkillContent(skill.Name)
	}
	if err != nil {
		return nil, err
	}
	if len(zipData) == 0 {
		return nil, service.ErrInternal("skill BLOB is unavailable")
	}
	if int64(len(zipData)) > packageLimit {
		return nil, service.ErrInternal("skill package exceeds configured limit")
	}
	if skill.PackageSize > 0 && int64(len(zipData)) != skill.PackageSize {
		return nil, service.ErrInternal("skill package size mismatch")
	}
	actualSHA := sha256Hex(zipData)
	if skill.SHA256 != "" && !strings.EqualFold(skill.SHA256, actualSHA) {
		return nil, service.ErrInternal("skill package integrity check failed")
	}
	// Historical rows may be missing either digest or package size.  Persist
	// both verified values together so later reads have a complete integrity
	// boundary.  The ID==0 branch is limited to compatibility adapters that do
	// not represent a persisted row (the production DAO always has an ID).
	if skill.ID != 0 && (skill.SHA256 == "" || skill.PackageSize <= 0) {
		backfill, ok := svc.skillDao.(skillHashBackfillStore)
		if !ok {
			if skill.SHA256 == "" {
				return nil, service.ErrInternal("historical Skill is missing its SHA-256")
			}
			return nil, service.ErrInternal("historical Skill is missing its package size")
		}
		if err := backfill.UpdateSkillContentAndMetadata(skill.ID, zipData, actualSHA, int64(len(zipData))); err != nil {
			return nil, service.ErrServiceUnavailable("backfill Skill integrity metadata failed")
		}
	}
	skill.SHA256 = actualSHA
	skill.PackageSize = int64(len(zipData))
	return zipData, nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func normalizeSkillName(name string) (string, error) {
	return service.NormalizeSkillName(name)
}

func canonicalPackageError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return service.ErrServiceUnavailable("canonical Skill package validation timed out or was cancelled")
	}
	return service.ErrBadRequest(err.Error())
}
