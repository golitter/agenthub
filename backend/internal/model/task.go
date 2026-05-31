package model

import "time"

type Task struct {
	ID        uint       `gorm:"primarykey" json:"id"`
	TaskID    string     `gorm:"uniqueIndex;size:36" json:"task_id"`
	Title     string     `gorm:"size:255" json:"title"`
	RepoPath  string     `gorm:"size:512" json:"repo_path"`
	Status    string     `gorm:"size:32;default:active" json:"status"`
	PinnedAt  *time.Time `gorm:"" json:"pinned_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
