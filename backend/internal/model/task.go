package model

import "time"

type Task struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	TaskID    string    `gorm:"uniqueIndex;size:36" json:"task_id"`
	SessionID string    `gorm:"index;size:36" json:"session_id"`
	AgentType string    `gorm:"size:64" json:"agent_type"`
	Status    string    `gorm:"size:32;default:pending" json:"status"`
	Message   string    `gorm:"type:text" json:"message"`
	Result    string    `gorm:"type:text" json:"result,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
