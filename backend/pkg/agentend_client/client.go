package agentend_client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agenthub/backend/internal/generated"
)

type Client struct {
	baseURL      string
	httpClient   *http.Client
	streamClient *http.Client
}

func New(host string, port int) *Client {
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	host = strings.TrimRight(host, "/")
	return &Client{
		baseURL:    fmt.Sprintf("%s:%d", host, port),
		httpClient: &http.Client{Timeout: 60 * time.Second},
		streamClient: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 30 * time.Second,
				ExpectContinueTimeout: 2 * time.Second,
			},
		},
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func escapePathSegment(value string) string {
	return url.PathEscape(value)
}

func escapeQueryValue(value string) string {
	return url.QueryEscape(value)
}

func statusError(action string, resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	detail := strings.TrimSpace(string(body))
	if detail != "" {
		return fmt.Errorf("%s failed: status %d: %s", action, resp.StatusCode, detail)
	}
	return fmt.Errorf("%s failed: status %d", action, resp.StatusCode)
}

func (c *Client) StreamAgent(req *generated.AgentRequest) (*http.Response, error) {
	return c.StreamAgentWithContext(context.Background(), req)
}

func (c *Client) StreamAgentWithContext(ctx context.Context, req *generated.AgentRequest) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/agent/stream", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	return c.streamClient.Do(httpReq)
}

type ReviewRequest struct {
	SessionID string `json:"session_id"`
	Action    string `json:"action"`
	Content   string `json:"content,omitempty"`
}

func (c *Client) ReviewAgent(req ReviewRequest) (map[string]interface{}, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal review request: %w", err)
	}
	httpReq, err := http.NewRequest("POST", c.baseURL+"/v1/agent/review", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create review request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("submit review: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(respBody) > 0 {
			return nil, fmt.Errorf("agent review failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		}
		return nil, fmt.Errorf("agent review failed: status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if len(respBody) == 0 {
		return map[string]interface{}{"status": "ok"}, nil
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode review response: %w", err)
	}
	return result, nil
}

type ValidateRepoPathResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

func (c *Client) ValidateRepoPath(repoPath string) (*ValidateRepoPathResult, error) {
	body, err := json.Marshal(map[string]string{"repo_path": repoPath})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequest("POST", c.baseURL+"/v1/validate-repo-path", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("validate repo path: %w", err)
	}
	defer resp.Body.Close()

	if err := statusError("validate repo path", resp); err != nil {
		return nil, err
	}

	var result ValidateRepoPathResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

type InitGitRepoResult struct {
	Success bool     `json:"success"`
	Errors  []string `json:"errors"`
}

func (c *Client) InitGitRepo(repoPath string) (*InitGitRepoResult, error) {
	body, err := json.Marshal(map[string]string{"repo_path": repoPath})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequest("POST", c.baseURL+"/v1/init-git-repo", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("init git repo: %w", err)
	}
	defer resp.Body.Close()

	if err := statusError("init git repo", resp); err != nil {
		return nil, err
	}

	var result InitGitRepoResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

func (c *Client) HealthCheck() error {
	resp, err := c.httpClient.Get(c.baseURL + "/health")
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	defer resp.Body.Close()
	return statusError("health check", resp)
}

func (c *Client) GetResources() (*http.Response, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/v1/resources")
	if err != nil {
		return nil, fmt.Errorf("get resources: %w", err)
	}
	return resp, nil
}

// AgentConfigInfo represents an agent's config returned by Agentend.
type AgentConfigInfo struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ConfigPath    string `json:"configPath"`
	ConfigContent string `json:"configContent"`
}

// GetAgentConfigs calls Agentend to read each agent CLI's system-level config file.
func (c *Client) GetAgentConfigs() ([]AgentConfigInfo, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/v1/agents/configs")
	if err != nil {
		return nil, fmt.Errorf("get agent configs: %w", err)
	}
	defer resp.Body.Close()

	if err := statusError("get agent configs", resp); err != nil {
		return nil, err
	}

	var configs []AgentConfigInfo
	if err := json.NewDecoder(resp.Body).Decode(&configs); err != nil {
		return nil, fmt.Errorf("decode agent configs: %w", err)
	}
	return configs, nil
}

type AnnouncementUnpinRequest struct {
	SharedDir  string `json:"shared_dir"`
	Content    string `json:"content"`
	SenderName string `json:"sender_name"`
}

func (c *Client) NotifyAnnouncementUnpin(req AnnouncementUnpinRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal unpin request: %w", err)
	}
	httpReq, err := http.NewRequest("POST", c.baseURL+"/v1/pin/announcement-unpin", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create unpin request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("notify announcement unpin: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("announcement unpin failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// DestroySession terminates an AgentEnd session process (best-effort).
func (c *Client) DestroySession(sessionID string) error {
	req, err := http.NewRequest("DELETE", c.baseURL+"/v1/session/"+escapePathSegment(sessionID), nil)
	if err != nil {
		return fmt.Errorf("create destroy session request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("destroy session %s: %w", sessionID, err)
	}
	defer resp.Body.Close()
	return statusError("destroy session "+sessionID, resp)
}

// CleanupByTask cleans up all workspaces and git branches for a task (best-effort).
func (c *Client) CleanupByTask(taskID string) error {
	req, err := http.NewRequest("DELETE", c.baseURL+"/v1/workspace/task/"+escapePathSegment(taskID), nil)
	if err != nil {
		return fmt.Errorf("create cleanup task request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cleanup task %s workspaces: %w", taskID, err)
	}
	defer resp.Body.Close()
	return statusError("cleanup task "+taskID+" workspaces", resp)
}

// CleanupTaskBranches force-cleans task branches even without active workspaces.
func (c *Client) CleanupTaskBranches(taskID string, repoPath string) error {
	body, err := json.Marshal(map[string]string{"repo_path": repoPath})
	if err != nil {
		return fmt.Errorf("marshal cleanup branches request: %w", err)
	}
	req, err := http.NewRequest("POST", c.baseURL+"/v1/workspace/task/"+escapePathSegment(taskID)+"/cleanup-branches", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create cleanup branches request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cleanup task %s branches: %w", taskID, err)
	}
	defer resp.Body.Close()
	return statusError("cleanup task "+taskID+" branches", resp)
}

// SkillInfo represents a skill returned by Agentend.
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
	Source      string `json:"source"`
}

// FetchSkills calls Agentend to scan workspace skills directory.
// Uses session_id to let Agentend resolve the correct worktree path.
func (c *Client) FetchSkills(agentType, sessionID string) ([]SkillInfo, error) {
	reqURL := fmt.Sprintf("%s/v1/skills/%s?session_id=%s", c.baseURL, escapePathSegment(agentType), escapeQueryValue(sessionID))
	resp, err := c.httpClient.Get(reqURL)
	if err != nil {
		return nil, fmt.Errorf("fetch skills: %w", err)
	}
	defer resp.Body.Close()

	if err := statusError("fetch skills", resp); err != nil {
		return nil, err
	}

	var skills []SkillInfo
	if err := json.NewDecoder(resp.Body).Decode(&skills); err != nil {
		return nil, fmt.Errorf("decode skills: %w", err)
	}
	return skills, nil
}

// RemoveSkill tells Agentend to remove a skill directory from the worktree.
func (c *Client) RemoveSkill(agentType, sessionID, skillName string) error {
	req, err := http.NewRequest("DELETE",
		fmt.Sprintf("%s/v1/skills/%s/%s?session_id=%s", c.baseURL, escapePathSegment(agentType), escapePathSegment(skillName), escapeQueryValue(sessionID)), nil)
	if err != nil {
		return fmt.Errorf("create remove skill request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("remove skill %s: %w", skillName, err)
	}
	defer resp.Body.Close()
	return statusError("remove skill "+skillName, resp)
}

// WorkspaceInfo represents a workspace returned by Agentend.
type WorkspaceInfo struct {
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	AgentName    string `json:"agent_name"`
	AgentType    string `json:"agent_type"`
	RepoPath     string `json:"repo_path"`
	WorktreePath string `json:"worktree_path"`
	BranchName   string `json:"branch_name"`
	SessionID    string `json:"session_id"`
	Status       string `json:"status"`
	CreatedAt    string `json:"created_at"`
}

// ListWorkspaces calls Agentend to get all workspaces.
func (c *Client) ListWorkspaces() ([]WorkspaceInfo, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/v1/workspace")
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer resp.Body.Close()

	if err := statusError("list workspaces", resp); err != nil {
		return nil, err
	}

	var workspaces []WorkspaceInfo
	if err := json.NewDecoder(resp.Body).Decode(&workspaces); err != nil {
		return nil, fmt.Errorf("decode workspaces: %w", err)
	}
	return workspaces, nil
}

// CleanupWorkspace calls Agentend to cleanup a single workspace by ID.
func (c *Client) CleanupWorkspace(workspaceID string) error {
	req, err := http.NewRequest("DELETE", c.baseURL+"/v1/workspace/"+escapePathSegment(workspaceID), nil)
	if err != nil {
		return fmt.Errorf("create cleanup workspace request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cleanup workspace %s: %w", workspaceID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("workspace %s not found or already cleaned", workspaceID)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cleanup workspace %s failed: status %d", workspaceID, resp.StatusCode)
	}
	return nil
}

// InstallSkill sends a zip archive to Agentend to install into the worktree.
func (c *Client) InstallSkill(agentType, sessionID, skillName string, zipData []byte) error {
	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/v1/skills/%s/%s/install?session_id=%s", c.baseURL, escapePathSegment(agentType), escapePathSegment(skillName), escapeQueryValue(sessionID)),
		bytes.NewReader(zipData))
	if err != nil {
		return fmt.Errorf("create install skill request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("install skill %s: %w", skillName, err)
	}
	defer resp.Body.Close()
	return statusError("install skill "+skillName, resp)
}
