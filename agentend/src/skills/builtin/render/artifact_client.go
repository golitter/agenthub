package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var artifactResourceIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type artifactUploadResponse struct {
	Code int `json:"code"`
	Data struct {
		ResourceID string `json:"resource_id"`
	} `json:"data"`
	Msg string `json:"msg"`
}

func uploadHTMLArtifact(content string) (string, error) {
	return uploadHTMLArtifactWithClient(content, nil)
}

// uploadHTMLArtifactWithClient keeps the production path on the private,
// redirect-blocking client while allowing a transport-only unit test without
// opening a real listener in restricted AgentEnd environments.
func uploadHTMLArtifactWithClient(content string, client *http.Client) (string, error) {
	endpoint := strings.TrimSpace(os.Getenv("AGENTHUB_ARTIFACT_ENDPOINT"))
	token := strings.TrimSpace(os.Getenv("AGENTHUB_ARTIFACT_TOKEN"))
	if endpoint == "" && token == "" {
		return "", fmt.Errorf("artifact upload context is unavailable")
	}
	if endpoint == "" || token == "" {
		return "", fmt.Errorf("artifact upload context is incomplete")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("artifact upload endpoint is invalid")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("kind", "html"); err != nil {
		return "", fmt.Errorf("prepare artifact upload: %w", err)
	}
	part, err := writer.CreateFormFile("file", "preview.html")
	if err != nil {
		return "", fmt.Errorf("prepare artifact upload: %w", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		return "", fmt.Errorf("prepare artifact upload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("prepare artifact upload: %w", err)
	}

	request, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return "", fmt.Errorf("create artifact upload request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Idempotency-Key", randomUploadKey())
	request.Header.Set("Expect", "100-continue")
	if client == nil {
		transport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return "", fmt.Errorf("artifact upload transport is unavailable")
		}
		transport = transport.Clone()
		// The capability and HTML must go directly to the configured Backend. Do
		// not let HTTP_PROXY/HTTPS_PROXY from the Agent shell redirect this private
		// upload through an unrelated proxy.
		transport.Proxy = nil
		transport.ExpectContinueTimeout = 2 * time.Second
		client = &http.Client{
			Timeout:   2 * time.Minute,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("artifact upload request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if readErr != nil {
		return "", fmt.Errorf("read artifact upload response: %w", readErr)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("artifact upload rejected (status %d)", response.StatusCode)
	}
	if contentType := strings.ToLower(response.Header.Get("Content-Type")); contentType == "" || !strings.HasPrefix(contentType, "application/json") {
		return "", fmt.Errorf("artifact upload returned an invalid response type")
	}
	var result artifactUploadResponse
	if err := json.Unmarshal(responseBody, &result); err != nil || result.Code != 0 || !artifactResourceIDPattern.MatchString(result.Data.ResourceID) {
		if result.Msg != "" {
			return "", fmt.Errorf("artifact upload rejected: %s", result.Msg)
		}
		return "", fmt.Errorf("artifact upload returned an invalid response")
	}
	return result.Data.ResourceID, nil
}

func randomUploadKey() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("render-%d", time.Now().UnixNano())
	}
	return "render-" + hex.EncodeToString(value)
}
