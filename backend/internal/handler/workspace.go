package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"agenthub/backend/pkg/agentend_client"

	"github.com/gin-gonic/gin"
)

type WorkspaceHandler struct {
	agentClient *agentend_client.Client
}

func NewWorkspaceHandler(agentClient *agentend_client.Client) *WorkspaceHandler {
	return &WorkspaceHandler{agentClient: agentClient}
}

// resolveWorkspaceID 通过 session_id 查询 AgentEnd 获取 workspace ID
func (h *WorkspaceHandler) resolveWorkspaceID(sessionID string) (string, error) {
	url := fmt.Sprintf("%s/v1/workspace/by-session/%s", h.agentClient.BaseURL(), sessionID)
	resp, err := http.DefaultClient.Get(url)
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

// SessionFileRead 通过 session_id 查找 workspace 后 proxy 文件读取
func (h *WorkspaceHandler) SessionFileRead(c *gin.Context) {
	sessionID := c.Param("sessionId")
	filePath := c.Param("filepath")
	wsID, err := h.resolveWorkspaceID(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	h.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/files%s", wsID, filePath), nil)
}

// SessionFileWrite 通过 session_id 查找 workspace 后 proxy 文件写入
func (h *WorkspaceHandler) SessionFileWrite(c *gin.Context) {
	sessionID := c.Param("sessionId")
	filePath := c.Param("filepath")
	wsID, err := h.resolveWorkspaceID(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	h.proxy(c, "PUT", fmt.Sprintf("/v1/workspace/%s/files%s", wsID, filePath), c.Request.Body)
}

// SessionGetDiff 通过 session_id 查找 workspace 后 proxy diff
func (h *WorkspaceHandler) SessionGetDiff(c *gin.Context) {
	sessionID := c.Param("sessionId")
	wsID, err := h.resolveWorkspaceID(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	h.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/diff", wsID), nil)
}

// SessionCommit 通过 session_id 查找 workspace 后 proxy commit
func (h *WorkspaceHandler) SessionCommit(c *gin.Context) {
	sessionID := c.Param("sessionId")
	wsID, err := h.resolveWorkspaceID(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	h.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/commit", wsID), c.Request.Body)
}

// SessionRevert 通过 session_id 查找 workspace 后 proxy revert
func (h *WorkspaceHandler) SessionRevert(c *gin.Context) {
	sessionID := c.Param("sessionId")
	wsID, err := h.resolveWorkspaceID(sessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	h.proxy(c, "POST", fmt.Sprintf("/v1/workspace/%s/revert", wsID), nil)
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

	// Copy content type from original request if body is present
	if body != nil && c.ContentType() != "" {
		req.Header.Set("Content-Type", c.ContentType())
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "agentend unavailable"})
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for k, vs := range resp.Header {
		for _, v := range vs {
			c.Writer.Header().Add(k, v)
		}
	}
	c.Writer.WriteHeader(resp.StatusCode)

	// Stream response body using io.Copy
	io.Copy(c.Writer, resp.Body)
}
