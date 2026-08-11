// Command skill-reconcile performs bounded, read-mostly checks between the
// Skill Hub metadata and its private MinIO bucket.  It is safe by default:
// repair is opt-in and only removes objects outside the configured grace
// period that have no database reference or pending operation.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"agenthub/backend/internal/conf"
	gormdao "agenthub/backend/internal/dao/gorm"
	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/db"
	"agenthub/backend/pkg/package_store"
)

type options struct {
	config       string
	batch        int
	repair       bool
	verify       bool
	verifyShadow bool
	orphanGrace  time.Duration
	incomingTTL  time.Duration
}

func main() {
	var rawGrace, rawIncoming string
	var opt options
	flag.StringVar(&opt.config, "config", "configs/config.yaml", "backend config path")
	flag.IntVar(&opt.batch, "batch-size", 100, "metadata/object page size")
	flag.BoolVar(&opt.repair, "repair", false, "delete only confirmed stale orphan/incoming objects")
	flag.BoolVar(&opt.verify, "verify", true, "verify referenced object size and SHA-256")
	flag.BoolVar(&opt.verifyShadow, "verify-shadow", true, "compare non-empty legacy Content shadow copies with MinIO")
	flag.StringVar(&rawGrace, "orphan-grace", "", "minimum age before an unreferenced formal object is a candidate (defaults to config)")
	flag.StringVar(&rawIncoming, "incoming-ttl", "", "minimum age before an incoming object is a cleanup candidate (defaults to config)")
	flag.Parse()
	if opt.batch <= 0 {
		fatal("batch-size must be positive")
	}
	cfg, err := conf.Load(opt.config)
	if err != nil {
		fatal("load config: %v", err)
	}
	if !cfg.SkillStorage.Enabled {
		fatal("skill_storage.enabled must be true for reconciliation")
	}
	if err := db.Init(&cfg.MySQL); err != nil {
		fatal("init db: %v", err)
	}
	defer db.Close()
	if rawGrace == "" {
		rawGrace = cfg.SkillStorage.OrphanGracePeriod
		if rawGrace == "" {
			rawGrace = "48h"
		}
	}
	if rawIncoming == "" {
		rawIncoming = cfg.SkillStorage.IncomingTTL
		if rawIncoming == "" {
			rawIncoming = "24h"
		}
	}
	if opt.orphanGrace, err = time.ParseDuration(rawGrace); err != nil || opt.orphanGrace <= 0 {
		fatal("invalid orphan-grace: %v", err)
	}
	if opt.incomingTTL, err = time.ParseDuration(rawIncoming); err != nil || opt.incomingTTL <= 0 {
		fatal("invalid incoming-ttl: %v", err)
	}
	packageLimit := service.MaxPackageSize
	if raw := cfg.SkillStorage.MaxPackageSize; raw != "" {
		packageLimit, err = conf.ParseByteSize(raw)
		if err != nil {
			fatal("invalid skill_storage.max_package_size: %v", err)
		}
	}
	store, err := package_store.NewMinIOStore(package_store.MinIOConfig{
		Endpoint: cfg.SkillStorage.Endpoint, Bucket: cfg.SkillStorage.Bucket,
		AccessKey: cfg.SkillStorage.AccessKey, SecretKey: cfg.SkillStorage.SecretKey, UseSSL: cfg.SkillStorage.UseSSL,
		CAFile: cfg.SkillStorage.CAFile,
	})
	if err != nil {
		fatal("init MinIO store: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	skillDao := gormdao.NewSkillDao()
	jobDao := gormdao.NewSkillOperationDao()
	if opt.verify {
		if err := verifyReferenced(ctx, skillDao, jobDao, store, opt.batch, packageLimit); err != nil {
			fatal("verify referenced objects: %v", err)
		}
		if opt.verifyShadow {
			if err := verifyShadowCopies(ctx, skillDao, store, opt.batch, cfg.SkillStorage.ShadowWriteBlob, packageLimit); err != nil {
				fatal("verify shadow BLOB copies: %v", err)
			}
		}
		if err := verifyReceipts(skillDao, opt.batch); err != nil {
			fatal("verify upload receipts: %v", err)
		}
	}
	if stuck, err := jobDao.CountStuckJobs(time.Now()); err != nil {
		fatal("inspect operation leases: %v", err)
	} else if stuck > 0 {
		slog.Warn("operation jobs have expired leases", "count", stuck)
	}
	if err := reconcileObjects(ctx, skillDao, jobDao, store, opt); err != nil {
		fatal("reconcile objects: %v", err)
	}
}

func verifyReceipts(dao *gormdao.SkillDao, batch int) error {
	cursor := ""
	var firstErr error
	for {
		receipts, err := dao.ListSkillUploadReceiptsAfter(cursor, batch)
		if err != nil {
			return err
		}
		if len(receipts) == 0 {
			return firstErr
		}
		for _, receipt := range receipts {
			cursor = receipt.UploadID
			skill, err := dao.GetSkillByID(receipt.SkillID)
			if err != nil {
				return err
			}
			if skill == nil || receipt.SHA256 == "" || skill.SHA256 == "" || !strings.EqualFold(receipt.SHA256, skill.SHA256) {
				if firstErr == nil {
					firstErr = fmt.Errorf("receipt %s is dangling or hash-inconsistent", receipt.UploadID)
				}
			}
		}
		if len(receipts) < batch {
			return firstErr
		}
	}
}

func verifyReferenced(ctx context.Context, dao *gormdao.SkillDao, jobs *gormdao.SkillOperationDao, store *package_store.MinIOStore, batch int, packageLimit int64) error {
	after := uint(0)
	var firstErr error
	for {
		skills, err := dao.ListSkillStorageMetadataAfter(after, batch)
		if err != nil {
			return err
		}
		if len(skills) == 0 {
			return firstErr
		}
		for _, skill := range skills {
			after = skill.ID
			if skill.Status == model.SkillStatusDeleting || skill.Status == model.SkillStatusMigrating {
				// Deletion owns the object lifecycle; a concurrent missing-object
				// observation must not resurrect the row or enqueue a repair.
				continue
			}
			if skill.StorageType != model.SkillStorageMinIO || strings.TrimSpace(skill.ObjectKey) == "" {
				mismatchErr := fmt.Errorf("%s: inconsistent MinIO storage metadata", skill.Name)
				statusErr := markSkillStorageError(dao, skill, mismatchErr)
				queueObjectVerification(jobs, skill)
				if statusErr != nil {
					mismatchErr = errors.Join(mismatchErr, statusErr)
				}
				if firstErr == nil {
					firstErr = mismatchErr
				}
				continue
			}
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			info, err := store.Stat(callCtx, skill.ObjectKey)
			if err == nil {
				if skill.PackageSize > 0 && info.Size != skill.PackageSize {
					mismatchErr := fmt.Errorf("%s: object size mismatch", skill.Name)
					statusErr := markSkillStorageError(dao, skill, mismatchErr)
					queueObjectVerification(jobs, skill)
					cancel()
					if statusErr != nil {
						mismatchErr = errors.Join(mismatchErr, statusErr)
					}
					if firstErr == nil {
						firstErr = mismatchErr
					}
					continue
				}
				data, readErr := readObject(callCtx, store, skill.ObjectKey, info.Size, packageLimit)
				if readErr != nil || skill.SHA256 == "" || !strings.EqualFold(skill.SHA256, hashBytes(data)) {
					causeErr := readErr
					if causeErr == nil {
						causeErr = fmt.Errorf("object SHA-256 mismatch")
					}
					mismatchErr := fmt.Errorf("%s: %w", skill.Name, causeErr)
					statusErr := markSkillStorageError(dao, skill, causeErr)
					queueObjectVerification(jobs, skill)
					cancel()
					if statusErr != nil {
						mismatchErr = errors.Join(mismatchErr, statusErr)
					}
					if firstErr == nil {
						firstErr = mismatchErr
					}
					continue
				}
			} else {
				statusErr := markSkillStorageError(dao, skill, err)
				mismatchErr := fmt.Errorf("%s: %w", skill.Name, err)
				queueObjectVerification(jobs, skill)
				cancel()
				if statusErr != nil {
					mismatchErr = errors.Join(mismatchErr, statusErr)
				}
				if firstErr == nil {
					firstErr = mismatchErr
				}
				continue
			}
			cancel()
		}
		if len(skills) < batch {
			return firstErr
		}
	}
}

func queueObjectVerification(jobs *gormdao.SkillOperationDao, skill model.SkillHub) {
	if jobs == nil {
		return
	}
	skillID := skill.ID
	_, err := jobs.CreateSkillOperationJob(model.SkillOperationJob{
		Operation:      model.SkillOperationVerifyObject,
		IdempotencyKey: fmt.Sprintf("verify-object:%d:%s", skill.ID, skill.SHA256),
		SkillID:        &skillID, SkillName: skill.Name, ObjectKey: skill.ObjectKey,
	})
	if err != nil {
		slog.Warn("queue Skill object verification failed", "skill", skill.Name, "error", err)
	}
}

func reconcileObjects(ctx context.Context, skills *gormdao.SkillDao, jobs *gormdao.SkillOperationDao, store *package_store.MinIOStore, opt options) error {
	cutoff := time.Now().Add(-opt.orphanGrace)
	items, cursor := 0, ""
	for {
		listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		objects, next, err := store.List(listCtx, "skills/", cursor, opt.batch)
		cancel()
		if err != nil {
			return err
		}
		for _, object := range objects {
			items++
			if object.LastModified.IsZero() || object.LastModified.After(cutoff) {
				continue
			}
			referenced, err := skills.GetSkillByObjectKey(object.Key)
			if err != nil {
				return err
			}
			if referenced != nil {
				continue
			}
			pending, err := jobs.HasPendingObjectOperation(object.Key)
			if err != nil {
				return err
			}
			if pending {
				continue
			}
			slog.Warn("formal Skill object is an orphan candidate", "object_key", object.Key, "last_modified", object.LastModified, "repair", opt.repair)
			if opt.repair {
				if err := repairDeleteObject(ctx, skills, jobs, store, object.Key, "formal_orphan"); err != nil {
					return err
				}
			}
		}
		if next == "" {
			break
		}
		cursor = next
	}

	cursor = ""
	incomingCutoff := time.Now().Add(-opt.incomingTTL)
	for {
		listCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		objects, next, err := store.List(listCtx, "incoming/", cursor, opt.batch)
		cancel()
		if err != nil {
			return err
		}
		for _, object := range objects {
			if object.LastModified.IsZero() || object.LastModified.After(incomingCutoff) {
				continue
			}
			pending, err := jobs.HasPendingObjectOperation(object.Key)
			if err != nil {
				return err
			}
			if pending {
				// A confirmation cleanup task may still own this object.  Never
				// delete it merely because the Redis session has expired.
				continue
			}
			slog.Warn("incoming Skill object is expired", "object_key", object.Key, "last_modified", object.LastModified, "repair", opt.repair)
			if opt.repair {
				if err := repairDeleteObject(ctx, skills, jobs, store, object.Key, "incoming_expired"); err != nil {
					return err
				}
			}
		}
		if next == "" {
			break
		}
		cursor = next
	}
	slog.Info("Skill object reconciliation completed", "formal_objects_scanned", items, "repair", opt.repair)
	return nil
}

// verifyShadowCopies is intentionally bounded by the same metadata page size
// as the regular MinIO verification.  Empty Content means the observation
// window has ended for that row and is not treated as a mismatch.
func verifyShadowCopies(ctx context.Context, dao *gormdao.SkillDao, store *package_store.MinIOStore, batch int, requireShadow bool, packageLimit int64) error {
	after := uint(0)
	checked := 0
	var firstErr error
	for {
		skills, err := dao.ListSkillStorageMetadataAfter(after, batch)
		if err != nil {
			return err
		}
		if len(skills) == 0 {
			slog.Info("Skill shadow BLOB verification completed", "checked", checked)
			return firstErr
		}
		for _, skill := range skills {
			after = skill.ID
			if skill.Status == model.SkillStatusDeleting || skill.Status == model.SkillStatusMigrating {
				continue
			}
			content, err := dao.GetSkillContentByIDLimited(skill.ID, packageLimit)
			if err != nil {
				return err
			}
			if len(content) == 0 {
				if requireShadow {
					mismatchErr := fmt.Errorf("%s: shadow BLOB is missing", skill.Name)
					statusErr := markSkillStorageError(dao, skill, mismatchErr)
					if statusErr != nil {
						mismatchErr = errors.Join(mismatchErr, statusErr)
					}
					if firstErr == nil {
						firstErr = mismatchErr
					}
				}
				continue
			}
			checked++
			if packageLimit > 0 && int64(len(content)) > packageLimit {
				mismatchErr := fmt.Errorf("%s: shadow BLOB exceeds configured package limit", skill.Name)
				statusErr := markSkillStorageError(dao, skill, mismatchErr)
				if statusErr != nil {
					mismatchErr = errors.Join(mismatchErr, statusErr)
				}
				if firstErr == nil {
					firstErr = mismatchErr
				}
				continue
			}
			if skill.PackageSize > 0 && int64(len(content)) != skill.PackageSize {
				mismatchErr := fmt.Errorf("%s: shadow BLOB size mismatch", skill.Name)
				statusErr := markSkillStorageError(dao, skill, mismatchErr)
				if statusErr != nil {
					mismatchErr = errors.Join(mismatchErr, statusErr)
				}
				if firstErr == nil {
					firstErr = mismatchErr
				}
				continue
			}
			if skill.SHA256 == "" || !strings.EqualFold(skill.SHA256, hashBytes(content)) {
				mismatchErr := fmt.Errorf("%s: shadow BLOB SHA-256 mismatch", skill.Name)
				statusErr := markSkillStorageError(dao, skill, mismatchErr)
				if statusErr != nil {
					mismatchErr = errors.Join(mismatchErr, statusErr)
				}
				if firstErr == nil {
					firstErr = mismatchErr
				}
				continue
			}
		}
		if len(skills) < batch {
			slog.Info("Skill shadow BLOB verification completed", "checked", checked)
			return firstErr
		}
	}
}

func markSkillStorageError(dao *gormdao.SkillDao, skill model.SkillHub, cause error) error {
	if dao == nil {
		return nil
	}
	event := model.SkillAuditEvent{
		Action: "reconcile", Outcome: model.SkillStatusStorageError, SkillID: &skill.ID,
		SkillName: skill.Name, ObjectKey: skill.ObjectKey, SHA256: skill.SHA256,
	}
	if cause != nil {
		event.Error = cause.Error()
	}
	return dao.UpdateSkillStatusIfNotDeletingWithAudit(skill.ID, model.SkillStatusStorageError, event)
}

func auditRepair(dao *gormdao.SkillDao, objectKey, kind string) error {
	if dao == nil {
		return nil
	}
	if err := dao.CreateSkillAuditEvent(model.SkillAuditEvent{Action: "reconcile", Outcome: "deleted", ObjectKey: objectKey, Error: kind, CreatedAt: time.Now()}); err != nil {
		return fmt.Errorf("write reconciliation audit event for %s: %w", objectKey, err)
	}
	return nil
}

func deleteObject(ctx context.Context, store *package_store.MinIOStore, key string) error {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := store.Delete(callCtx, key); err != nil && !errors.Is(err, package_store.ErrNotFound) {
		return err
	}
	return nil
}

func repairDeleteObject(ctx context.Context, skills *gormdao.SkillDao, jobs *gormdao.SkillOperationDao, store *package_store.MinIOStore, key, kind string) error {
	if kind == "formal_orphan" && skills != nil {
		referenced, err := skills.GetSkillByObjectKey(key)
		if err != nil {
			return err
		}
		if referenced != nil {
			return nil
		}
	}
	// Use the same deterministic keys as the long-running cleanup worker.  A
	// one-shot reconciliation command may overlap with that worker; sharing the
	// key makes CreateSkillOperationJob return the existing durable task instead
	// of scheduling a second delete for the same object.
	idempotencyKey := "reconcile-delete:" + kind + ":" + key
	switch kind {
	case "formal_orphan":
		idempotencyKey = "formal-orphan-delete:" + key
	case "incoming_expired":
		idempotencyKey = "incoming-expired-delete:" + key
	}
	job, err := jobs.CreateSkillOperationJob(model.SkillOperationJob{
		Operation: model.SkillOperationDeleteObject, IdempotencyKey: idempotencyKey, ObjectKey: key,
	})
	if err != nil {
		return err
	}
	claimed, err := jobs.ClaimSkillOperationJob(job.ID, time.Now(), 5*time.Minute)
	if err != nil {
		return err
	}
	if claimed == nil {
		return nil
	}
	if kind == "formal_orphan" && skills != nil {
		referenced, err := skills.GetSkillByObjectKey(key)
		if err != nil {
			_ = jobs.RetrySkillOperationJob(claimed.ID, claimed.LeaseToken, err.Error(), time.Now().Add(time.Minute))
			return err
		}
		if referenced != nil {
			_ = jobs.CompleteSkillOperationJob(claimed.ID, claimed.LeaseToken)
			return jobs.DeleteSkillOperationJob(claimed.ID, claimed.LeaseToken)
		}
	}
	// The list-time orphan check can race a confirmation that has just created
	// its formal-object protection job.  Recheck the outbox after claiming the
	// repair task, excluding this task itself; if another operation owns the
	// object, leave deletion to that operation and remove this duplicate task.
	if pending, pendingErr := jobs.HasPendingObjectOperationExcept(key, claimed.ID); pendingErr != nil {
		_ = jobs.RetrySkillOperationJob(claimed.ID, claimed.LeaseToken, pendingErr.Error(), time.Now().Add(time.Minute))
		return pendingErr
	} else if pending {
		if err := jobs.CompleteSkillOperationJob(claimed.ID, claimed.LeaseToken); err != nil {
			return err
		}
		return jobs.DeleteSkillOperationJob(claimed.ID, claimed.LeaseToken)
	}
	if err := deleteObject(ctx, store, key); err != nil {
		_ = jobs.RetrySkillOperationJob(claimed.ID, claimed.LeaseToken, err.Error(), time.Now().Add(time.Minute))
		return err
	}
	// Keep the durable job retryable until the audit record is committed. A
	// successful object deletion followed by a database outage must not leave
	// an untracked repair with no way to reconstruct its audit trail.
	if err := auditRepair(skills, key, kind); err != nil {
		_ = jobs.RetrySkillOperationJob(claimed.ID, claimed.LeaseToken, err.Error(), time.Now().Add(time.Minute))
		return err
	}
	if err := jobs.CompleteSkillOperationJob(claimed.ID, claimed.LeaseToken); err != nil {
		return err
	}
	return jobs.DeleteSkillOperationJob(claimed.ID, claimed.LeaseToken)
}

func readObject(ctx context.Context, store *package_store.MinIOStore, key string, size, packageLimit int64) ([]byte, error) {
	if packageLimit <= 0 {
		packageLimit = service.MaxPackageSize
	}
	if packageLimit == 1<<63-1 || size < 0 || size > packageLimit {
		return nil, fmt.Errorf("object exceeds package limit")
	}
	rc, err := store.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, packageLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size || int64(len(data)) > packageLimit {
		return nil, fmt.Errorf("object size mismatch")
	}
	return data, nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func fatal(format string, args ...interface{}) {
	slog.Error(fmt.Sprintf(format, args...))
	os.Exit(1)
}
