package dao

import (
	"errors"
	"time"

	"agenthub/backend/internal/model"
)

var ErrDuplicate = errors.New("duplicate record")

type SessionDao interface {
	DeactivateSession(sessionID string) (bool, error)
	GetBySessionID(sessionID string) (*model.Session, error)
	GetByTaskAndSessionID(taskID, sessionID string) (*model.Session, error)
	ListByTaskID(taskID string) ([]model.Session, error)
	ListAll() ([]model.Session, error)
	FindPrimaryGroupSessionID(taskID string) (string, error)
	UpdateFields(sessionID string, updates map[string]interface{}) (bool, error)
	UpdateSoul(sessionID, soulMD string) (bool, error)
	UpdateStatusByTask(sessionID, taskID, status string) error
}

type DiffSnapshotDao interface {
	GetBySnapshotID(snapshotID string) (*model.DiffSnapshot, error)
	CancelPendingBySession(sessionID, excludedSnapshotID string) error
	Upsert(snapshot model.DiffSnapshot) (*model.DiffSnapshot, error)
	UpsertPending(snapshotID, sessionID, diff string) error
}

type TaskDao interface {
	GetByTaskID(taskID string) (*model.Task, error)
	FindRepoPathByTaskID(taskID string) (string, error)
	CreateTaskWithSessions(task *model.Task, sessions []model.Session, sessionAgents []model.SessionAgent) error
	ListTasks(limit int, beforeTaskID string) ([]model.Task, error)
	ListSessionAgentsBySessionIDs(sessionIDs []string) ([]model.SessionAgent, error)
	DeleteTaskCascade(taskID string) (bool, error)
	GetTaskAndSessionIDs(taskID string) (*model.Task, []string, error)
	PatchTask(taskID string, updates map[string]interface{}) (bool, error)
}

type AnnouncementDao interface {
	ListByTaskID(taskID string, pinnedOnly bool) ([]model.Announcement, error)
	CreateAnnouncement(announcement model.Announcement) (*model.Announcement, error)
	GetAnnouncementByID(id uint) (*model.Announcement, error)
	DeleteAnnouncement(id uint) (*model.Announcement, error)
}

type ContactGroupDao interface {
	ListGroups() ([]model.ContactGroup, error)
	ListItemsByGroupID(groupID string) ([]model.ContactGroupItem, error)
	ListActiveTaskIDs() ([]string, error)
	GroupExists(groupID string) (bool, error)
	ActiveTaskExists(taskID string) (bool, error)
	ItemExists(groupID, taskID string) (bool, error)
	CreateGroup(group model.ContactGroup) (*model.ContactGroup, error)
	UpdateGroupName(groupID, name string) (bool, error)
	DeleteGroupWithItems(groupID string) (bool, error)
	CreateItem(item model.ContactGroupItem) (*model.ContactGroupItem, error)
	DeleteItem(groupID, taskID string) (bool, error)
}

type MessageDao interface {
	ListByTask(taskID, sessionID, mode, primarySessionID string, limit int, beforeID *uint64) ([]model.Message, error)
	CountBySessionID(sessionID string) (int64, error)
	FindByMessageID(messageID string) (*model.Message, error)
	CreateMessage(message model.Message) error
	FindSessionIDByTaskMessage(taskID, messageID string) (string, error)
	FindMessageContent(messageID string) (string, error)
	UpdateMessageContentAndSeq(messageID, content, seq string) error
	UpdateMessageStatus(messageID, status string) error
	FailStaleStreamingMessages() (int64, error)
	FindLatestCompletedAgentMessage(taskID, sessionID string) (*model.Message, error)
	ListGroupChatWindowMessages(taskID, sessionID string, afterCreatedAt *model.Message) ([]model.Message, error)
	FindLatestPlanReviewMessage(taskID, sessionID string) (*model.Message, error)
	UpdateContent(messageID, content string) error
}

type SkillDao interface {
	CountBuiltinByName(name string) (int64, error)
	CountByName(name string) (int64, error)
	CreateSkill(skill model.SkillHub) error
	ListSkills() ([]model.SkillHub, error)
	CountImportsBySkillName(name string) (int64, error)
	GetSkillByName(name string) (*model.SkillHub, error)
	GetSkillByID(id uint) (*model.SkillHub, error)
	GetSkillContent(name string) ([]byte, error)
	GetSkillUploadReceipt(uploadID string) (*model.SkillUploadReceipt, error)
	CreateSkillUploadReceipt(receipt model.SkillUploadReceipt) error
	DeleteSkillCascade(name string) error
	HasAgentSkill(sessionID, skillName string) (bool, error)
	CreateAgentSkill(skill model.AgentSkill) error
	UpdateAgentSkillStatus(sessionID, skillName, status string) error
	DeleteAgentSkill(sessionID, skillName string) error
	UpsertSkillHub(name, description string, builtin bool) error
	EnsureAgentSkill(sessionID, skillName, agentType string) error
	ListBuiltinSkills() ([]model.SkillHub, error)
	ListExternalSkillsBySession(sessionID string) ([]model.SkillHub, error)
}

type AdminDao interface {
	GetAdminSetting(key string) (*model.AdminSetting, error)
	ReplaceAdminSetting(key, value string) error
	DeleteSessions(sessionIDs []string) (int, error)
	CountSessionsByDate(date string) (int64, error)
	CountSessionsBetween(start, end time.Time) (int64, error)
	CountMessages() (int64, error)
	CountMessagesByAgent() (map[string]int64, error)
}
