package handler

import (
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

func (h *WorkspaceHandler) ReadFile(c *gin.Context) {
	workspaceID := c.Param("id")
	filePath := c.Param("filepath")
	h.proxy(c, "GET", fmt.Sprintf("/v1/workspace/%s/files/%s", workspaceID, filePath), nil)
}

func (h *WorkspaceHandler) WriteFile(c *gin.Context) {
	workspaceID := c.Param("id")
	filePath := c.Param("filepath")
	h.proxy(c, "PUT", fmt.Sprintf("/v1/workspace/%s/files/%s", workspaceID, filePath), c.Request.Body)
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
