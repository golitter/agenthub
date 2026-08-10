package dao

import (
	"time"

	"agenthub/backend/internal/model"
)

// SkillOperationDao is the durable outbox boundary for Skill storage and
// AgentEnd side effects.  Implementations must claim jobs with a lease so a
// crashed Backend can be recovered by another instance.
type SkillOperationDao interface {
	CreateSkillOperationJob(job model.SkillOperationJob) (*model.SkillOperationJob, error)
	ClaimSkillOperationJob(id uint64, now time.Time, lease time.Duration) (*model.SkillOperationJob, error)
	ClaimDueSkillOperationJob(now time.Time, lease time.Duration) (*model.SkillOperationJob, error)
	CompleteSkillOperationJob(id uint64, leaseToken string) error
	RetrySkillOperationJob(id uint64, leaseToken, lastError string, nextRetry time.Time) error
	DeleteSkillOperationJob(id uint64, leaseToken string) error
	HasPendingObjectOperation(objectKey string) (bool, error)
}
