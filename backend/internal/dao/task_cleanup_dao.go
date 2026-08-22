package dao

import (
	"context"
	"time"

	"agenthub/backend/internal/model"
)

// TaskCleanupDao owns the lease-fenced Task cleanup outbox state machine.
type TaskCleanupDao interface {
	ClaimDueTaskCleanup(ctx context.Context, now time.Time, lease time.Duration) (*model.TaskCleanupJob, error)
	CompleteTaskCleanup(ctx context.Context, id uint64, leaseToken string) error
	RetryTaskCleanup(ctx context.Context, id uint64, leaseToken, lastError string, nextRetry time.Time) error
}
