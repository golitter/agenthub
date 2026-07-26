package model

import "time"

// ContactGroup 存储用户自定义的会话分组。
type ContactGroup struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	GroupID   string    `gorm:"uniqueIndex;size:36" json:"group_id"`
	Name      string    `gorm:"size:128;not null" json:"name"`
	SortOrder int       `gorm:"default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ContactGroupItem 将 task 关联到 group（多对多）。
type ContactGroupItem struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	GroupID   string    `gorm:"index;uniqueIndex:idx_contact_group_item_group_task;size:36;not null" json:"group_id"`
	TaskID    string    `gorm:"index;uniqueIndex:idx_contact_group_item_group_task;size:36;not null" json:"task_id"`
	SortOrder int       `gorm:"default:0" json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}
