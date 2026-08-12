package dao

import (
	"errors"
	"time"

	"agenthub/backend/internal/model"
)

var ErrArtifactQuota = errors.New("artifact quota exceeded")

type ArtifactDao interface {
	CreatePending(artifact *model.Artifact, maxObjects int64) error
	MarkReady(resourceID string, size int64, sha256 string) error
	MarkFailed(resourceID, message string) error
	FindReadyByResourceID(resourceID string) (*model.Artifact, error)
	FindByResourceID(resourceID string) (*model.Artifact, error)
	FindByMessageAndIdempotency(messageID, key string) (*model.Artifact, error)
	CountByMessageID(messageID string) (int64, error)
	ListObjectKeysByTaskID(taskID string) ([]model.Artifact, error)
	MarkDeletingByTaskID(taskID string) ([]model.Artifact, error)
	MarkDeleteFailed(resourceID, message string) error
	DeleteRow(resourceID string) error
	ListStalePendingOrFailed(before time.Time, limit int) ([]model.Artifact, error)
}
