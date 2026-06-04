package model

import "time"

// SkillHub 统一仓库 (builtin + external)
type SkillHub struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	Name        string    `gorm:"uniqueIndex;size:128;not null" json:"name"`
	Builtin     bool      `gorm:"not null;default:false" json:"builtin"`
	StoragePath string    `gorm:"size:512" json:"-"` // Deprecated: 迁移后不再使用
	Description string    `gorm:"type:text" json:"description"`
	FileCount   int       `gorm:"default:0" json:"file_count"`
	TotalSize   int64     `gorm:"default:0" json:"total_size"`
	Content     []byte    `gorm:"type:longblob" json:"-"` // zip blob，external skill 专用
	UploadedBy  string    `gorm:"size:64" json:"uploaded_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AgentSkill Session ↔ Skill 关联 (仅 external skills 需要关联)
type AgentSkill struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	SessionID  string    `gorm:"size:128;not null" json:"session_id"`
	SkillName  string    `gorm:"size:128;not null" json:"skill_name"`
	AgentType  string    `gorm:"size:32;not null" json:"agent_type"`
	ImportedAt time.Time `json:"imported_at"`
}

func (AgentSkill) TableName() string {
	return "agent_skill"
}
