package model

import "time"

type Session struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	SessionID string    `gorm:"uniqueIndex;size:128" json:"session_id"`
	TaskID    string    `gorm:"index;size:36" json:"task_id"`
	AgentType string    `gorm:"size:64" json:"agent_type"`
	Status    string    `gorm:"size:32;default:running" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
