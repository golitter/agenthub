package model

import "time"

const (
	SkillStorageDB    = "db"
	SkillStorageMinIO = "minio"

	SkillStatusReady        = "ready"
	SkillStatusMigrating    = "migrating"
	SkillStatusDeleting     = "deleting"
	SkillStatusStorageError = "storage_error"

	AgentSkillStatusInstalling = "installing"
	AgentSkillStatusReady      = "ready"
	AgentSkillStatusRemoving   = "removing"
	AgentSkillStatusSyncError  = "sync_error"

	SkillJobStatusPending = "pending"
	SkillJobStatusRunning = "running"
	SkillJobStatusDone    = "done"
	SkillJobStatusFailed  = "failed"

	SkillOperationDeleteObject = "delete_object"
	SkillOperationInstall      = "install"
	SkillOperationRemove       = "remove"
	SkillOperationMigrate      = "migrate"
	SkillOperationVerifyObject = "verify_object"
)

// SkillHub 统一仓库 (builtin + external)
type SkillHub struct {
	ID                 uint      `gorm:"primarykey" json:"id"`
	Name               string    `gorm:"uniqueIndex;size:128;not null" json:"name"`
	Builtin            bool      `gorm:"not null;default:false" json:"builtin"`
	Description        string    `gorm:"type:text" json:"description"`
	FileCount          int       `gorm:"default:0" json:"file_count"`
	TotalSize          int64     `gorm:"default:0" json:"total_size"`
	Content            []byte    `gorm:"type:longblob" json:"-"` // 迁移期兼容字段，external skill 专用
	ObjectKey          string    `gorm:"size:512" json:"-"`
	SHA256             string    `gorm:"size:64" json:"sha256,omitempty"`
	PackageSize        int64     `gorm:"default:0" json:"package_size"`
	StorageType        string    `gorm:"size:16" json:"storage_type"`
	Status             string    `gorm:"size:32;not null;default:ready" json:"status"`
	UploadedBy         string    `gorm:"size:64" json:"uploaded_by,omitempty"`
	FilesJSON          string    `gorm:"type:text" json:"-"`
	ContainsExecutable bool      `gorm:"not null;default:false" json:"contains_executable"`
	ContainsBinary     bool      `gorm:"not null;default:false" json:"contains_binary"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// AgentSkill Session ↔ Skill 关联 (仅 external skills 需要关联)
type AgentSkill struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	SessionID  string    `gorm:"uniqueIndex:idx_agent_skill_session_skill;size:128;not null" json:"session_id"`
	SkillName  string    `gorm:"uniqueIndex:idx_agent_skill_session_skill;size:128;not null" json:"skill_name"`
	AgentType  string    `gorm:"size:32;not null" json:"agent_type"`
	Status     string    `gorm:"size:16;not null;default:ready" json:"status"`
	ImportedAt time.Time `json:"imported_at"`
}

func (AgentSkill) TableName() string {
	return "agent_skill"
}

// SkillUploadReceipt 持久化上传确认结果，Redis 会话丢失后仍可按 upload_id 幂等返回。
type SkillUploadReceipt struct {
	// UUID upload IDs are currently 36 characters; keep headroom for a future
	// ULID or other opaque identifier instead of truncating at the SQL layer.
	UploadID  string    `gorm:"primaryKey;size:64" json:"upload_id"`
	SkillID   uint      `gorm:"not null;index" json:"skill_id"`
	SHA256    string    `gorm:"size:64;not null" json:"sha256"`
	OwnerID   string    `gorm:"size:128" json:"owner_id,omitempty"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// SkillOperationJob 是对象存储和 AgentEnd 补偿操作的持久化 Outbox。
type SkillOperationJob struct {
	ID             uint64 `gorm:"primaryKey" json:"id"`
	Operation      string `gorm:"size:32;not null;index:idx_skill_jobs_due,priority:1" json:"operation"`
	IdempotencyKey string `gorm:"size:512;not null;uniqueIndex" json:"idempotency_key"`
	SkillID        *uint  `gorm:"index" json:"skill_id,omitempty"`
	// AgentSkillID fences install/remove retries against a later relation with
	// the same (session_id, skill_name) pair.
	AgentSkillID *uint      `gorm:"index" json:"-"`
	SkillName    string     `gorm:"size:128" json:"skill_name,omitempty"`
	SessionID    string     `gorm:"size:128" json:"session_id,omitempty"`
	AgentType    string     `gorm:"size:32" json:"agent_type,omitempty"`
	ObjectKey    string     `gorm:"size:512" json:"-"`
	Status       string     `gorm:"size:16;not null;default:pending;index:idx_skill_jobs_due,priority:2" json:"status"`
	Attempts     int        `gorm:"not null;default:0" json:"attempts"`
	NextRetryAt  *time.Time `gorm:"index:idx_skill_jobs_due,priority:3" json:"next_retry_at,omitempty"`
	LeaseUntil   *time.Time `json:"lease_until,omitempty"`
	LeaseToken   string     `gorm:"size:64" json:"-"`
	LastError    string     `gorm:"type:text" json:"last_error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// SkillAuditEvent records the actor, integrity hash and content indicators for
// Skill lifecycle actions.  It is append-only from the service perspective.
type SkillAuditEvent struct {
	ID                 uint64    `gorm:"primaryKey" json:"id"`
	Action             string    `gorm:"size:32;not null;index" json:"action"`
	Outcome            string    `gorm:"size:16;not null;index" json:"outcome"`
	UploadID           string    `gorm:"size:64;index" json:"upload_id,omitempty"`
	SkillID            *uint     `gorm:"index" json:"skill_id,omitempty"`
	SkillName          string    `gorm:"size:128;index" json:"skill_name"`
	OwnerID            string    `gorm:"size:128;index" json:"owner_id,omitempty"`
	ObjectKey          string    `gorm:"size:512" json:"-"`
	SHA256             string    `gorm:"size:64" json:"sha256,omitempty"`
	FilesJSON          string    `gorm:"type:text" json:"-"`
	ContainsExecutable bool      `gorm:"not null;default:false" json:"contains_executable"`
	ContainsBinary     bool      `gorm:"not null;default:false" json:"contains_binary"`
	Error              string    `gorm:"type:text" json:"error,omitempty"`
	CreatedAt          time.Time `gorm:"index" json:"created_at"`
}
