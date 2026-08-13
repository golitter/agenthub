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

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestServiceAuthTransportInjectsTokenWithoutMutatingRequest(t *testing.T) {
	seen := ""
	transport := serviceAuthTransport{
		token: "service-secret",
		base: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			seen = req.Header.Get("Authorization")
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
		}),
	}
	req, err := http.NewRequest(http.MethodGet, "http://agentend/v1/runs/demo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if seen != "Bearer service-secret" {
		t.Fatalf("Authorization = %q", seen)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("transport mutated caller request")
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
	releaseHandler := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathCh <- r.URL.Path
		select {
		case <-r.Context().Done():
		case <-releaseHandler:
		}
	}))
	defer func() {
		close(releaseHandler)
		server.CloseClientConnections()
		server.Close()
	}()

	client := New("localhost", 1)
	client.baseURL = server.URL
	client.streamClient = server.Client()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		resp, err := client.StreamAgentWithContext(ctx, &generated.AgentRequest{
			TaskId:    "task-1",
			SessionId: "session-1",
			Message:   "hello",
		})
		if resp != nil {
			_ = resp.Body.Close()
		}
		errCh <- err
	}()

	select {
	case path := <-pathCh:
		if path != "/v1/agent/stream" {
			t.Fatalf("path = %q, want /v1/agent/stream", path)
		}
	case <-time.After(time.Second):
		cancel()
		t.Fatal("server did not receive stream request")
	}

	cancel()
	err := <-errCh
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v, want context canceled", err)
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
	} else {
		var statusErr *HTTPStatusError
		if errors.As(err, &statusErr) {
			if statusErr.KnownFailure() {
				t.Fatal("3xx response should remain an unknown outcome")
			}
		}
	}
	if err := client.RemoveSkill("codex", "session-1", "reviewer"); err == nil {
		t.Fatal("RemoveSkill accepted 302 response")
	}
}

func TestSkillMutationDoesNotFollowRedirect(t *testing.T) {
	var redirected bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			redirected = true
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/redirected", http.StatusFound)
	}))
	defer server.Close()

	client := New("localhost", 1)
	client.baseURL = server.URL
	if err := client.InstallSkill("codex", "session-1", "reviewer", []byte("zip")); err == nil {
		t.Fatal("InstallSkill accepted a redirect response")
	}
	if redirected {
		t.Fatal("InstallSkill followed a redirect and observed an unrelated response")
	}
}

func TestSkillMutationRejectsApplicationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"error":"unsafe archive"}`))
	}))
	defer server.Close()

	client := New("localhost", 1)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	if err := client.InstallSkill("codex", "session-1", "reviewer", []byte("zip")); err == nil || !strings.Contains(err.Error(), "unsafe archive") {
		t.Fatalf("InstallSkill error = %v, want application failure", err)
	} else {
		var mutationErr *SkillMutationError
		if !errors.As(err, &mutationErr) || !mutationErr.KnownFailure() {
			t.Fatalf("InstallSkill error type = %T, want known SkillMutationError", err)
		}
	}
	if err := client.RemoveSkill("codex", "session-1", "reviewer"); err == nil || !strings.Contains(err.Error(), "unsafe archive") {
		t.Fatalf("RemoveSkill error = %v, want application failure", err)
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
