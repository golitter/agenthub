package impl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/package_store"
)

// SkillOperationWorker reclaims durable Skill side effects after a process
// crash.  Requests may execute a newly-created job synchronously, while this
// worker handles jobs whose lease expired or whose request disappeared.
type SkillOperationWorker struct {
	jobs           dao.SkillOperationDao
	skillDao       dao.SkillDao
	packageStore   package_store.PackageStore
	agentClient    skillAgentClient
	poll           time.Duration
	opTimeout      time.Duration
	readPreference string
	zipLimits      service.ZipLimits
	orphanGrace    time.Duration
	incomingTTL    time.Duration
	cleanupEvery   time.Duration
	nextCleanup    time.Time
}

var errSkillOperationTerminal = errors.New("skill operation reached a terminal outcome")

func NewSkillOperationWorker(jobs dao.SkillOperationDao, skillDao dao.SkillDao, store package_store.PackageStore, agentClient skillAgentClient) *SkillOperationWorker {
	return &SkillOperationWorker{jobs: jobs, skillDao: skillDao, packageStore: store, agentClient: agentClient, poll: 2 * time.Second, opTimeout: 2 * time.Minute, zipLimits: service.DefaultZipLimits(), orphanGrace: 48 * time.Hour, incomingTTL: 24 * time.Hour, cleanupEvery: 5 * time.Minute}
}

func (w *SkillOperationWorker) SetReadPreference(preference string) {
	if preference == "minio" || preference == "db" {
		w.readPreference = preference
	}
}

func (w *SkillOperationWorker) SetZipLimits(limits service.ZipLimits) {
	w.zipLimits = service.NormalizeZipLimits(limits)
}

func (w *SkillOperationWorker) SetIncomingCleanupPolicy(ttl, interval time.Duration) {
	if ttl > 0 {
		w.incomingTTL = ttl
	}
	if interval > 0 {
		w.cleanupEvery = interval
	}
}

func (w *SkillOperationWorker) SetOrphanCleanupPolicy(grace time.Duration) {
	if grace > 0 {
		w.orphanGrace = grace
	}
}

func (w *SkillOperationWorker) Run(ctx context.Context) {
	if w == nil || w.jobs == nil {
		return
	}
	ticker := time.NewTicker(w.poll)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Warn("skill operation worker iteration failed", "error", err)
		}
		if time.Now().After(w.nextCleanup) {
			if err := w.CleanupStaleFormal(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("stale formal Skill cleanup failed", "error", err)
			}
			if err := w.CleanupStaleIncoming(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("stale incoming Skill cleanup failed", "error", err)
			}
			w.nextCleanup = time.Now().Add(w.cleanupEvery)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type skillObjectLookup interface {
	GetSkillByObjectKey(objectKey string) (*model.SkillHub, error)
}

// skillPendingObjectOperationExcept is an optional capability implemented by
// the production outbox DAO.  Older compatibility/test DAOs keep the broader
// HasPendingObjectOperation method, while the worker can use this fenced form
// when it is available to distinguish its own running delete row from another
// confirmation/repair operation for the same immutable object.
type skillPendingObjectOperationExcept interface {
	HasPendingObjectOperationExcept(objectKey string, excludeID uint64) (bool, error)
}

type skillStorageErrorStore interface {
	UpdateSkillStatusIfNotDeleting(name, status string) error
}

type skillStorageStatusAuditStore interface {
	UpdateSkillStatusIfNotDeletingWithAudit(id uint, status string, event model.SkillAuditEvent) error
}

type agentSkillIDLookup interface {
	GetAgentSkillByID(id uint) (*model.AgentSkill, error)
}

type agentSkillInstallCompletionStore interface {
	CompleteAgentSkillInstall(sessionID, skillName string) (bool, error)
}

type agentSkillInstallCompletionByIDStore interface {
	CompleteAgentSkillInstallByID(id uint) (bool, error)
}

type agentSkillInstallErrorStore interface {
	MarkAgentSkillInstallError(sessionID, skillName string) (bool, error)
}

type agentSkillInstallErrorByIDStore interface {
	MarkAgentSkillInstallErrorByID(id uint) (bool, error)
}

type agentSkillRemovalByIDStore interface {
	DeleteAgentSkillByID(id uint) error
}

type agentSkillStatusByIDStore interface {
	UpdateAgentSkillStatusByID(id uint, status string) (bool, error)
}

// agentSkillForJob returns the relation identified by the operation's row ID.
// A false fence means this is an old job created before AgentSkillID existed;
// those jobs retain the legacy pair-based checks for compatibility.
func (w *SkillOperationWorker) agentSkillForJob(job *model.SkillOperationJob) (*model.AgentSkill, bool, error) {
	if job == nil || job.AgentSkillID == nil {
		return nil, false, nil
	}
	lookup, ok := w.skillDao.(agentSkillIDLookup)
	if !ok {
		return nil, false, nil
	}
	relation, err := lookup.GetAgentSkillByID(*job.AgentSkillID)
	if err != nil {
		return nil, true, err
	}
	if relation == nil || relation.SessionID != job.SessionID || relation.SkillName != job.SkillName ||
		(job.AgentType != "" && relation.AgentType != job.AgentType) {
		return nil, true, nil
	}
	return relation, true, nil
}

func (w *SkillOperationWorker) compensateInstallRemoval(ctx context.Context, reader *SkillService, job *model.SkillOperationJob) error {
	return reader.compensateInstallRemoval(ctx, job.AgentType, job.SessionID, job.SkillName, job)
}

func (w *SkillOperationWorker) queueObjectVerification(skill *model.SkillHub, cause error) {
	if w == nil || w.jobs == nil || skill == nil || skill.ID == 0 || skill.ObjectKey == "" {
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
	if _, err := w.jobs.CreateSkillOperationJob(job); err != nil {
		slog.Warn("queue Skill object verification failed", "skill", skill.Name, "object_key", skill.ObjectKey, "error", err)
	}
}

// CleanupStaleFormal discovers unreferenced formal objects and puts them into
// the durable delete queue.  It does not delete inline: executeDelete checks
// the reference again after claiming the job, closing the race where a new
// Skill row is committed between the list and the delete attempt.
func (w *SkillOperationWorker) CleanupStaleFormal(ctx context.Context) error {
	if w == nil || w.packageStore == nil || w.jobs == nil || w.skillDao == nil || w.orphanGrace <= 0 {
		return nil
	}
	lookup, ok := w.skillDao.(skillObjectLookup)
	if !ok {
		return nil
	}
	cutoff := time.Now().Add(-w.orphanGrace)
	cursor := ""
	queued := 0
	for {
		listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		objects, next, err := w.packageStore.List(listCtx, "skills/", cursor, 100)
		cancel()
		if err != nil {
			return err
		}
		for _, object := range objects {
			if object.LastModified.IsZero() || object.LastModified.After(cutoff) {
				continue
			}
			referenced, err := lookup.GetSkillByObjectKey(object.Key)
			if err != nil {
				return err
			}
			if referenced != nil {
				continue
			}
			pending, err := w.jobs.HasPendingObjectOperation(object.Key)
			if err != nil {
				return err
			}
			if pending {
				continue
			}
			if _, err := w.jobs.CreateSkillOperationJob(model.SkillOperationJob{
				Operation: model.SkillOperationDeleteObject, IdempotencyKey: "formal-orphan-delete:" + object.Key, ObjectKey: object.Key,
			}); err != nil {
				return err
			}
			queued++
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if queued > 0 {
		slog.Info("queued stale formal Skill object cleanup", "count", queued)
	}
	return nil
}

// CleanupStaleIncoming removes only incoming objects older than the session
// retention window and with no durable delete operation.  Failures are left
// for the next pass, so a transient MinIO outage cannot lose the candidate.
func (w *SkillOperationWorker) CleanupStaleIncoming(ctx context.Context) error {
	if w == nil || w.packageStore == nil || w.jobs == nil {
		return nil
	}
	cutoff := time.Now().Add(-w.incomingTTL)
	cursor := ""
	queued := 0
	for {
		listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		objects, next, err := w.packageStore.List(listCtx, "incoming/", cursor, 100)
		cancel()
		if err != nil {
			return err
		}
		for _, object := range objects {
			if object.LastModified.IsZero() || object.LastModified.After(cutoff) {
				continue
			}
			pending, pendingErr := w.jobs.HasPendingObjectOperation(object.Key)
			if pendingErr != nil {
				return pendingErr
			}
			if pending {
				continue
			}
			if _, err := w.jobs.CreateSkillOperationJob(model.SkillOperationJob{
				Operation:      model.SkillOperationDeleteObject,
				IdempotencyKey: "incoming-expired-delete:" + object.Key,
				ObjectKey:      object.Key,
			}); err != nil {
				return err
			}
			queued++
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if queued > 0 {
		slog.Info("queued stale incoming Skill object cleanup", "count", queued)
	}
	return nil
}

func (w *SkillOperationWorker) RunOnce(ctx context.Context) error {
	job, err := w.jobs.ClaimDueSkillOperationJob(time.Now(), skillOperationLease)
	if err != nil || job == nil {
		return err
	}
	opCtx, cancel := context.WithTimeout(ctx, w.opTimeout)
	err = w.execute(opCtx, job)
	cancel()
	if err != nil {
		if errors.Is(err, errSkillOperationTerminal) {
			// The terminal path already removed the claimed job together with
			// the failed AgentSkill reservation. Do not run the normal completion
			// update against a deleted row.
			return nil
		}
		if retryErr := w.jobs.RetrySkillOperationJob(job.ID, job.LeaseToken, err.Error(), time.Now().Add(retryDelay(job.Attempts))); retryErr != nil {
			return errors.Join(err, retryErr)
		}
		return err
	}
	if err := w.jobs.CompleteSkillOperationJob(job.ID, job.LeaseToken); err != nil {
		return err
	}
	// Install/remove jobs are per-reservation lifecycles with fresh
	// idempotency keys, so successful rows can be removed after completion. Keep
	// verification and migration rows for their deterministic reconciliation
	// keys; those operations intentionally reopen a completed row on a later
	// audit pass.
	if job.Operation == model.SkillOperationDeleteObject ||
		job.Operation == model.SkillOperationInstall ||
		job.Operation == model.SkillOperationRemove {
		return w.jobs.DeleteSkillOperationJob(job.ID, job.LeaseToken)
	}
	return nil
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 {
		attempt = 8
	}
	return time.Duration(1<<(attempt-1)) * time.Minute
}

func (w *SkillOperationWorker) execute(ctx context.Context, job *model.SkillOperationJob) error {
	switch job.Operation {
	case model.SkillOperationDeleteObject:
		return w.executeDelete(ctx, job)
	case model.SkillOperationInstall:
		return w.executeInstall(ctx, job)
	case model.SkillOperationRemove:
		return w.executeRemove(ctx, job)
	case model.SkillOperationVerifyObject:
		return w.executeVerifyObject(ctx, job)
	default:
		return errors.New("unknown skill operation: " + job.Operation)
	}
}

func (w *SkillOperationWorker) executeVerifyObject(ctx context.Context, job *model.SkillOperationJob) error {
	if job.SkillID == nil {
		return errors.New("incomplete Skill object verification operation")
	}
	skill, err := w.skillDao.GetSkillByID(*job.SkillID)
	if err != nil {
		return err
	}
	if skill == nil {
		// The Hub row may have been deleted after the repair task was claimed.
		// Deletion is the terminal owner of the object, so this is an idempotent
		// completion rather than a retryable failure.
		return nil
	}
	if skill.Status == model.SkillStatusDeleting || skill.Status == model.SkillStatusMigrating {
		return nil
	}
	reader := &SkillService{skillDao: w.skillDao, packageStore: w.packageStore, readPreference: w.readPreference, zipLimits: w.zipLimits}
	if _, err := reader.readSkillPackage(ctx, skill); err != nil {
		current, lookupErr := w.skillDao.GetSkillByID(*job.SkillID)
		if lookupErr != nil {
			return errors.Join(err, lookupErr)
		}
		if current != nil && current.Status != model.SkillStatusDeleting {
			if statusStore, ok := w.skillDao.(skillStorageStatusAuditStore); ok {
				statusErr := statusStore.UpdateSkillStatusIfNotDeletingWithAudit(*job.SkillID, model.SkillStatusStorageError, model.SkillAuditEvent{
					Action: "reconcile", Outcome: model.SkillStatusStorageError, SkillID: job.SkillID,
					SkillName: skill.Name, ObjectKey: skill.ObjectKey, SHA256: skill.SHA256, Error: err.Error(),
				})
				if statusErr != nil {
					return errors.Join(err, statusErr)
				}
			} else if statusStore, ok := w.skillDao.(skillStorageErrorStore); ok {
				_ = statusStore.UpdateSkillStatusIfNotDeleting(skill.Name, model.SkillStatusStorageError)
			} else if statusStore, ok := w.skillDao.(skillStatusStore); ok {
				// Test/fallback DAOs may not expose the fenced mutation.  The
				// production GORM DAO always does; keep the fallback bounded by
				// the status read above rather than changing the public contract.
				_ = statusStore.UpdateSkillStatus(skill.Name, model.SkillStatusStorageError)
			}
		}
		return err
	}
	current, err := w.skillDao.GetSkillByID(*job.SkillID)
	if err != nil {
		return err
	}
	if current == nil || current.Status == model.SkillStatusDeleting || current.Status == model.SkillStatusMigrating {
		return nil
	}
	if statusStore, ok := w.skillDao.(skillStorageErrorStore); ok {
		if audited, auditedOK := w.skillDao.(skillStorageStatusAuditStore); auditedOK {
			return audited.UpdateSkillStatusIfNotDeletingWithAudit(*job.SkillID, model.SkillStatusReady, model.SkillAuditEvent{
				Action: "reconcile", Outcome: "repaired", SkillID: job.SkillID,
				SkillName: skill.Name, ObjectKey: skill.ObjectKey, SHA256: skill.SHA256,
			})
		}
		return statusStore.UpdateSkillStatusIfNotDeleting(skill.Name, model.SkillStatusReady)
	}
	if statusStore, ok := w.skillDao.(skillStatusStore); ok {
		return statusStore.UpdateSkillStatus(skill.Name, model.SkillStatusReady)
	}
	return nil
}

func (w *SkillOperationWorker) executeDelete(ctx context.Context, job *model.SkillOperationJob) error {
	// Confirmation creates a claimed delete_object row before promoting the
	// deterministic formal key.  If the worker wins the tiny create/claim race,
	// it must not interpret that still-live protection row as permission to
	// delete a future object.  A request-owned claim is attempt 1; only a later
	// claim after the lease expires is allowed to clean up a crashed confirmer.
	if job != nil && strings.HasPrefix(job.IdempotencyKey, "confirm-object:") && job.Attempts <= 1 {
		return nil
	}
	// A regular delete job carries the Hub row ID. Re-read it before touching
	// MinIO: after a crash, the old row may already be gone and its deterministic
	// object key may have been reused by a newly uploaded Skill with the same
	// name. Never let a stale retry delete that new object's bytes.
	if job.SkillName != "" {
		var current *model.SkillHub
		var err error
		if job.SkillID != nil {
			current, err = w.skillDao.GetSkillByID(*job.SkillID)
		} else {
			// Jobs created by an older Backend may not have a SkillID.  Still
			// fence them by name, object key, and lifecycle state; otherwise a
			// stale retry could delete a newer same-name Skill.
			current, err = w.skillDao.GetSkillByName(job.SkillName)
		}
		if err != nil {
			return err
		}
		if current == nil {
			if job.SkillID != nil || job.ObjectKey == "" {
				// A row-ID job means the database cascade completed before a
				// previous audit/ack step failed.  An old no-ID job without an
				// object key has no safe identity left, so do not cascade by name.
				return w.writeDeleteAudit(job)
			}
			if lookup, ok := w.skillDao.(skillObjectLookup); !ok {
				// Without an object-key reference lookup, deleting an old job's
				// object could race a newly-created Skill. Leave it for explicit
				// reconciliation instead.
				return w.writeDeleteAudit(job)
			} else if referenced, lookupErr := lookup.GetSkillByObjectKey(job.ObjectKey); lookupErr != nil {
				return lookupErr
			} else if referenced != nil {
				return nil
			}
		}
		if current != nil && (current.Name != job.SkillName || (job.ObjectKey != "" && current.ObjectKey != job.ObjectKey) ||
			(current.Status != model.SkillStatusDeleting && current.Status != model.SkillStatusStorageError)) {
			slog.Warn("stale Skill delete job fenced", "job_id", job.ID, "skill", job.SkillName, "skill_id", job.SkillID)
			return nil
		}
	}
	if job.SkillName == "" && strings.HasPrefix(job.ObjectKey, "skills/") {
		if lookup, ok := w.skillDao.(skillObjectLookup); ok {
			referenced, err := lookup.GetSkillByObjectKey(job.ObjectKey)
			if err != nil {
				return err
			}
			if referenced != nil {
				// A formal orphan candidate became referenced after it was queued.
				return nil
			}
		}
	}
	// A stale orphan task can pass the list-time reference check just before a
	// confirmation creates its formal-object protection task.  Once this task
	// is claimed, fence the object side effect against any other pending task
	// for the same key.  The other task will either finish the delete or keep
	// the object protected until the Hub transaction commits.
	if job.SkillName == "" && job.ObjectKey != "" {
		if lookup, ok := w.jobs.(skillPendingObjectOperationExcept); ok {
			pending, err := lookup.HasPendingObjectOperationExcept(job.ObjectKey, job.ID)
			if err != nil {
				return err
			}
			if pending {
				slog.Info("skip Skill object delete while another operation is pending", "object_key", job.ObjectKey, "job_id", job.ID)
				return nil
			}
		}
	}
	if job.ObjectKey != "" {
		if w.packageStore == nil {
			if statusStore, ok := w.skillDao.(skillStatusStore); ok && job.SkillName != "" {
				_ = statusStore.UpdateSkillStatus(job.SkillName, model.SkillStatusStorageError)
			}
			return errors.New("package store unavailable for delete operation")
		}
		if err := w.packageStore.Delete(ctx, job.ObjectKey); err != nil && !errors.Is(err, package_store.ErrNotFound) {
			if statusStore, ok := w.skillDao.(skillStatusStore); ok && job.SkillName != "" {
				_ = statusStore.UpdateSkillStatus(job.SkillName, model.SkillStatusStorageError)
			}
			return err
		}
	}
	if job.SkillName == "" {
		return w.writeDeleteAudit(job)
	}
	if job.SkillName != "" {
		var current *model.SkillHub
		var err error
		if job.SkillID != nil {
			current, err = w.skillDao.GetSkillByID(*job.SkillID)
		} else {
			// Recheck legacy jobs by name after the object side effect.  The
			// original row may have disappeared and been replaced while this
			// worker was deleting the old object; never cascade into that newer
			// row.
			current, err = w.skillDao.GetSkillByName(job.SkillName)
		}
		if err != nil {
			return err
		}
		if current == nil {
			return w.writeDeleteAudit(job)
		}
		if current.Name != job.SkillName || (job.ObjectKey != "" && current.ObjectKey != job.ObjectKey) ||
			(current.Status != model.SkillStatusDeleting && current.Status != model.SkillStatusStorageError) {
			slog.Warn("stale Skill delete job fenced before cascade", "job_id", job.ID, "skill", job.SkillName, "skill_id", job.SkillID)
			return nil
		}
		// Production GORM closes the final crash window by deleting the Hub row,
		// receipts, audit event, and claimed operation in one transaction.  The
		// fallback below remains for test/legacy DAOs that expose only the older
		// split methods.
		if completion, ok := w.skillDao.(skillDeleteCompletionStore); ok && job.ID != 0 {
			event := model.SkillAuditEvent{
				Action: "delete", Outcome: "success", SkillID: job.SkillID,
				SkillName: job.SkillName, ObjectKey: job.ObjectKey, CreatedAt: time.Now(),
			}
			if err := completion.DeleteSkillCascadeWithOperation(job.SkillName, job.ID, job.LeaseToken, event); err != nil {
				if statusStore, statusOK := w.skillDao.(skillStatusStore); statusOK {
					_ = statusStore.UpdateSkillStatus(job.SkillName, model.SkillStatusStorageError)
				}
				return err
			}
			return errSkillOperationTerminal
		}
	}
	if err := w.skillDao.DeleteSkillCascade(job.SkillName); err != nil {
		if statusStore, ok := w.skillDao.(skillStatusStore); ok {
			_ = statusStore.UpdateSkillStatus(job.SkillName, model.SkillStatusStorageError)
		}
		return err
	}
	return w.writeDeleteAudit(job)
}

func (w *SkillOperationWorker) writeDeleteAudit(job *model.SkillOperationJob) error {
	audit, ok := w.skillDao.(skillAuditStore)
	if !ok {
		return nil
	}
	event := model.SkillAuditEvent{
		Outcome: "deleted", SkillID: job.SkillID, SkillName: job.SkillName,
		ObjectKey: job.ObjectKey, CreatedAt: time.Now(),
	}
	if job.SkillName == "" {
		event.Action = "reconcile"
		event.Error = "incoming_expired"
		if strings.HasPrefix(job.ObjectKey, "skills/") {
			event.Error = "formal_orphan"
		}
	} else {
		event.Action = "delete"
	}
	return audit.CreateSkillAuditEvent(event)
}

func (w *SkillOperationWorker) executeInstall(ctx context.Context, job *model.SkillOperationJob) error {
	if job.SkillID == nil || job.SessionID == "" || job.SkillName == "" {
		return errors.New("incomplete install operation")
	}
	skill, err := w.skillDao.GetSkillByID(*job.SkillID)
	if err != nil {
		return err
	}
	if skill == nil {
		return errors.New("skill for install operation not found")
	}
	if reservation, fenced, lookupErr := w.agentSkillForJob(job); lookupErr != nil {
		return lookupErr
	} else if fenced {
		if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving || reservation.Status == model.AgentSkillStatusReady {
			return nil
		}
	} else if lookup, ok := w.skillDao.(agentSkillStatusLookup); ok {
		reservation, lookupErr := lookup.GetAgentSkill(job.SessionID, job.SkillName)
		if lookupErr != nil {
			return lookupErr
		}
		if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving || reservation.Status == model.AgentSkillStatusReady {
			// Removal (or a previous successful attempt) already owns the
			// reservation. Do not call AgentEnd for a stale retry.
			return nil
		}
	}
	reader := &SkillService{skillDao: w.skillDao, packageStore: w.packageStore, readPreference: w.readPreference, zipLimits: w.zipLimits}
	zipData, err := reader.readSkillPackage(ctx, skill)
	if err != nil {
		w.queueObjectVerification(skill, err)
		if statusStore, ok := w.skillDao.(skillStorageStatusAuditStore); ok {
			_ = statusStore.UpdateSkillStatusIfNotDeletingWithAudit(*job.SkillID, model.SkillStatusStorageError, model.SkillAuditEvent{
				Action: "reconcile", Outcome: model.SkillStatusStorageError, SkillID: job.SkillID,
				SkillName: skill.Name, ObjectKey: skill.ObjectKey, SHA256: skill.SHA256, Error: err.Error(),
			})
		} else if statusStore, ok := w.skillDao.(skillStatusStore); ok {
			_ = statusStore.UpdateSkillStatus(job.SkillName, model.SkillStatusStorageError)
		}
		w.markAgentSkillInstallErrorForJob(job)
		return err
	}
	if reservation, fenced, lookupErr := w.agentSkillForJob(job); lookupErr != nil {
		return lookupErr
	} else if fenced {
		if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving || reservation.Status == model.AgentSkillStatusReady {
			return nil
		}
	} else if lookup, ok := w.skillDao.(agentSkillStatusLookup); ok {
		reservation, lookupErr := lookup.GetAgentSkill(job.SessionID, job.SkillName)
		if lookupErr != nil {
			return lookupErr
		}
		if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving || reservation.Status == model.AgentSkillStatusReady {
			return nil
		}
	}
	if err := reader.installSkill(ctx, job.AgentType, job.SessionID, job.SkillName, zipData); err != nil {
		if isKnownSkillAgentFailure(err) {
			if rollbackErr := rollbackKnownAgentSkillInstall(w.skillDao, w.jobs, job); rollbackErr != nil {
				w.markAgentSkillInstallErrorForJob(job)
				return errors.Join(err, rollbackErr)
			}
			return errSkillOperationTerminal
		}
		w.markAgentSkillInstallErrorForJob(job)
		return err
	}
	if reservation, fenced, lookupErr := w.agentSkillForJob(job); lookupErr != nil {
		return lookupErr
	} else if fenced {
		if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving {
			// Removal may have won while AgentEnd was installing. Compensate
			// with an idempotent remove so the worktree cannot outlive the row.
			if removeErr := w.compensateInstallRemoval(ctx, reader, job); removeErr != nil {
				return removeErr
			}
			return nil
		}
	} else if lookup, ok := w.skillDao.(agentSkillStatusLookup); ok {
		reservation, lookupErr := lookup.GetAgentSkill(job.SessionID, job.SkillName)
		if lookupErr != nil {
			return lookupErr
		}
		if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving {
			// Removal may have won while AgentEnd was installing. Compensate
			// with an idempotent remove so the worktree cannot outlive the row.
			if removeErr := w.compensateInstallRemoval(ctx, reader, job); removeErr != nil {
				return removeErr
			}
			return nil
		}
	}
	if job.AgentSkillID != nil {
		if completion, ok := w.skillDao.(agentSkillInstallCompletionByIDStore); ok {
			updated, updateErr := completion.CompleteAgentSkillInstallByID(*job.AgentSkillID)
			if updateErr != nil {
				w.markAgentSkillInstallErrorForJob(job)
				return updateErr
			}
			if !updated {
				// The reservation may have been changed after the last fence. A
				// concurrent remove owns cleanup; any other lost conditional
				// update must stay retryable instead of silently completing a job
				// whose AgentSkill is still not ready.
				if reservation, fenced, lookupErr := w.agentSkillForJob(job); lookupErr != nil {
					return lookupErr
				} else if fenced {
					if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving {
						if removeErr := w.compensateInstallRemoval(ctx, reader, job); removeErr != nil {
							return removeErr
						}
						return nil
					}
					if reservation.Status == model.AgentSkillStatusReady {
						return nil
					}
				}
				return errors.New("agent skill install status update was fenced")
			}
			return nil
		}
	}
	if completion, ok := w.skillDao.(agentSkillInstallCompletionStore); ok {
		updated, updateErr := completion.CompleteAgentSkillInstall(job.SessionID, job.SkillName)
		if updateErr != nil {
			w.markAgentSkillInstallErrorForJob(job)
			return updateErr
		}
		if !updated {
			if lookup, lookupOK := w.skillDao.(agentSkillStatusLookup); lookupOK {
				reservation, lookupErr := lookup.GetAgentSkill(job.SessionID, job.SkillName)
				if lookupErr != nil {
					return lookupErr
				}
				if reservation == nil || reservation.Status == model.AgentSkillStatusRemoving {
					if removeErr := w.compensateInstallRemoval(ctx, reader, job); removeErr != nil {
						return removeErr
					}
					return nil
				}
				if reservation.Status == model.AgentSkillStatusReady {
					return nil
				}
			}
			return errors.New("agent skill install status update was fenced")
		}
		return nil
	}
	return w.skillDao.UpdateAgentSkillStatus(job.SessionID, job.SkillName, model.AgentSkillStatusReady)
}

func (w *SkillOperationWorker) markAgentSkillInstallError(sessionID, skillName string) {
	// Callers that have an AgentSkillID use markAgentSkillInstallErrorForJob so
	// a late error cannot update a newer relation with the same pair.
	if fenced, ok := w.skillDao.(agentSkillInstallErrorStore); ok {
		_, _ = fenced.MarkAgentSkillInstallError(sessionID, skillName)
		return
	}
	_ = w.skillDao.UpdateAgentSkillStatus(sessionID, skillName, model.AgentSkillStatusSyncError)
}

func (w *SkillOperationWorker) markAgentSkillInstallErrorForJob(job *model.SkillOperationJob) {
	if job != nil && job.AgentSkillID != nil {
		if fenced, ok := w.skillDao.(agentSkillInstallErrorByIDStore); ok {
			_, _ = fenced.MarkAgentSkillInstallErrorByID(*job.AgentSkillID)
			return
		}
	}
	w.markAgentSkillInstallError(job.SessionID, job.SkillName)
}

func (w *SkillOperationWorker) executeRemove(ctx context.Context, job *model.SkillOperationJob) error {
	if job.SessionID == "" || job.SkillName == "" || job.AgentType == "" {
		return errors.New("incomplete remove operation")
	}
	if reservation, fenced, lookupErr := w.agentSkillForJob(job); lookupErr != nil {
		return lookupErr
	} else if fenced {
		if reservation == nil || (reservation.Status != model.AgentSkillStatusRemoving && reservation.Status != model.AgentSkillStatusSyncError) {
			return nil
		}
	} else if lookup, ok := w.skillDao.(agentSkillStatusLookup); ok {
		reservation, err := lookup.GetAgentSkill(job.SessionID, job.SkillName)
		if err != nil {
			return err
		}
		if reservation == nil || (reservation.Status != model.AgentSkillStatusRemoving && reservation.Status != model.AgentSkillStatusSyncError) {
			// The original reservation was already removed, or a newer import
			// owns this session/Skill pair. Do not let a stale retry remove it.
			return nil
		}
	}
	reader := &SkillService{skillDao: w.skillDao, agentClient: w.agentClient}
	if err := reader.removeSkill(ctx, job.AgentType, job.SessionID, job.SkillName); err != nil {
		if isKnownSkillAgentFailure(err) {
			if rollbackErr := restoreKnownAgentSkillRemove(w.skillDao, w.jobs, job, job.SessionID, job.SkillName); rollbackErr != nil {
				w.markAgentSkillRemoveErrorForJob(job)
				return errors.Join(err, rollbackErr)
			}
			return errSkillOperationTerminal
		}
		if job.AgentSkillID != nil {
			if fenced, ok := w.skillDao.(agentSkillStatusByIDStore); ok {
				_, _ = fenced.UpdateAgentSkillStatusByID(*job.AgentSkillID, model.AgentSkillStatusSyncError)
			} else {
				_ = w.skillDao.UpdateAgentSkillStatus(job.SessionID, job.SkillName, model.AgentSkillStatusSyncError)
			}
		} else {
			_ = w.skillDao.UpdateAgentSkillStatus(job.SessionID, job.SkillName, model.AgentSkillStatusSyncError)
		}
		return err
	}
	if reservation, fenced, lookupErr := w.agentSkillForJob(job); lookupErr != nil {
		return lookupErr
	} else if fenced {
		if reservation == nil || (reservation.Status != model.AgentSkillStatusRemoving && reservation.Status != model.AgentSkillStatusSyncError) {
			return nil
		}
	} else if lookup, ok := w.skillDao.(agentSkillStatusLookup); ok {
		reservation, err := lookup.GetAgentSkill(job.SessionID, job.SkillName)
		if err != nil {
			return err
		}
		if reservation == nil || (reservation.Status != model.AgentSkillStatusRemoving && reservation.Status != model.AgentSkillStatusSyncError) {
			return nil
		}
	}
	if job.AgentSkillID != nil {
		if completion, ok := w.skillDao.(skillRemoveCompletionStore); ok && job.ID != 0 {
			event := model.SkillAuditEvent{
				Action: "remove", Outcome: "success", SkillName: job.SkillName, CreatedAt: time.Now(),
			}
			if err := completion.CompleteAgentSkillRemovalByID(*job.AgentSkillID, job.ID, job.LeaseToken, event); err != nil {
				w.markAgentSkillRemoveErrorForJob(job)
				return err
			}
			return errSkillOperationTerminal
		}
		if fenced, ok := w.skillDao.(agentSkillRemovalByIDStore); ok {
			if err := fenced.DeleteAgentSkillByID(*job.AgentSkillID); err != nil {
				w.markAgentSkillRemoveErrorForJob(job)
				return err
			}
			return nil
		}
	}
	if err := w.skillDao.DeleteAgentSkill(job.SessionID, job.SkillName); err != nil {
		w.markAgentSkillRemoveErrorForJob(job)
		return err
	}
	return nil
}

func (w *SkillOperationWorker) markAgentSkillRemoveErrorForJob(job *model.SkillOperationJob) {
	if job == nil {
		return
	}
	if job.AgentSkillID != nil {
		if fenced, ok := w.skillDao.(agentSkillStatusByIDStore); ok {
			_, _ = fenced.UpdateAgentSkillStatusByID(*job.AgentSkillID, model.AgentSkillStatusSyncError)
			return
		}
	}
	_ = w.skillDao.UpdateAgentSkillStatus(job.SessionID, job.SkillName, model.AgentSkillStatusSyncError)
}
