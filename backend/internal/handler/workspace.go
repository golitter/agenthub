package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"agenthub/backend/pkg/agentend_client"

	"github.com/gin-gonic/gin"
)

type WorkspaceHandler struct {
	agentClient *agentend_client.Client
	httpClient  *http.Client
}

func NewWorkspaceHandler(agentClient *agentend_client.Client) *WorkspaceHandler {
	return &WorkspaceHandler{
		agentClient: agentClient,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// resolveWorkspaceID queries agentend to map session_id → workspace ID.
func (h *WorkspaceHandler) resolveWorkspaceID(sessionID string) (string, error) {
	url := fmt.Sprintf("%s/v1/workspace/by-session/%s", h.agentClient.BaseURL(), sessionID)
	resp, err := h.httpClient.Get(url)
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

// withResolvedWorkspace resolves session_id → workspace_id and calls fn.
func (h *WorkspaceHandler) withResolvedWorkspace(c *gin.Context, fn func(wsID string)) {
	sessionID := c.Param("sessionId")
	wsID, err := h.resolveWorkspaceID(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	fn(wsID)
}

func (h *WorkspaceHandler) SessionFileRead(c *gin.Context) {
	filePath := c.Param("filepath")
	h.withResolvedWorkspace(c, func(wsID string) {
		h.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/files%s", wsID, filePath), nil)
	})
}

func (h *WorkspaceHandler) SessionFileWrite(c *gin.Context) {
	filePath := c.Param("filepath")
	h.withResolvedWorkspace(c, func(wsID string) {
		h.proxy(c, "PUT", fmt.Sprintf("/v1/workspace/%s/files%s", wsID, filePath), c.Request.Body)
	})
}

func (h *WorkspaceHandler) SessionGetDiff(c *gin.Context) {
	h.withResolvedWorkspace(c, func(wsID string) {
		h.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/diff", wsID), nil)
	})
}

func (h *WorkspaceHandler) SessionCommit(c *gin.Context) {
	h.withResolvedWorkspace(c, func(wsID string) {
		h.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/commit", wsID), c.Request.Body)
	})
}

func (h *WorkspaceHandler) SessionRevert(c *gin.Context) {
	h.withResolvedWorkspace(c, func(wsID string) {
		h.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/revert", wsID), nil)
	})
}

func (h *WorkspaceHandler) ReadFile(c *gin.Context) {
	workspaceID := c.Param("id")
	filePath := c.Param("filepath")
	h.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/files%s", workspaceID, filePath), nil)
}

func (h *WorkspaceHandler) WriteFile(c *gin.Context) {
	workspaceID := c.Param("id")
	filePath := c.Param("filepath")
	h.proxy(c, "PUT", fmt.Sprintf("/v1/workspace/%s/files%s", workspaceID, filePath), c.Request.Body)
}

func (h *WorkspaceHandler) GetDiff(c *gin.Context) {
	workspaceID := c.Param("id")
	h.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/diff", workspaceID), nil)
}

func (h *WorkspaceHandler) Commit(c *gin.Context) {
	workspaceID := c.Param("id")
	h.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/commit", workspaceID), c.Request.Body)
}

func (h *WorkspaceHandler) Revert(c *gin.Context) {
	workspaceID := c.Param("id")
	h.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/revert", workspaceID), nil)
}

func (h *WorkspaceHandler) TaskGitInfo(c *gin.Context) {
	taskID := c.Param("taskId")
	h.proxy(c, "GET", fmt.Sprintf("/v1/workspace/task/%s/git-info", taskID), nil)
}

func (h *WorkspaceHandler) MergeTaskToMain(c *gin.Context) {
	taskID := c.Param("taskId")
	h.proxy(c, "POST", fmt.Sprintf("/v1/workspace/task/%s/merge-to-main", taskID), c.Request.Body)
}

func (h *WorkspaceHandler) StartPreview(c *gin.Context) {
	workspaceID := c.Param("id")
	h.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/preview/start", workspaceID), nil)
}

func (h *WorkspaceHandler) StopPreview(c *gin.Context) {
	workspaceID := c.Param("id")
	h.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/preview/stop", workspaceID), nil)
}

func (h *WorkspaceHandler) proxy(c *gin.Context, method, path string, body io.Reader) {
	url := h.agentClient.BaseURL() + path

	req, err := http.NewRequestWithContext(c.Request.Context(), method, url, body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if body != nil && c.ContentType() != "" {
		req.Header.Set("Content-Type", c.ContentType())
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
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

	io.Copy(c.Writer, resp.Body)
}
