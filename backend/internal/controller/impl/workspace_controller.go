package impl

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"agenthub/backend/pkg/agentend_client"

	"github.com/gin-gonic/gin"
)

type WorkspaceController struct {
	agentClient *agentend_client.Client
	httpClient  *http.Client
}

const maxWorkspaceProxyBodySize = 25 << 20

func NewWorkspaceController(agentClient *agentend_client.Client) *WorkspaceController {
	return &WorkspaceController{
		agentClient: agentClient,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (ctrl *WorkspaceController) RegisterRoutes(rg *gin.RouterGroup) {
	ws := rg.Group("/workspace")
	{
		ws.GET("/task/:taskId/git-info", ctrl.TaskGitInfo)
		ws.POST("/task/:taskId/merge-to-main", ctrl.MergeTaskToMain)
		ws.GET("/:id/files/*filepath", ctrl.ReadFile)
		ws.PUT("/:id/files/*filepath", ctrl.WriteFile)
		ws.GET("/:id/diff", ctrl.GetDiff)
		ws.POST("/:id/commit", ctrl.Commit)
		ws.POST("/:id/revert", ctrl.Revert)
		ws.POST("/:id/preview/start", ctrl.StartPreview)
		ws.POST("/:id/preview/stop", ctrl.StopPreview)
	}

	ss := rg.Group("/session")
	{
		ss.GET("/:sessionId/files/*filepath", ctrl.SessionFileRead)
		ss.PUT("/:sessionId/files/*filepath", ctrl.SessionFileWrite)
		ss.GET("/:sessionId/diff", ctrl.SessionGetDiff)
		ss.POST("/:sessionId/commit", ctrl.SessionCommit)
		ss.POST("/:sessionId/revert", ctrl.SessionRevert)
	}
}

func sanitizePath(p string) (string, bool) {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", false
		}
	}
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	for _, seg := range strings.Split(cleaned, "/") {
		if seg == ".." {
			return "", false
		}
	}
	return cleaned, true
}

func workspaceFileURLPath(p string) string {
	if p == "" || p == "." || p == "/" {
		return "/"
	}

	trimmed := strings.TrimPrefix(p, "/")
	segments := strings.Split(trimmed, "/")
	escaped := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		escaped = append(escaped, url.PathEscape(seg))
	}
	return "/" + strings.Join(escaped, "/")
}

func (ctrl *WorkspaceController) resolveWorkspaceID(sessionID string) (string, error) {
	reqURL := fmt.Sprintf("%s/v1/workspace/by-session/%s", ctrl.agentClient.BaseURL(), url.PathEscape(sessionID))
	resp, err := ctrl.httpClient.Get(reqURL)
	if err != nil {
		return "", fmt.Errorf("agentend unavailable")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("workspace not found for session %s", sessionID)
	}

	var ws struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ws); err != nil {
		return "", fmt.Errorf("invalid workspace response")
	}
	return ws.ID, nil
}

func (ctrl *WorkspaceController) withResolvedWorkspace(c *gin.Context, fn func(wsID string)) {
	wsID, err := ctrl.resolveWorkspaceID(c.Param("sessionId"))
	if err != nil {
		slog.Warn("resolve workspace failed", "session_id", c.Param("sessionId"), "error", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "workspace not found"})
		return
	}
	fn(wsID)
}

func (ctrl *WorkspaceController) SessionFileRead(c *gin.Context) {
	filePath, ok := sanitizePath(c.Param("filepath"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file path"})
		return
	}
	ctrl.withResolvedWorkspace(c, func(wsID string) {
		ctrl.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/files%s", url.PathEscape(wsID), workspaceFileURLPath(filePath)), nil)
	})
}

func (ctrl *WorkspaceController) SessionFileWrite(c *gin.Context) {
	filePath, ok := sanitizePath(c.Param("filepath"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file path"})
		return
	}
	ctrl.withResolvedWorkspace(c, func(wsID string) {
		ctrl.proxy(c, "PUT", fmt.Sprintf("/v1/workspace/%s/files%s", url.PathEscape(wsID), workspaceFileURLPath(filePath)), c.Request.Body)
	})
}

func (ctrl *WorkspaceController) SessionGetDiff(c *gin.Context) {
	ctrl.withResolvedWorkspace(c, func(wsID string) {
		ctrl.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/diff", url.PathEscape(wsID)), nil)
	})
}

func (ctrl *WorkspaceController) SessionCommit(c *gin.Context) {
	ctrl.withResolvedWorkspace(c, func(wsID string) {
		ctrl.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/commit", url.PathEscape(wsID)), c.Request.Body)
	})
}

func (ctrl *WorkspaceController) SessionRevert(c *gin.Context) {
	ctrl.withResolvedWorkspace(c, func(wsID string) {
		ctrl.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/revert", url.PathEscape(wsID)), nil)
	})
}

func (ctrl *WorkspaceController) ReadFile(c *gin.Context) {
	filePath, ok := sanitizePath(c.Param("filepath"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file path"})
		return
	}
	ctrl.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/files%s", url.PathEscape(c.Param("id")), workspaceFileURLPath(filePath)), nil)
}

func (ctrl *WorkspaceController) WriteFile(c *gin.Context) {
	filePath, ok := sanitizePath(c.Param("filepath"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid file path"})
		return
	}
	ctrl.proxy(c, "PUT", fmt.Sprintf("/v1/workspace/%s/files%s", url.PathEscape(c.Param("id")), workspaceFileURLPath(filePath)), c.Request.Body)
}

func (ctrl *WorkspaceController) GetDiff(c *gin.Context) {
	ctrl.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/diff", url.PathEscape(c.Param("id"))), nil)
}

func (ctrl *WorkspaceController) Commit(c *gin.Context) {
	ctrl.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/commit", url.PathEscape(c.Param("id"))), c.Request.Body)
}

func (ctrl *WorkspaceController) Revert(c *gin.Context) {
	ctrl.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/revert", url.PathEscape(c.Param("id"))), nil)
}

func (ctrl *WorkspaceController) TaskGitInfo(c *gin.Context) {
	ctrl.proxy(c, "GET", fmt.Sprintf("/v1/workspace/task/%s/git-info", url.PathEscape(c.Param("taskId"))), nil)
}

func (ctrl *WorkspaceController) MergeTaskToMain(c *gin.Context) {
	ctrl.proxy(c, "POST", fmt.Sprintf("/v1/workspace/task/%s/merge-to-main", url.PathEscape(c.Param("taskId"))), c.Request.Body)
}

func (ctrl *WorkspaceController) StartPreview(c *gin.Context) {
	ctrl.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/preview/start", url.PathEscape(c.Param("id"))), nil)
}

func (ctrl *WorkspaceController) StopPreview(c *gin.Context) {
	ctrl.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/preview/stop", url.PathEscape(c.Param("id"))), nil)
}

func (ctrl *WorkspaceController) proxy(c *gin.Context, method, path string, body io.Reader) {
	url := ctrl.agentClient.BaseURL() + path
	if body != nil {
		if c.Request.ContentLength > maxWorkspaceProxyBodySize {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body exceeds 25MB limit"})
			return
		}
		body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkspaceProxyBodySize)
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), method, url, body)
	if err != nil {
		slog.Error("failed to create workspace proxy request", "method", method, "path", path, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if body != nil && c.ContentType() != "" {
		req.Header.Set("Content-Type", c.ContentType())
	}

	resp, err := ctrl.httpClient.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body exceeds 25MB limit"})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "agentend unavailable"})
		return
	}
	defer resp.Body.Close()

	for k, vs := range resp.Header {
		for _, v := range vs {
			c.Writer.Header().Add(k, v)
		}
	}
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		slog.Warn("failed to proxy workspace response", "path", path, "error", err)
	}
}
