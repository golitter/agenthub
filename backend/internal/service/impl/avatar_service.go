package impl

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"unicode"

	"agenthub/backend/internal/dao"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/storage"

	"github.com/google/uuid"
)

const (
	maxAvatarURLLen                = 512
	maxAvatarBytes                 = 2 << 20
	maxAvatarDimension             = 4096
	maxAvatarPixels          int64 = 16 << 20
	defaultLocalAvatarPrefix       = "/uploads"
)

type AvatarService struct {
	sessionDao           dao.SessionDao
	uploader             storage.Provider
	localAvatarURLPrefix string
}

func NewAvatarService(sessionDao dao.SessionDao, uploader storage.Provider, localURLPrefix ...string) *AvatarService {
	prefix := defaultLocalAvatarPrefix
	if len(localURLPrefix) > 0 {
		if normalized := normalizeLocalAvatarURLPrefix(localURLPrefix[0]); normalized != "" {
			prefix = normalized
		}
	}
	return &AvatarService{sessionDao: sessionDao, uploader: uploader, localAvatarURLPrefix: prefix}
}

func (svc *AvatarService) UploadAvatar(ctx context.Context, filename string, data []byte) (string, error) {
	if len(data) > maxAvatarBytes {
		return "", service.ErrBadRequest("file size exceeds 2MB limit")
	}
	ext, err := canonicalAvatarExtension(filename, data)
	if err != nil {
		return "", service.ErrBadRequest(err.Error())
	}
	if svc.uploader == nil {
		return "", service.ErrInternal("avatar storage is not configured")
	}

	// UUID collisions are extraordinarily unlikely, but an immutable object
	// store must still reject rather than overwrite. Regenerate the key when a
	// provider reports the collision.
	for attempt := 0; attempt < 3; attempt++ {
		key := "avatars/" + uuid.New().String() + ext
		avatarURL, uploadErr := svc.uploader.UploadBytes(ctx, key, data)
		if uploadErr == nil {
			return avatarURL, nil
		}
		if errors.Is(uploadErr, storage.ErrObjectExists) {
			continue
		}
		return "", service.ErrInternal("failed to upload file")
	}
	return "", service.ErrInternal("failed to allocate avatar object key")
}

func canonicalAvatarExtension(filename string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("avatar file is empty")
	}
	extension := strings.ToLower(filepath.Ext(filename))
	if extension != ".jpg" && extension != ".jpeg" && extension != ".png" && extension != ".gif" && extension != ".webp" {
		return "", fmt.Errorf("unsupported file format, allowed: jpg/png/gif/webp")
	}

	detected := strings.ToLower(strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0]))
	expected := map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
	}[extension]
	if detected != expected {
		return "", fmt.Errorf("file content does not match filename extension")
	}

	var width, height int
	if detected == "image/webp" {
		var err error
		width, height, err = decodeWebPDimensions(data)
		if err != nil {
			return "", fmt.Errorf("invalid image: %w", err)
		}
	} else {
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return "", fmt.Errorf("invalid image: %w", err)
		}
		width, height = config.Width, config.Height
	}
	if width <= 0 || height <= 0 || width > maxAvatarDimension || height > maxAvatarDimension {
		return "", fmt.Errorf("image dimensions exceed 4096px limit")
	}
	if int64(width)*int64(height) > maxAvatarPixels {
		return "", fmt.Errorf("image pixel count exceeds 16MP limit")
	}

	if extension == ".jpeg" {
		return ".jpg", nil
	}
	return extension, nil
}

// decodeWebPDimensions validates the RIFF/WebP container and extracts the
// canvas size without decoding pixels. Go's standard image package does not
// ship a WebP decoder, while the dimensions are sufficient for the avatar
// resource limits enforced here.
func decodeWebPDimensions(data []byte) (int, int, error) {
	if len(data) < 20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 0, 0, fmt.Errorf("invalid webp container")
	}
	riffSize := int64(binary.LittleEndian.Uint32(data[4:8]))
	containerEnd := int64(8) + riffSize
	if riffSize < 4 || containerEnd > int64(len(data)) || containerEnd < 20 {
		return 0, 0, fmt.Errorf("truncated webp container")
	}
	var canvasWidth, canvasHeight int
	var imageWidth, imageHeight int
	offset := int64(12)
	for offset < containerEnd {
		if offset+8 > containerEnd {
			return 0, 0, fmt.Errorf("truncated webp chunk header")
		}
		chunkType := string(data[offset : offset+4])
		chunkSize := int64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		payloadStart := offset + 8
		payloadEnd := payloadStart + chunkSize
		if chunkSize < 0 || payloadEnd > containerEnd || payloadEnd > int64(len(data)) {
			return 0, 0, fmt.Errorf("truncated webp chunk")
		}
		payload := data[payloadStart:payloadEnd]
		switch chunkType {
		case "VP8X":
			if len(payload) < 10 {
				return 0, 0, fmt.Errorf("invalid VP8X chunk")
			}
			canvasWidth = 1 + (int(payload[4]) | int(payload[5])<<8 | int(payload[6])<<16)
			canvasHeight = 1 + (int(payload[7]) | int(payload[8])<<8 | int(payload[9])<<16)
		case "VP8 ":
			if len(payload) < 10 || payload[3] != 0x9d || payload[4] != 0x01 || payload[5] != 0x2a {
				return 0, 0, fmt.Errorf("invalid VP8 chunk")
			}
			imageWidth = int(binary.LittleEndian.Uint16(payload[6:8]) & 0x3fff)
			imageHeight = int(binary.LittleEndian.Uint16(payload[8:10]) & 0x3fff)
		case "VP8L":
			if len(payload) < 5 || payload[0] != 0x2f {
				return 0, 0, fmt.Errorf("invalid VP8L chunk")
			}
			bits := uint32(payload[1]) | uint32(payload[2])<<8 | uint32(payload[3])<<16 | uint32(payload[4])<<24
			imageWidth = int((bits & 0x3fff) + 1)
			imageHeight = int(((bits >> 14) & 0x3fff) + 1)
		}
		offset = payloadEnd
		if chunkSize%2 == 1 {
			if offset >= containerEnd || offset >= int64(len(data)) {
				return 0, 0, fmt.Errorf("truncated webp chunk padding")
			}
			offset++
		}
	}
	if imageWidth <= 0 || imageHeight <= 0 {
		return 0, 0, fmt.Errorf("webp image chunk is missing")
	}
	if canvasWidth > 0 && canvasHeight > 0 {
		return canvasWidth, canvasHeight, nil
	}
	return imageWidth, imageHeight, nil
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
	if err := validateAvatarURL(avatarURL, svc.localAvatarURLPrefix); err != nil {
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

func validateAvatarURL(avatarURL string, localPrefixes ...string) error {
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
		if strings.HasPrefix(avatarURL, "//") || strings.Contains(avatarURL, "\\") || strings.Contains(avatarURL, "%") {
			return service.ErrBadRequest("avatar_url is invalid")
		}
		if strings.HasPrefix(avatarURL, "/api/assets/avatars/") {
			if !isCanonicalAvatarFileName(strings.TrimPrefix(avatarURL, "/api/assets/avatars/")) {
				return service.ErrBadRequest("avatar_url is invalid")
			}
			return nil
		}
		prefixes := append([]string{defaultLocalAvatarPrefix}, localPrefixes...)
		for _, prefix := range prefixes {
			prefix = normalizeLocalAvatarURLPrefix(prefix)
			if prefix == "" {
				continue
			}
			avatarPrefix := prefix + "/avatars/"
			if strings.HasPrefix(avatarURL, avatarPrefix) {
				if !isCanonicalAvatarFileName(strings.TrimPrefix(avatarURL, avatarPrefix)) {
					return service.ErrBadRequest("avatar_url is invalid")
				}
				return nil
			}
		}
		return service.ErrBadRequest("avatar_url is invalid")
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

func normalizeLocalAvatarURLPrefix(prefix string) string {
	prefix = strings.TrimRight(strings.TrimSpace(prefix), "/")
	if prefix == "" || prefix == "/" || !strings.HasPrefix(prefix, "/") || strings.HasPrefix(prefix, "//") {
		return ""
	}
	if strings.Contains(prefix, "\\") || strings.ContainsAny(prefix, "?#%") || strings.ContainsFunc(prefix, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return ""
	}
	for _, segment := range strings.Split(strings.Trim(prefix, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return ""
		}
	}
	if prefix == "/api" || strings.HasPrefix(prefix, "/api/") {
		return ""
	}
	return prefix
}

func isCanonicalAvatarFileName(value string) bool {
	if value == "" || strings.Contains(value, "/") || strings.Contains(value, "\\") || strings.Contains(value, "%") {
		return false
	}
	dot := strings.LastIndexByte(value, '.')
	if dot <= 0 || dot == len(value)-1 {
		return false
	}
	identifier, extension := value[:dot], value[dot:]
	if extension != strings.ToLower(extension) {
		return false
	}
	parsed, err := uuid.Parse(identifier)
	if err != nil || parsed.String() != identifier {
		return false
	}
	switch extension {
	case ".jpg", ".png", ".gif", ".webp":
		return true
	default:
		return false
	}
}
