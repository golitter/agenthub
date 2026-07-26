package model

import "time"

type Session struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	SessionID   string    `gorm:"uniqueIndex;size:128" json:"session_id"`
	TaskID      string    `gorm:"index;size:36" json:"task_id"`
	AgentType   string    `gorm:"size:64" json:"agent_type"`
	AgentName   string    `gorm:"size:128" json:"agent_name"`
	AvatarURL   string    `gorm:"size:512" json:"avatar_url,omitempty"`
	Status      string    `gorm:"size:32;default:idle" json:"status"`
	SettledDiff string    `gorm:"type:longtext" json:"settled_diff,omitempty"`
	DiffStatus  string    `gorm:"size:32" json:"diff_status,omitempty"`
	SoulMD      string    `gorm:"size:300" json:"soul_md,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
