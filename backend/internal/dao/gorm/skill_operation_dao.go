package gormdao

import (
	"errors"
	"strings"
	"time"

	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SkillOperationDao struct{}

func NewSkillOperationDao() *SkillOperationDao { return &SkillOperationDao{} }

func (dao *SkillOperationDao) CreateSkillOperationJob(job model.SkillOperationJob) (*model.SkillOperationJob, error) {
	return createSkillOperationJob(db.GetDB(), job)
}

func createSkillOperationJob(tx *gorm.DB, job model.SkillOperationJob) (*model.SkillOperationJob, error) {
	if job.IdempotencyKey == "" || job.Operation == "" {
		return nil, errors.New("operation and idempotency key are required")
	}
	if job.Status == "" {
		job.Status = model.SkillJobStatusPending
	}
	var existing model.SkillOperationJob
	err := tx.Where("idempotency_key = ?", job.IdempotencyKey).First(&existing).Error
	if err == nil {
		// Reconciliation intentionally reuses a deterministic verification key.
		// If an earlier verification completed but a later audit found the same
		// object corrupted again, reopen that done repair task instead of
		// permanently suppressing future repairs behind the idempotency row.
		if (existing.Operation == model.SkillOperationVerifyObject ||
			(existing.Operation == model.SkillOperationDeleteObject && strings.HasPrefix(existing.IdempotencyKey, "formal-orphan-delete:"))) && existing.Status == model.SkillJobStatusDone {
			if err := tx.Model(&model.SkillOperationJob{}).Where("id = ? AND status = ?", existing.ID, model.SkillJobStatusDone).
				Updates(map[string]interface{}{
					"status": model.SkillJobStatusPending, "lease_until": nil, "lease_token": "",
					"next_retry_at": nil, "last_error": "reopened by reconciliation",
				}).Error; err != nil {
				return nil, err
			}
			existing.Status = model.SkillJobStatusPending
			existing.LeaseUntil = nil
			existing.LeaseToken = ""
			existing.NextRetryAt = nil
			existing.LastError = "reopened by reconciliation"
		}
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := tx.Create(&job).Error; err != nil {
		if isDuplicateKeyError(err) {
			if retryErr := tx.Where("idempotency_key = ?", job.IdempotencyKey).First(&existing).Error; retryErr == nil {
				return &existing, nil
			}
		}
		return nil, err
	}
	return &job, nil
}

func (dao *SkillOperationDao) ClaimSkillOperationJob(id uint64, now time.Time, lease time.Duration) (*model.SkillOperationJob, error) {
	return claimSkillOperationJob(id, now, lease)
}

func (dao *SkillOperationDao) ClaimDueSkillOperationJob(now time.Time, lease time.Duration) (*model.SkillOperationJob, error) {
	return dao.claimDueSkillOperationJob(now, lease, "")
}

// ClaimDueSkillMigrationJob is used by the resumable migration command.  It
// filters the shared outbox so a one-shot migration process never steals an
// install/remove/delete job from the Backend worker.
func (dao *SkillOperationDao) ClaimDueSkillMigrationJob(now time.Time, lease time.Duration) (*model.SkillOperationJob, error) {
	return dao.claimDueSkillOperationJob(now, lease, "migrate")
}

func (dao *SkillOperationDao) claimDueSkillOperationJob(now time.Time, lease time.Duration, operation string) (*model.SkillOperationJob, error) {
	var claimed *model.SkillOperationJob
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		var job model.SkillOperationJob
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("operation <> ?", model.SkillOperationMigrate).
			Where("(status = ? OR (status = ? AND lease_until IS NOT NULL AND lease_until < ?))", model.SkillJobStatusPending, model.SkillJobStatusRunning, now).
			Where("next_retry_at IS NULL OR next_retry_at <= ?", now).
			Order("created_at ASC").First(&job)
		if operation != "" {
			query = tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
				Where("operation = ?", operation).
				Where("(status = ? OR (status = ? AND lease_until IS NOT NULL AND lease_until < ?))", model.SkillJobStatusPending, model.SkillJobStatusRunning, now).
				Where("next_retry_at IS NULL OR next_retry_at <= ?", now).
				Order("created_at ASC").First(&job)
		}
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if query.Error != nil {
			return query.Error
		}
		return claimLockedJob(tx, &job, now, lease, &claimed)
	})
	return claimed, err
}

func (dao *SkillOperationDao) UpdateSkillOperationObjectKey(id uint64, leaseToken, objectKey string) error {
	query := db.GetDB().Model(&model.SkillOperationJob{}).Where("id = ? AND status = ?", id, model.SkillJobStatusRunning)
	if leaseToken != "" {
		query = query.Where("lease_token = ?", leaseToken)
	}
	return query.Update("object_key", objectKey).Error
}

func claimSkillOperationJob(id uint64, now time.Time, lease time.Duration) (*model.SkillOperationJob, error) {
	var claimed *model.SkillOperationJob
	err := db.GetDB().Transaction(func(tx *gorm.DB) error {
		var job model.SkillOperationJob
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&job)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			// The creator and claimer normally run back-to-back, but a
			// concurrent cleanup may remove the row.  Treat that as a missing
			// claim rather than misreporting it as a duplicate reservation.
			return nil
		}
		if query.Error != nil {
			return query.Error
		}
		if job.Status == model.SkillJobStatusDone || (job.Status == model.SkillJobStatusRunning && job.LeaseUntil != nil && job.LeaseUntil.After(now)) {
			return nil
		}
		return claimLockedJob(tx, &job, now, lease, &claimed)
	})
	return claimed, err
}

func claimLockedJob(tx *gorm.DB, job *model.SkillOperationJob, now time.Time, lease time.Duration, result **model.SkillOperationJob) error {
	leaseUntil := now.Add(lease)
	token := uuid.NewString()
	updates := map[string]interface{}{
		"status": model.SkillJobStatusRunning, "attempts": gorm.Expr("attempts + 1"),
		"lease_until": leaseUntil, "lease_token": token, "last_error": "",
	}
	if err := tx.Model(job).Updates(updates).Error; err != nil {
		return err
	}
	job.Status = model.SkillJobStatusRunning
	job.Attempts++
	job.LeaseUntil = &leaseUntil
	job.LeaseToken = token
	job.LastError = ""
	*result = job
	return nil
}

func (dao *SkillOperationDao) CompleteSkillOperationJob(id uint64, leaseToken string) error {
	query := db.GetDB().Model(&model.SkillOperationJob{}).Where("id = ? AND status = ?", id, model.SkillJobStatusRunning)
	if leaseToken != "" {
		query = query.Where("lease_token = ?", leaseToken)
	}
	result := query.Updates(map[string]interface{}{"status": model.SkillJobStatusDone, "lease_until": nil, "lease_token": "", "next_retry_at": nil})
	return result.Error
}

func (dao *SkillOperationDao) RetrySkillOperationJob(id uint64, leaseToken, lastError string, nextRetry time.Time) error {
	query := db.GetDB().Model(&model.SkillOperationJob{}).Where("id = ? AND status = ?", id, model.SkillJobStatusRunning)
	if leaseToken != "" {
		query = query.Where("lease_token = ?", leaseToken)
	}
	return query.Updates(map[string]interface{}{"status": model.SkillJobStatusPending, "lease_until": nil, "lease_token": "", "next_retry_at": nextRetry, "last_error": lastError}).Error
}

func (dao *SkillOperationDao) DeleteSkillOperationJob(id uint64, leaseToken string) error {
	query := db.GetDB().Where("id = ? AND status = ?", id, model.SkillJobStatusDone)
	if leaseToken != "" {
		// A completed job clears its lease token; the token is accepted only
		// as an optional fence for callers that delete before completion.
		query = query.Where("lease_token = ? OR lease_token = ''", leaseToken)
	}
	return query.Delete(&model.SkillOperationJob{}).Error
}

func (dao *SkillOperationDao) HasPendingObjectOperation(objectKey string) (bool, error) {
	var count int64
	err := db.GetDB().Model(&model.SkillOperationJob{}).
		Where("object_key = ? AND status <> ?", objectKey, model.SkillJobStatusDone).
		Count(&count).Error
	return count > 0, err
}

func (dao *SkillOperationDao) CountStuckJobs(now time.Time) (int64, error) {
	var count int64
	err := db.GetDB().Model(&model.SkillOperationJob{}).
		Where("status = ? AND lease_until IS NOT NULL AND lease_until < ?", model.SkillJobStatusRunning, now).
		Count(&count).Error
	return count, err
}
