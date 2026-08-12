package impl

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"agenthub/backend/internal/model"
	"agenthub/backend/internal/service"
	"agenthub/backend/pkg/artifact_store"

	"github.com/gin-gonic/gin"
)

const (
	artifactContentCache      = "private, max-age=31536000, immutable"
	artifactContentCSP        = "default-src 'none'; style-src 'unsafe-inline'; img-src data: https:; script-src 'none'; frame-ancestors 'self'"
	defaultArtifactMaxSize    = 25 * 1024 * 1024
	artifactMultipartOverhead = 1 * 1024 * 1024
)

type ArtifactController struct {
	service       service.ArtifactService
	maxObjectSize int64
}

func NewArtifactController(artifactService service.ArtifactService, maxObjectSize ...int64) *ArtifactController {
	limit := int64(defaultArtifactMaxSize)
	if len(maxObjectSize) > 0 && maxObjectSize[0] > 0 {
		limit = maxObjectSize[0]
	}
	return &ArtifactController{service: artifactService, maxObjectSize: limit}
}

// RegisterUploadRoutes is deliberately outside the normal user JWT group;
// the endpoint accepts only the short-lived capability in Authorization.
func (ctrl *ArtifactController) RegisterUploadRoutes(rg *gin.RouterGroup) {
	if ctrl == nil || ctrl.service == nil {
		return
	}
	rg.POST("/artifacts", ctrl.Upload)
}

func (ctrl *ArtifactController) RegisterRoutes(rg *gin.RouterGroup) {
	if ctrl == nil || ctrl.service == nil {
		return
	}
	rg.GET("/artifacts/:resourceId", ctrl.GetMetadata)
	rg.GET("/artifacts/:resourceId/content", ctrl.GetContent)
	rg.HEAD("/artifacts/:resourceId/content", ctrl.GetContent)
}

func (ctrl *ArtifactController) Upload(c *gin.Context) {
	if ctrl == nil || ctrl.service == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": http.StatusServiceUnavailable, "msg": "artifact storage is not configured"})
		return
	}
	token := bearerToken(c.GetHeader("Authorization"))
	if token == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": "missing artifact capability"})
		return
	}
	// Bound the complete multipart request before parsing it. The service still
	// checks the exact object size and digest after reading the single file part.
	requestLimit := ctrl.maxObjectSize + artifactMultipartOverhead
	if requestLimit < ctrl.maxObjectSize || requestLimit <= 0 {
		requestLimit = ctrl.maxObjectSize
	}
	if requestLimit <= 0 {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": http.StatusInternalServerError, "msg": "artifact size limit is invalid"})
		return
	}
	if c.Request.ContentLength > requestLimit {
		writeArtifactRequestTooLarge(c, &http.MaxBytesError{Limit: requestLimit})
		return
	}
	if validator, ok := ctrl.service.(service.ArtifactCapabilityValidator); ok {
		if err := validator.ValidateUploadCapability(c.Request.Context(), token); err != nil {
			handleBizError(c, err)
			return
		}
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, requestLimit)
	if err := c.Request.ParseMultipartForm(2 << 20); err != nil {
		if writeArtifactRequestTooLarge(c, err) {
			return
		}
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "invalid multipart artifact upload"})
		return
	}
	form, err := c.MultipartForm()
	if err != nil || form == nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "invalid multipart artifact upload"})
		return
	}
	defer form.RemoveAll()
	if len(form.Value["kind"]) != 1 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "exactly one artifact kind is required"})
		return
	}
	for field := range form.Value {
		if field != "kind" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "unknown artifact field"})
			return
		}
	}
	for field := range form.File {
		if field != "file" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "unknown artifact file field"})
			return
		}
	}
	kind := strings.TrimSpace(form.Value["kind"][0])
	fileHeaders := form.File["file"]
	if len(fileHeaders) != 1 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"code": http.StatusBadRequest, "msg": "exactly one artifact file is required"})
		return
	}
	file, err := fileHeaders[0].Open()
	if err != nil {
		handleBizError(c, service.ErrBadRequest("invalid artifact file"))
		return
	}
	defer file.Close()
	var result *service.ArtifactInfo
	if uploader, ok := ctrl.service.(service.IdempotentArtifactUploader); ok {
		result, err = uploader.UploadWithIdempotency(c.Request.Context(), token, kind, fileHeaders[0].Filename, c.GetHeader("Idempotency-Key"), file, fileHeaders[0].Size)
	} else {
		result, err = ctrl.service.Upload(c.Request.Context(), token, kind, fileHeaders[0].Filename, file, fileHeaders[0].Size)
	}
	if err != nil {
		handleBizError(c, err)
		return
	}
	voCreated(c, result)
}

func (ctrl *ArtifactController) GetMetadata(c *gin.Context) {
	artifact, err := ctrl.service.Get(c.Param("resourceId"))
	if err != nil {
		handleBizError(c, err)
		return
	}
	voOK(c, artifactPublicResponse(artifact))
}

func (ctrl *ArtifactController) GetContent(c *gin.Context) {
	resourceID := c.Param("resourceId")
	artifact, err := ctrl.service.Get(resourceID)
	if err != nil {
		handleBizError(c, err)
		return
	}
	etag := `"` + artifact.SHA256 + `"`
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", artifactContentCSP)
	c.Header("Cache-Control", artifactContentCache)
	c.Header("ETag", etag)
	c.Header("Content-Type", artifact.ContentType)
	filename := safeArtifactFilename(artifact.Filename)
	c.Header("Content-Disposition", `inline; filename="`+filename+`"`)
	if matchesIfNoneMatch(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}
	if c.Request.Method == http.MethodHead {
		c.Header("Content-Length", strconv.FormatInt(artifact.Size, 10))
		c.Status(http.StatusOK)
		return
	}
	reader, _, err := ctrl.service.Open(c.Request.Context(), resourceID)
	if err != nil {
		handleBizError(c, err)
		return
	}
	defer reader.Close()
	c.Header("Content-Length", strconv.FormatInt(artifact.Size, 10))
	if artifact.Size == 0 {
		c.Status(http.StatusOK)
		return
	}
	if _, err := io.CopyN(c.Writer, reader, artifact.Size); err != nil {
		if !c.Writer.Written() {
			writeArtifactStorageError(c, err)
			return
		}
		c.Abort()
	}
}

func voCreated(c *gin.Context, data interface{}) {
	c.JSON(http.StatusCreated, gin.H{"code": 0, "data": data})
}

func voOK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func bearerToken(header string) string {
	parts := strings.SplitN(strings.TrimSpace(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func writeArtifactStorageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, artifact_store.ErrNotFound):
		c.Status(http.StatusNotFound)
	case errors.Is(err, artifact_store.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		c.Status(http.StatusServiceUnavailable)
	default:
		c.Status(http.StatusInternalServerError)
	}
}

func writeArtifactRequestTooLarge(c *gin.Context, err error) bool {
	var maxErr *http.MaxBytesError
	if !errors.As(err, &maxErr) {
		return false
	}
	c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
		"code": http.StatusRequestEntityTooLarge,
		"msg":  "artifact upload exceeds the configured size limit",
	})
	return true
}

// Keep this small response projection local so object keys and storage
// internals never reach the browser.
func artifactPublicResponse(artifact *model.Artifact) interface{} {
	return service.ArtifactInfo{
		ResourceID:  artifact.ResourceID,
		Kind:        artifact.Kind,
		Filename:    artifact.Filename,
		ContentType: artifact.ContentType,
		Size:        artifact.Size,
		SHA256:      artifact.SHA256,
		CreatedAt:   artifact.CreatedAt,
	}
}

func safeArtifactFilename(filename string) string {
	filename = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' || r == '"' {
			return '_'
		}
		return r
	}, filename)
	if strings.TrimSpace(filename) == "" {
		return "preview.html"
	}
	if len([]byte(filename)) > 255 {
		return "preview.html"
	}
	return filename
}
