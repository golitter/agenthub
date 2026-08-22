package model

import "time"

const (
	TaskCleanupStatusPending = "pending"
	TaskCleanupStatusRunning = "running"
	TaskCleanupStatusDone    = "done"
)

// TaskCleanupJob is the durable cleanup intent written atomically with Task
// deletion. SessionIDsJSON is a snapshot because the session rows are removed
// by the same transaction.
type TaskCleanupJob struct {
	ID             uint64     `gorm:"primaryKey" json:"id"`
	TaskID         string     `gorm:"size:36;not null;uniqueIndex" json:"task_id"`
	Action         string     `gorm:"size:16;not null" json:"action"`
	RepoPath       string     `gorm:"size:512" json:"repo_path,omitempty"`
	SessionIDsJSON string     `gorm:"type:text;not null" json:"-"`
	Status         string     `gorm:"size:16;not null;default:pending;index:idx_task_cleanup_due,priority:1" json:"status"`
	Attempts       int        `gorm:"not null;default:0" json:"attempts"`
	NextRetryAt    *time.Time `gorm:"index:idx_task_cleanup_due,priority:2" json:"next_retry_at,omitempty"`
	LeaseUntil     *time.Time `json:"lease_until,omitempty"`
	LeaseToken     string     `gorm:"size:64" json:"-"`
	LastError      string     `gorm:"type:text" json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
