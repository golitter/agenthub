package agentend_client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"agenthub/backend/internal/generated"
)

type Client struct {
	baseURL      string
	httpClient   *http.Client
	streamClient *http.Client
}

type serviceAuthTransport struct {
	base  http.RoundTripper
	token string
}

func (t serviceAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	if t.token != "" {
		clone.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(clone)
}

// HTTPStatusError preserves the AgentEnd response status so callers that own
// a durable side effect can distinguish a deterministic request rejection
// (4xx, no install/remove mutation) from an unknown server/network outcome.
// The latter must remain retryable because AgentEnd may have committed the
// filesystem change before the connection failed.
type HTTPStatusError struct {
	Action     string
	StatusCode int
	Detail     string
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return "AgentEnd request failed"
	}
	if e.Detail != "" {
		return fmt.Sprintf("%s failed: status %d: %s", e.Action, e.StatusCode, e.Detail)
	}
	return fmt.Sprintf("%s failed: status %d", e.Action, e.StatusCode)
}

// KnownFailure reports that AgentEnd rejected the request before a successful
// mutation.  Backend uses this marker to remove a failed install reservation;
// 5xx responses intentionally remain unknown and retryable.
func (e *HTTPStatusError) KnownFailure() bool {
	return e != nil && e.StatusCode >= http.StatusBadRequest && e.StatusCode < http.StatusInternalServerError
}

// SkillMutationError represents an explicit {success:false} application
// response. AgentEnd returns this only after rejecting the mutation, so it is
// safe to roll back the corresponding durable reservation.
type SkillMutationError struct {
	Action string
	Detail string
}

func (e *SkillMutationError) Error() string {
	if e == nil {
		return "AgentEnd skill mutation failed"
	}
	if e.Detail != "" {
		return fmt.Sprintf("%s failed: %s", e.Action, e.Detail)
	}
	return fmt.Sprintf("%s failed", e.Action)
}

func (e *SkillMutationError) KnownFailure() bool { return e != nil }

func New(host string, port int) *Client {
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	host = strings.TrimRight(host, "/")
	token := strings.TrimSpace(os.Getenv("AGENTEND_SERVICE_TOKEN"))
	defaultTransport := http.DefaultTransport
	streamTransport := &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 2 * time.Second,
	}
	return &Client{
		baseURL: fmt.Sprintf("%s:%d", host, port),
		// A redirect is not a successful Skill mutation: following it could
		// turn an intermediate/unknown AgentEnd outcome into an unrelated 2xx
		// response and make the Backend commit the wrong reservation state.
		httpClient: &http.Client{
			Transport: serviceAuthTransport{base: defaultTransport, token: token},
			Timeout:   60 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		streamClient: &http.Client{
			Transport: serviceAuthTransport{base: streamTransport, token: token},
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *Client) GetRun(ctx context.Context, runID string) (*generated.AgentRunStatus, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/runs/"+escapePathSegment(runID), nil)
	if err != nil {
		return nil, fmt.Errorf("create get run request: %w", err)
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	defer resp.Body.Close()
	if err := statusError("get run", resp); err != nil {
		return nil, err
	}
	var status generated.AgentRunStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode run status: %w", err)
	}
	return &status, nil
}

func (c *Client) CancelRun(ctx context.Context, runID string, reason generated.AgentRunTerminationReason) (*generated.CancelAgentRunResponse, error) {
	body, err := json.Marshal(generated.CancelAgentRunRequest{Reason: reason})
	if err != nil {
		return nil, fmt.Errorf("marshal cancel run: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/runs/"+escapePathSegment(runID)+"/cancel", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create cancel run request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cancel run: %w", err)
	}
	defer resp.Body.Close()
	if err := statusError("cancel run", resp); err != nil {
		return nil, err
	}
	var result generated.CancelAgentRunResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode cancel run: %w", err)
	}
	return &result, nil
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
	return &HTTPStatusError{Action: action, StatusCode: resp.StatusCode, Detail: detail}
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

// AgentConfigInfo 表示 AgentEnd 返回的某个 Agent 配置。
type AgentConfigInfo struct {
	Type          string `json:"type"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ConfigPath    string `json:"configPath"`
	ConfigContent string `json:"configContent"`
}

// GetAgentConfigs 调用 AgentEnd 读取各 Agent CLI 的系统级配置文件。
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

// DestroySession 终止 AgentEnd 会话进程（best-effort，尽力而为）。
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

// CleanupByTask 清理某个 task 名下的全部工作区与 git 分支（best-effort，尽力而为）。
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

// CleanupTaskBranches 即使没有活跃工作区，也强制清理该 task 的分支。
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

// SkillInfo 表示 AgentEnd 返回的技能信息。
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
	Source      string `json:"source"`
}

// FetchSkills 调用 AgentEnd 扫描工作区的技能目录。
// 通过 session_id 让 AgentEnd 解析出正确的 worktree 路径。
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

// RemoveSkill 通知 AgentEnd 从 worktree 中移除某个技能目录。
func (c *Client) RemoveSkill(agentType, sessionID, skillName string) error {
	return c.RemoveSkillWithContext(context.Background(), agentType, sessionID, skillName)
}

// RemoveSkillWithContext removes a skill while honoring the caller's request cancellation.
func (c *Client) RemoveSkillWithContext(ctx context.Context, agentType, sessionID, skillName string) error {
	req, err := http.NewRequest("DELETE",
		fmt.Sprintf("%s/v1/skills/%s/%s?session_id=%s", c.baseURL, escapePathSegment(agentType), escapePathSegment(skillName), escapeQueryValue(sessionID)), nil)
	if err != nil {
		return fmt.Errorf("create remove skill request: %w", err)
	}
	req = req.WithContext(ctx)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("remove skill %s: %w", skillName, err)
	}
	defer resp.Body.Close()
	if err := statusError("remove skill "+skillName, resp); err != nil {
		return err
	}
	return decodeSkillMutationResult("remove skill "+skillName, resp.Body)
}

// WorkspaceInfo 表示 AgentEnd 返回的工作区信息。
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

// ListWorkspaces 调用 AgentEnd 获取全部工作区。
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

// CleanupWorkspace 调用 AgentEnd 按 ID 清理单个工作区。
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

// InstallSkill 将 zip 压缩包发送给 AgentEnd，安装到对应 worktree 中。
func (c *Client) InstallSkill(agentType, sessionID, skillName string, zipData []byte) error {
	return c.InstallSkillWithContext(context.Background(), agentType, sessionID, skillName, zipData)
}

// InstallSkillWithContext installs a skill while honoring the caller's request cancellation.
func (c *Client) InstallSkillWithContext(ctx context.Context, agentType, sessionID, skillName string, zipData []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST",
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
	if err := statusError("install skill "+skillName, resp); err != nil {
		return err
	}
	return decodeSkillMutationResult("install skill "+skillName, resp.Body)
}

func decodeSkillMutationResult(action string, body io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(body, 4096))
	if err != nil {
		return fmt.Errorf("%s response read failed: %w", action, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var result struct {
		Success *bool  `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("%s response decode failed: %w", action, err)
	}
	if result.Success != nil && !*result.Success {
		return &SkillMutationError{Action: action, Detail: result.Error}
	}
	return nil
}
