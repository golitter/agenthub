package model

import "time"

type Session struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	SessionID string    `gorm:"uniqueIndex;size:36" json:"session_id"`
	Title     string    `gorm:"size:255" json:"title"`
	Status    string    `gorm:"size:32;default:active" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
