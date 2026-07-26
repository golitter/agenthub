package impl

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/storage"

	"github.com/google/uuid"
)

type AvatarService struct {
	sessionDao dao.SessionDao
	uploader   storage.Provider
}

const maxAvatarURLLen = 512

func NewAvatarService(sessionDao dao.SessionDao, uploader storage.Provider) *AvatarService {
	return &AvatarService{sessionDao: sessionDao, uploader: uploader}
}

func (svc *AvatarService) UploadAvatar(filename string, size int64, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	key := "avatars/" + uuid.New().String() + ext
	avatarURL, err := svc.uploader.UploadBytes(context.Background(), key, data)
	if err != nil {
		return "", service.ErrInternal("failed to upload file")
	}
	return avatarURL, nil
}

func (svc *AvatarService) UpdateSession(sessionID, agentName, avatarURL string) error {
	sessionID = strings.TrimSpace(sessionID)
	agentName = strings.TrimSpace(agentName)
	avatarURL = strings.TrimSpace(avatarURL)
	if sessionID == "" {
		return service.ErrBadRequest("session_id is required")
	}
	if len([]rune(sessionID)) > maxSessionIDLen {
		return service.ErrBadRequest("session_id is too long")
	}
	if len([]rune(agentName)) > maxAgentNameLen {
		return service.ErrBadRequest("agent_name is too long")
	}
	if err := validateAvatarURL(avatarURL); err != nil {
		return err
	}

	updates := map[string]interface{}{}
	if agentName != "" {
		updates["agent_name"] = agentName
	}
	if avatarURL != "" {
		updates["avatar_url"] = avatarURL
	}
	if len(updates) == 0 {
		return service.ErrBadRequest("at least one field (agent_name or avatar_url) is required")
	}

	updated, err := svc.sessionDao.UpdateFields(sessionID, updates)
	if err != nil {
		return err
	}
	if !updated {
		return service.ErrNotFound("session not found")
	}
	return nil
}

func validateAvatarURL(avatarURL string) error {
	if avatarURL == "" {
		return nil
	}
	if len(avatarURL) > maxAvatarURLLen {
		return service.ErrBadRequest("avatar_url is too long")
	}
	if strings.ContainsAny(avatarURL, " \t\r\n") || strings.ContainsFunc(avatarURL, unicode.IsControl) {
		return service.ErrBadRequest("avatar_url is invalid")
	}
	if strings.HasPrefix(avatarURL, "/") {
		if strings.HasPrefix(avatarURL, "//") || strings.Contains(avatarURL, "\\") {
			return service.ErrBadRequest("avatar_url is invalid")
		}
		return nil
	}

	parsed, err := url.Parse(avatarURL)
	if err != nil || parsed.Host == "" {
		return service.ErrBadRequest("avatar_url is invalid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return service.ErrBadRequest("avatar_url must use http or https")
	}
	return nil
}
