package agentend_client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agenthub/backend/internal/generated"
)

func TestNewNormalizesBaseURL(t *testing.T) {
	client := New("localhost/", 8001)
	if client.BaseURL() != "http://localhost:8001" {
		t.Fatalf("BaseURL = %q, want http://localhost:8001", client.BaseURL())
	}
}

func TestNewPreservesExplicitScheme(t *testing.T) {
	client := New("https://agentend.example.com/", 443)
	if client.BaseURL() != "https://agentend.example.com:443" {
		t.Fatalf("BaseURL = %q, want https://agentend.example.com:443", client.BaseURL())
	}
}

func TestEscapePathSegment(t *testing.T) {
	escaped := escapePathSegment("session id/with/slash")
	if escaped != "session%20id%2Fwith%2Fslash" {
		t.Fatalf("escaped = %q, want escaped path segment", escaped)
	}
}

func TestStreamAgentWithContextCancelsRequest(t *testing.T) {
	pathCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathCh <- r.URL.Path
		<-r.Context().Done()
	}))
	defer server.Close()

	client := New("localhost", 1)
	client.baseURL = server.URL
	client.streamClient = server.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.StreamAgentWithContext(ctx, &generated.AgentRequest{
		TaskId:    "task-1",
		SessionId: "session-1",
		Message:   "hello",
	})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("err = %v, want context deadline exceeded", err)
	}

	select {
	case path := <-pathCh:
		if path != "/v1/agent/stream" {
			t.Fatalf("path = %q, want /v1/agent/stream", path)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not receive stream request")
	}
}

func TestInstallAndRemoveSkillRejectNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	client := New("localhost", 1)
	client.baseURL = server.URL
	client.httpClient = server.Client()

	if err := client.InstallSkill("codex", "session-1", "reviewer", []byte("zip")); err == nil {
		t.Fatal("InstallSkill accepted 302 response")
	}
	if err := client.RemoveSkill("codex", "session-1", "reviewer"); err == nil {
		t.Fatal("RemoveSkill accepted 302 response")
	}
}

func TestListWorkspacesRejectsNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer server.Close()

	client := New("localhost", 1)
	client.baseURL = server.URL
	client.httpClient = server.Client()

	if _, err := client.ListWorkspaces(); err == nil {
		t.Fatal("ListWorkspaces accepted non-2xx response")
	}
}
