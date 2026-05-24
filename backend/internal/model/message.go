package model

import "time"

type Message struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	TaskID    string    `gorm:"index;size:36" json:"task_id"`
	SessionID string    `gorm:"size:128" json:"session_id"`
	Role      string    `gorm:"size:16" json:"role"`
	Content   string    `gorm:"type:longtext" json:"content"`
	AgentType string    `gorm:"size:64" json:"agent_type,omitempty"`
	AgentName string    `gorm:"size:128" json:"agent_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
