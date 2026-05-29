package agentend_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agenthub/backend/internal/generated"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(host string, port int) *Client {
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return &Client{
		baseURL:    fmt.Sprintf("%s:%d", host, port),
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) StreamAgent(req *generated.AgentRequest) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequest("POST", c.baseURL+"/v1/agent/stream", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	return c.httpClient.Do(httpReq)
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

	var result ValidateRepoPathResult
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
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) GetResources() (*http.Response, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/v1/resources")
	if err != nil {
		return nil, fmt.Errorf("get resources: %w", err)
	}
	return resp, nil
}
