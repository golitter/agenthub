package gormdao

import (
	"context"
	"errors"
	"time"

	"agenthub/backend/internal/model"
	"agenthub/backend/pkg/db"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errTaskCleanupLeaseLost = errors.New("task cleanup lease lost")

type TaskCleanupDao struct{}

func NewTaskCleanupDao() *TaskCleanupDao { return &TaskCleanupDao{} }

func (dao *TaskCleanupDao) ClaimDueTaskCleanup(ctx context.Context, now time.Time, lease time.Duration) (*model.TaskCleanupJob, error) {
	var claimed *model.TaskCleanupJob
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job model.TaskCleanupJob
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("(status = ? OR (status = ? AND lease_until IS NOT NULL AND lease_until < ?))", model.TaskCleanupStatusPending, model.TaskCleanupStatusRunning, now).
			Where("next_retry_at IS NULL OR next_retry_at <= ?", now).
			Order("created_at ASC").First(&job)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if result.Error != nil {
			return result.Error
		}
		leaseUntil := now.Add(lease)
		token := uuid.NewString()
		if err := tx.Model(&job).Updates(map[string]interface{}{
			"status": model.TaskCleanupStatusRunning, "attempts": gorm.Expr("attempts + 1"),
			"lease_until": leaseUntil, "lease_token": token, "last_error": "",
		}).Error; err != nil {
			return err
		}
		job.Status = model.TaskCleanupStatusRunning
		job.Attempts++
		job.LeaseUntil = &leaseUntil
		job.LeaseToken = token
		claimed = &job
		return nil
	})
	return claimed, err
}

func (dao *TaskCleanupDao) CompleteTaskCleanup(ctx context.Context, id uint64, leaseToken string) error {
	result := db.GetDB().WithContext(ctx).Model(&model.TaskCleanupJob{}).
		Where("id = ? AND status = ? AND lease_token = ?", id, model.TaskCleanupStatusRunning, leaseToken).
		Updates(map[string]interface{}{"status": model.TaskCleanupStatusDone, "lease_until": nil, "lease_token": "", "next_retry_at": nil, "last_error": ""})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errTaskCleanupLeaseLost
	}
	return nil
}

func (dao *TaskCleanupDao) RetryTaskCleanup(ctx context.Context, id uint64, leaseToken, lastError string, nextRetry time.Time) error {
	result := db.GetDB().WithContext(ctx).Model(&model.TaskCleanupJob{}).
		Where("id = ? AND status = ? AND lease_token = ?", id, model.TaskCleanupStatusRunning, leaseToken).
		Updates(map[string]interface{}{"status": model.TaskCleanupStatusPending, "lease_until": nil, "lease_token": "", "next_retry_at": nextRetry, "last_error": lastError})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errTaskCleanupLeaseLost
	}
	return nil
}
