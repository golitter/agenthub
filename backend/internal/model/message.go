package model

import "time"

type Message struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	MessageID string    `gorm:"uniqueIndex;size:36" json:"message_id"`
	TaskID    string    `gorm:"index;size:36" json:"task_id"`
	SessionID string    `gorm:"index:idx_session_id;index:idx_session_status,size:128" json:"session_id"`
	Role      string    `gorm:"size:16" json:"role"`
	Content   string    `gorm:"type:longtext" json:"content"`
	Status    string    `gorm:"size:16;default:completed;index:idx_session_status" json:"status"`
	LastSeq   string    `gorm:"size:64;default:''" json:"last_seq"`
	AgentType string    `gorm:"size:64" json:"agent_type,omitempty"`
	AgentName string    `gorm:"size:128" json:"agent_name,omitempty"`
	GroupID   string    `gorm:"column:group_id;size:64;index" json:"group_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
