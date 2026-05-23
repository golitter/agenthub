package agentend_client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"agenthub/backend/internal/generated"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(host string, port int) *Client {
	return &Client{
		baseURL:    fmt.Sprintf("%s:%d", host, port),
		httpClient: &http.Client{},
	}
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
