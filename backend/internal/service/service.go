package service

import (
	"context"
	"io"
	"net/http"
	"time"

	"agenthub/backend/internal/model"
)

type SessionStatus struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

type SaveDiffSnapshotInput struct {
	SessionID string
	Diff      string
	Status    string
}

type AgentConfig struct {
	Type string `json:"type" binding:"required"`
	Name string `json:"name"`
}

type CreateTaskInput struct {
	Title    string        `json:"title" binding:"required"`
	RepoPath string        `json:"repo_path"`
	Agents   []AgentConfig `json:"agents"`
}

type PatchTaskInput struct {
	PinnedAt *string `json:"pinned_at"`
}

type RunTaskInput struct {
	Message         string `json:"message" binding:"required"`
	AgentType       string `json:"agent_type"`
	SessionID       string `json:"session_id" binding:"required"`
	Cwd             string `json:"cwd"`
	SkipUserMessage bool   `json:"skip_user_message"`
}

type ReviewTaskInput struct {
	SessionID string `json:"session_id" binding:"required"`
	Action    string `json:"action" binding:"required"`
	Content   string `json:"content"`
}

type MessageRoute struct {
	Mode           string `json:"mode"`
	SessionID      string `json:"session_id"`
	AgentType      string `json:"agent_type"`
	AgentName      string `json:"agent_name"`
	RouteID        string `json:"route_id"`
	AgentMessage   string `json:"agent_message"`
	DisplayMessage string `json:"display_message"`
}

type TaskSessionWithAgent struct {
	model.Session
	AgentType    string   `json:"agent_type"`
	AgentName    string   `json:"agent_name"`
	AvatarURL    string   `json:"avatar_url,omitempty"`
	RouteID      string   `json:"route_id"`
	MentionLabel string   `json:"mention_label"`
	Aliases      []string `json:"aliases,omitempty"`
}

type TaskDetailResponse struct {
	Task     model.Task             `json:"task"`
	Sessions []TaskSessionWithAgent `json:"sessions"`
}

type RunTaskResult struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
	AgentType string `json:"agent_type"`
	AgentName string `json:"agent_name"`
	RouteID   string `json:"route_id"`
	RouteMode string `json:"route_mode"`
}

type CreateAnnouncementInput struct {
	SenderID   string
	SenderName string
	Content    string
	Pinned     bool
}

type GroupItem struct {
	TaskID    string `json:"task_id"`
	SortOrder int    `json:"sort_order"`
}

type GroupWithItems struct {
	GroupID   string      `json:"group_id"`
	Name      string      `json:"name"`
	SortOrder int         `json:"sort_order"`
	Items     []GroupItem `json:"items"`
}

type ListGroupsResponse struct {
	Groups           []GroupWithItems `json:"groups"`
	UngroupedTaskIDs []string         `json:"ungrouped_task_ids"`
}

type ListMessagesResponse struct {
	Data    []model.Message `json:"data"`
	HasMore bool            `json:"has_more"`
}

type SkillHubItem struct {
	Name        string `json:"name"`
	Builtin     bool   `json:"builtin"`
	Description string `json:"description"`
	FileCount   int    `json:"file_count"`
	TotalSize   int64  `json:"total_size"`
	ImportCount int64  `json:"import_count"`
	CreatedAt   string `json:"created_at"`
}

type SkillImportResult struct {
	Success bool   `json:"success"`
	Skill   string `json:"skill,omitempty"`
	Session string `json:"session,omitempty"`
	Name    string `json:"name,omitempty"`
}

type BuiltinSkillItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
	Source      string `json:"source"`
}

type AgentSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
	Source      string `json:"source"`
}

type AgentProfileResponse struct {
	AgentName string       `json:"agent_name"`
	AgentType string       `json:"agent_type"`
	AvatarURL string       `json:"avatar_url,omitempty"`
	Status    string       `json:"status"`
	SessionID string       `json:"session_id"`
	SoulMD    string       `json:"soul_md,omitempty"`
	Skills    []AgentSkill `json:"skills"`
}

type AgentDetailResponse struct {
	AgentName     string       `json:"agent_name"`
	AgentType     string       `json:"agent_type"`
	AvatarURL     string       `json:"avatar_url,omitempty"`
	Status        string       `json:"status"`
	SessionID     string       `json:"session_id"`
	TaskID        string       `json:"task_id"`
	RepoPath      string       `json:"repo_path,omitempty"`
	WorkspacePath string       `json:"workspace_path,omitempty"`
	SoulMD        string       `json:"soul_md,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	MessageCount  int64        `json:"message_count"`
	Skills        []AgentSkill `json:"skills"`
}

type AuthResponse struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

type AgentInfo struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ConfigPath    string `json:"configPath"`
	ConfigContent string `json:"configContent,omitempty"`
}

type ServiceInfo struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Uptime    string `json:"uptime"`
	Version   string `json:"version"`
	Port      int    `json:"port"`
	LastCheck string `json:"lastCheck"`
}

type ResourceInfo struct {
	Used  float64 `json:"used"`
	Total float64 `json:"total"`
	Unit  string  `json:"unit"`
}

type ResourceSummary struct {
	Disk   ResourceInfo `json:"disk"`
	Memory ResourceInfo `json:"memory"`
	Redis  ResourceInfo `json:"redis"`
}

type DailySession struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type MessageByAgent struct {
	AgentType string `json:"agentType"`
	Count     int    `json:"count"`
}

type StorageDay struct {
	Date string  `json:"date"`
	Size float64 `json:"size"`
}

type StatisticsResponse struct {
	DailySessions   []DailySession   `json:"dailySessions"`
	WeeklySessions  []DailySession   `json:"weeklySessions"`
	Labels          []string         `json:"labels"`
	TotalMessages   int              `json:"totalMessages"`
	MessagesByAgent []MessageByAgent `json:"messagesByAgent"`
	StorageDays     []StorageDay     `json:"storageDays"`
	StorageLabels   []string         `json:"storageLabels"`
}

type WorkspaceItem struct {
	ID     string  `json:"id"`
	Task   string  `json:"task"`
	Agent  string  `json:"agent"`
	Branch string  `json:"branch"`
	DiskMB float64 `json:"disk_mb"`
	Status string  `json:"status"`
}

type WorkspaceSummary struct {
	Workspaces []WorkspaceItem `json:"workspaces"`
	Total      int             `json:"total"`
	Active     int             `json:"active"`
	Cleaned    int             `json:"cleaned"`
	TotalDisk  int             `json:"totalDisk"`
}

type SessionService interface {
	PatchSessionStatus(sessionID, status string) (*SessionStatus, error)
}

type DiffSnapshotService interface {
	GetDiffSnapshot(snapshotID string) (*model.DiffSnapshot, error)
	SaveDiffSnapshot(snapshotID string, input SaveDiffSnapshotInput) (*model.DiffSnapshot, error)
}

type AnnouncementService interface {
	ListAnnouncements(taskID string, pinnedOnly bool) ([]model.Announcement, error)
	CreateAnnouncement(taskID string, input CreateAnnouncementInput) (*model.Announcement, error)
	DeleteAnnouncement(id string) error
}

type ContactGroupService interface {
	ListGroups() (*ListGroupsResponse, error)
	CreateGroup(name string) (*model.ContactGroup, error)
	UpdateGroup(groupID, name string) error
	DeleteGroup(groupID string) error
	AddItem(groupID, taskID string) (*model.ContactGroupItem, error)
	RemoveItem(groupID, taskID string) error
}

type MessageService interface {
	ListMessages(taskID, sessionID, mode, primarySessionID string, limit int, beforeID *uint64, paginated bool) (*ListMessagesResponse, error)
	WindowMessages(taskID, sessionID string) ([]map[string]interface{}, error)
}

type AvatarService interface {
	UploadAvatar(filename string, size int64, data []byte) (string, error)
	UpdateSession(sessionID, agentName, avatarURL string) error
}

type SkillService interface {
	UploadSkill(filename string, zipData []byte) (*ValidationResult, error)
	ConfirmSkill(name, description string, fileCount int, totalSize int64, tmpDir string) (*SkillImportResult, error)
	ListSkills() ([]SkillHubItem, error)
	DeleteSkill(name string) error
	ImportSkill(skillName, sessionID string) (*SkillImportResult, error)
	RemoveSkill(skillName, sessionID string) (*SkillImportResult, error)
	ReportBuiltinSkills(skills []BuiltinSkillItem) error
}

type AgentProfileService interface {
	GetProfile(sessionID string) (*AgentProfileResponse, error)
	GetDetail(sessionID string) (*AgentDetailResponse, error)
	GetSoul(sessionID string) (string, error)
	UpdateSoul(sessionID, soulMD string) error
}

type TaskService interface {
	CreateTask(input CreateTaskInput) (*model.Task, error)
	ListTasks() ([]model.Task, error)
	GetTask(taskID string) (*TaskDetailResponse, error)
	DeleteTask(taskID string) error
	LeaveTask(taskID string) error
	PatchTask(taskID string, input PatchTaskInput) error
	RunTask(taskID string, input RunTaskInput) (*RunTaskResult, error)
	ReviewTask(taskID string, input ReviewTaskInput) (map[string]interface{}, error)
	FetchGroupChatWindow(taskID, sessionID string) []map[string]interface{}
}

type StreamService interface {
	ServeStream(ctx context.Context, sessionID, messageID string, writer io.Writer, flusher http.Flusher) error
}

type AdminService interface {
	Auth(password string) (*AuthResponse, error)
	GetAvatar() (string, error)
	UpdateAvatar(url string) error
	GetAgents() ([]AgentInfo, error)
	GetServices() []ServiceInfo
	GetResources() (*ResourceSummary, error)
	DeleteSessions(sessionIDs []string) (int, error)
	GetStatistics() (*StatisticsResponse, error)
	GetWorkspaces() (*WorkspaceSummary, error)
	DeleteWorkspace(id string) error
}
