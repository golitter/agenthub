package model

import "time"

type Announcement struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	TaskID     string    `gorm:"index;size:36;not null" json:"task_id"`
	SenderID   string    `gorm:"size:64;not null" json:"sender_id"`
	SenderName string    `gorm:"size:64;not null" json:"sender_name"`
	Content    string    `gorm:"type:text;not null" json:"content"`
	Pinned     bool      `gorm:"default:false" json:"pinned"`
	CreatedAt  time.Time `json:"created_at"`
}
