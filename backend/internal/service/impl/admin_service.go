package impl

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agenthub/backend/internal/conf"
	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/middleware"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/agentend_client"
	"agenthub/backend/pkg/redis"
)

const (
	adminAvatarKey   = "admin_avatar_url"
	redisInfoTimeout = 3 * time.Second
)

var (
	adminStartTime     = time.Now()
	defaultAdminAvatar = "https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede"
)

type AdminService struct {
	cfg         *conf.Config
	adminDao    dao.AdminDao
	sessionDao  dao.SessionDao
	agentClient *agentend_client.Client
}

func NewAdminService(cfg *conf.Config, adminDao dao.AdminDao, sessionDao dao.SessionDao, agentClient *agentend_client.Client) *AdminService {
	return &AdminService{
		cfg:         cfg,
		adminDao:    adminDao,
		sessionDao:  sessionDao,
		agentClient: agentClient,
	}
}

func (svc *AdminService) Auth(password string) (*service.AuthResponse, error) {
	if !middleware.VerifyAdminPassword(password, svc.cfg.Admin.Password) {
		return nil, service.ErrUnauthorized("密码错误")
	}

	token, err := middleware.GenerateAdminToken(svc.cfg.JWT.Secret)
	if err != nil {
		return nil, service.ErrInternal("failed to generate token")
	}
	return &service.AuthResponse{Token: token, ExpiresIn: 3600}, nil
}

func (svc *AdminService) GetAvatar() (string, error) {
	setting, err := svc.adminDao.GetAdminSetting(adminAvatarKey)
	if err != nil {
		return "", err
	}
	if setting == nil || setting.Value == "" {
		return defaultAdminAvatar, nil
	}
	return setting.Value, nil
}

func (svc *AdminService) UpdateAvatar(url string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return service.ErrBadRequest("url is required")
	}
	if err := validateAvatarURL(url); err != nil {
		return err
	}
	return svc.adminDao.ReplaceAdminSetting(adminAvatarKey, url)
}

func (svc *AdminService) GetAgents() ([]service.AgentInfo, error) {
	configs, err := svc.agentClient.GetAgentConfigs()
	if err != nil {
		slog.Warn("get agent configs failed", "error", err)
		return nil, service.ErrServiceUnavailable("agent config service unavailable")
	}

	agents := make([]service.AgentInfo, 0, len(configs))
	for _, cfg := range configs {
		agents = append(agents, service.AgentInfo{
			Type:          cfg.Type,
			Name:          cfg.Name,
			Description:   cfg.Description,
			ConfigPath:    cfg.ConfigPath,
			ConfigContent: cfg.ConfigContent,
		})
	}
	return agents, nil
}

func (svc *AdminService) GetServices() []service.ServiceInfo {
	now := time.Now().Format("2006-01-02 15:04:05")
	return []service.ServiceInfo{
		checkHTTPService("Frontend", "http://localhost:5173", 5173, now),
		checkHTTPService("Backend", "http://localhost:"+strconv.Itoa(svc.cfg.Server.Port)+"/ping", svc.cfg.Server.Port, now),
		checkHTTPService("AgentEnd", "http://localhost:"+strconv.Itoa(svc.cfg.AgentEnd.Port)+"/health", svc.cfg.AgentEnd.Port, now),
	}
}

func (svc *AdminService) GetResources() (*service.ResourceSummary, error) {
	var disk service.ResourceInfo
	var memory service.ResourceInfo

	resp, err := svc.agentClient.GetResources()
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var result struct {
				Disk   service.ResourceInfo `json:"disk"`
				Memory service.ResourceInfo `json:"memory"`
			}
			if json.NewDecoder(resp.Body).Decode(&result) == nil {
				disk = result.Disk
				memory = result.Memory
			}
		}
	}

	return &service.ResourceSummary{
		Disk:   disk,
		Memory: memory,
		Redis:  getRedisUsage(),
	}, nil
}

func (svc *AdminService) DeleteSessions(sessionIDs []string) (int, error) {
	sessionIDs, err := normalizeAdminSessionIDs(sessionIDs)
	if err != nil {
		return 0, err
	}
	if len(sessionIDs) == 0 {
		return 0, nil
	}

	workspaces, err := svc.agentClient.ListWorkspaces()
	if err != nil {
		slog.Warn("list agentend workspaces failed before deleting sessions", "error", err)
		return 0, service.ErrServiceUnavailable("agentend workspace list unavailable")
	}

	sessionSet := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		sessionSet[sessionID] = struct{}{}
	}
	for _, workspace := range workspaces {
		if _, ok := sessionSet[workspace.SessionID]; !ok {
			continue
		}
		if workspace.ID == "" {
			continue
		}
		if err := svc.agentClient.CleanupWorkspace(workspace.ID); err != nil {
			slog.Warn("cleanup agentend workspace failed before deleting session", "session_id", workspace.SessionID, "workspace_id", workspace.ID, "error", err)
			return 0, service.ErrServiceUnavailable("cleanup workspace failed")
		}
	}

	return svc.adminDao.DeleteSessions(sessionIDs)
}

func (svc *AdminService) GetStatistics() (*service.StatisticsResponse, error) {
	now := time.Now()
	dailySessions := make([]service.DailySession, 0, 7)
	for i := 6; i >= 0; i-- {
		date := now.AddDate(0, 0, -i)
		dateStr := date.Format("2006-01-02")
		count, err := svc.adminDao.CountSessionsByDate(dateStr)
		if err != nil {
			return nil, err
		}
		dailySessions = append(dailySessions, service.DailySession{Date: dateStr, Count: int(count)})
	}

	weeklySessions := make([]service.DailySession, 0, 4)
	for i := 3; i >= 0; i-- {
		weekStart := now.AddDate(0, 0, -7*(i+1))
		weekEnd := now.AddDate(0, 0, -7*i)
		count, err := svc.adminDao.CountSessionsBetween(weekStart, weekEnd)
		if err != nil {
			return nil, err
		}
		weeklySessions = append(weeklySessions, service.DailySession{
			Date:  weekStart.Format("01-02"),
			Count: int(count),
		})
	}

	labels := make([]string, 7)
	for i := 6; i >= 0; i-- {
		labels[6-i] = now.AddDate(0, 0, -i).Format("01-02")
	}

	totalMessages, err := svc.adminDao.CountMessages()
	if err != nil {
		return nil, err
	}

	messageMap, err := svc.adminDao.CountMessagesByAgent()
	if err != nil {
		return nil, err
	}
	messagesByAgent := make([]service.MessageByAgent, 0, len(messageMap))
	for agentType, count := range messageMap {
		messagesByAgent = append(messagesByAgent, service.MessageByAgent{AgentType: agentType, Count: int(count)})
	}

	storageDays := make([]service.StorageDay, 0, 7)
	storageLabels := make([]string, 0, 7)
	for i := 6; i >= 0; i-- {
		date := now.AddDate(0, 0, -i)
		storageLabels = append(storageLabels, date.Format("01-02"))
		storageDays = append(storageDays, service.StorageDay{
			Date: date.Format("01-02"),
			Size: float64(100 + (6-i)*10),
		})
	}

	return &service.StatisticsResponse{
		DailySessions:   dailySessions,
		WeeklySessions:  weeklySessions,
		Labels:          labels,
		TotalMessages:   int(totalMessages),
		MessagesByAgent: messagesByAgent,
		StorageDays:     storageDays,
		StorageLabels:   storageLabels,
	}, nil
}

func (svc *AdminService) GetWorkspaces() (*service.WorkspaceSummary, error) {
	sessions, err := svc.sessionDao.ListAll()
	if err != nil {
		return nil, err
	}

	wsMap := make(map[string]*agentend_client.WorkspaceInfo)
	if workspaces, err := svc.agentClient.ListWorkspaces(); err == nil {
		for i := range workspaces {
			wsMap[workspaces[i].SessionID] = &workspaces[i]
		}
	}

	items := make([]service.WorkspaceItem, 0, len(sessions))
	active := 0
	cleaned := 0
	for _, sessionModel := range sessions {
		workspaceInfo := wsMap[sessionModel.SessionID]
		status := sessionModel.Status
		branch := ""
		if workspaceInfo != nil {
			branch = workspaceInfo.BranchName
			status = workspaceInfo.Status
		} else if sessionModel.Status == sessionStatusInactive {
			status = "cleaned"
		}

		items = append(items, service.WorkspaceItem{
			ID:     sessionModel.SessionID,
			Task:   sessionModel.TaskID,
			Agent:  sessionModel.AgentName,
			Branch: branch,
			DiskMB: 0,
			Status: status,
		})

		switch status {
		case "active":
			active++
		case "cleaned":
			cleaned++
		}
	}

	return &service.WorkspaceSummary{
		Workspaces: items,
		Total:      len(items),
		Active:     active,
		Cleaned:    cleaned,
		TotalDisk:  0,
	}, nil
}

func (svc *AdminService) DeleteWorkspace(sessionID string) error {
	sessionID, err := normalizeProfileSessionID(sessionID)
	if err != nil {
		return err
	}
	workspaces, err := svc.agentClient.ListWorkspaces()
	if err != nil {
		slog.Warn("list agentend workspaces failed before deleting workspace", "session_id", sessionID, "error", err)
		return service.ErrServiceUnavailable("agentend workspace list unavailable")
	}

	workspaceID := ""
	for _, workspace := range workspaces {
		if workspace.SessionID == sessionID {
			workspaceID = workspace.ID
			break
		}
	}
	if workspaceID != "" {
		if err := svc.agentClient.CleanupWorkspace(workspaceID); err != nil {
			slog.Warn("cleanup agentend workspace failed before marking workspace cleaned", "session_id", sessionID, "workspace_id", workspaceID, "error", err)
			return service.ErrServiceUnavailable("cleanup workspace failed")
		}
	}

	updated, err := svc.sessionDao.UpdateFields(sessionID, map[string]interface{}{"status": sessionStatusInactive})
	if err != nil {
		return err
	}
	if !updated {
		return service.ErrNotFound("session not found")
	}
	return nil
}

func normalizeAdminSessionIDs(sessionIDs []string) ([]string, error) {
	seen := make(map[string]struct{}, len(sessionIDs))
	normalized := make([]string, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		sessionID, err := normalizeProfileSessionID(sessionID)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		normalized = append(normalized, sessionID)
	}
	return normalized, nil
}

func checkHTTPService(name, url string, port int, lastCheck string) service.ServiceInfo {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	uptime := time.Since(adminStartTime).Round(time.Second).String()

	status := "Down"
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			status = "Running"
		}
	}

	return service.ServiceInfo{
		Name:      name,
		Status:    status,
		Uptime:    uptime,
		Version:   "1.0.0",
		Port:      port,
		LastCheck: lastCheck,
	}
}

func getRedisUsage() service.ResourceInfo {
	client := redis.GetClient()
	if client == nil {
		return service.ResourceInfo{Used: 0, Total: 0, Unit: "MB"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), redisInfoTimeout)
	defer cancel()

	info, err := client.Info(ctx, "memory").Result()
	if err != nil {
		return service.ResourceInfo{Used: 0, Total: 0, Unit: "MB"}
	}

	usedMB := parseRedisInfoFloat(info, "used_memory") / 1e6
	maxMemoryStr := parseRedisInfoString(info, "maxmemory")
	var totalMB float64
	if maxMemoryStr != "" && maxMemoryStr != "0" {
		if val, err := strconv.ParseFloat(maxMemoryStr, 64); err == nil {
			totalMB = val / 1e6
		}
	}
	if totalMB == 0 {
		totalMB = 512
	}
	return service.ResourceInfo{Used: usedMB, Total: totalMB, Unit: "MB"}
}

func parseRedisInfoFloat(info, key string) float64 {
	target := key + ":"
	for i := 0; i < len(info); i++ {
		if i+len(target) <= len(info) && info[i:i+len(target)] == target {
			j := i + len(target)
			for j < len(info) && info[j] != '\r' && info[j] != '\n' {
				j++
			}
			val, _ := strconv.ParseFloat(info[i+len(target):j], 64)
			return val
		}
	}
	return 0
}

func parseRedisInfoString(info, key string) string {
	target := key + ":"
	for i := 0; i < len(info); i++ {
		if i+len(target) <= len(info) && info[i:i+len(target)] == target {
			j := i + len(target)
			for j < len(info) && info[j] != '\r' && info[j] != '\n' {
				j++
			}
			return info[i+len(target) : j]
		}
	}
	return ""
}
