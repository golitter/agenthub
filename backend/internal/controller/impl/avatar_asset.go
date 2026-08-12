package impl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode"

	"agenthub/backend/pkg/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	avatarCacheControl = "public, max-age=31536000, immutable"
	avatarNoSniff      = "nosniff"
)

type avatarAsset struct {
	Key         string
	ContentType string
}

// AssetController exposes only immutable avatar reads. It never accepts a
// client-provided bucket or object key.
type AssetController struct {
	reader storage.ObjectReader
}

func NewAssetController(reader storage.ObjectReader) *AssetController {
	return &AssetController{reader: reader}
}

func (ctrl *AssetController) RegisterRoutes(rg *gin.RouterGroup) {
	if ctrl == nil || ctrl.reader == nil {
		return
	}
	rg.GET("/avatars/*path", ctrl.GetAvatar)
	rg.HEAD("/avatars/*path", ctrl.GetAvatar)
}

func (ctrl *AssetController) GetAvatar(c *gin.Context) {
	asset, err := parseAvatarAssetPath(c.Param("path"))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("X-Content-Type-Options", avatarNoSniff)

	var info storage.ObjectInfo
	var reader io.ReadCloser
	if c.Request.Method == http.MethodHead {
		info, err = ctrl.reader.Stat(c.Request.Context(), asset.Key)
	} else {
		reader, info, err = ctrl.reader.Open(c.Request.Context(), asset.Key)
		if reader != nil {
			defer reader.Close()
		}
	}
	if err != nil {
		writeAssetStorageError(c, err)
		return
	}
	if info.Size < 0 {
		c.Status(http.StatusInternalServerError)
		return
	}
	if c.Request.Method == http.MethodGet && reader == nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	etag := strongAvatarETag(info)
	c.Header("Cache-Control", avatarCacheControl)
	c.Header("ETag", etag)
	if matchesIfNoneMatch(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}

	// The extension is validated by parseAvatarAssetPath; never reflect an
	// arbitrary object metadata value into the response Content-Type.
	c.Header("Content-Type", asset.ContentType)
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size))
	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	if info.Size == 0 {
		c.Status(http.StatusOK)
		return
	}
	if _, err := io.CopyN(c.Writer, reader, info.Size); err != nil {
		// A read can fail after Open/Stat succeeded (for example, when the
		// upstream connection drops). If no bytes were committed yet, still
		// return the correct storage status instead of a false 200.
		if !c.Writer.Written() {
			clearAvatarSuccessHeaders(c)
			writeAssetStorageError(c, err)
			return
		}
		c.Abort()
	}
}

func clearAvatarSuccessHeaders(c *gin.Context) {
	c.Writer.Header().Del("Cache-Control")
	c.Writer.Header().Del("ETag")
	c.Writer.Header().Del("Content-Type")
	c.Writer.Header().Del("Content-Length")
}

func parseAvatarAssetPath(raw string) (avatarAsset, error) {
	if raw == "" || strings.Contains(raw, "\\") || strings.Contains(raw, "%") || strings.ContainsFunc(raw, unicode.IsControl) {
		return avatarAsset{}, fmt.Errorf("invalid avatar asset path")
	}
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" || strings.Contains(raw, "/") {
		return avatarAsset{}, fmt.Errorf("invalid avatar asset path")
	}
	dot := strings.LastIndexByte(raw, '.')
	if dot <= 0 || dot == len(raw)-1 {
		return avatarAsset{}, fmt.Errorf("invalid avatar asset path")
	}
	identifier, extension := raw[:dot], raw[dot:]
	if extension != strings.ToLower(extension) {
		return avatarAsset{}, fmt.Errorf("invalid avatar asset extension")
	}
	parsed, err := uuid.Parse(identifier)
	if err != nil || parsed.String() != identifier {
		return avatarAsset{}, fmt.Errorf("invalid avatar asset uuid")
	}
	contentTypes := map[string]string{
		".jpg":  "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
	}
	contentType, ok := contentTypes[extension]
	if !ok {
		return avatarAsset{}, fmt.Errorf("invalid avatar asset extension")
	}
	return avatarAsset{Key: "avatars/" + identifier + extension, ContentType: contentType}, nil
}

func strongAvatarETag(info storage.ObjectInfo) string {
	value := strings.TrimSpace(info.SHA256)
	if value == "" {
		value = strings.TrimSpace(info.ETag)
	}
	value = strings.Trim(value, "\"")
	if value == "" || strings.ContainsAny(value, "\"\\") || strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) {
		return "\"unknown\""
	}
	return `"` + value + `"`
}

func matchesIfNoneMatch(header, etag string) bool {
	if strings.TrimSpace(header) == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

func writeAssetStorageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		c.Status(http.StatusNotFound)
	case errors.Is(err, storage.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		c.Status(http.StatusServiceUnavailable)
	case errors.Is(err, storage.ErrPermission):
		c.Status(http.StatusInternalServerError)
	default:
		c.Status(http.StatusInternalServerError)
	}
}
