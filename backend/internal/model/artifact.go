package model

import "time"

const (
	ArtifactKindHTML = "html"

	ArtifactStatusPending  = "pending"
	ArtifactStatusReady    = "ready"
	ArtifactStatusFailed   = "failed"
	ArtifactStatusDeleting = "deleting"
	ArtifactStatusDeleted  = "deleted"
)

// Artifact is the trusted metadata link between a message and one immutable
// object in the private artifact bucket.
type Artifact struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ResourceID     string    `gorm:"size:36;not null;uniqueIndex" json:"resource_id"`
	TaskID         string    `gorm:"size:36;not null;index" json:"task_id"`
	SessionID      string    `gorm:"size:128;not null;index" json:"session_id"`
	MessageID      string    `gorm:"size:36;not null;index;uniqueIndex:idx_artifact_message_idempotency" json:"message_id"`
	IdempotencyKey *string   `gorm:"size:128;index;uniqueIndex:idx_artifact_message_idempotency" json:"-"`
	Kind           string    `gorm:"size:32;not null" json:"kind"`
	ObjectKey      string    `gorm:"size:512;not null;uniqueIndex" json:"-"`
	Filename       string    `gorm:"size:255;not null" json:"filename"`
	ContentType    string    `gorm:"size:128;not null" json:"content_type"`
	Size           int64     `gorm:"not null" json:"size"`
	SHA256         string    `gorm:"size:64;not null" json:"sha256"`
	Status         string    `gorm:"size:16;not null;index:idx_artifact_status_updated" json:"status"`
	LastError      string    `gorm:"type:text" json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `gorm:"index:idx_artifact_status_updated" json:"updated_at"`
}
