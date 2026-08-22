package impl

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agenthub/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func TestNormalizeControllerRepoPathValue(t *testing.T) {
	repoPath, message := normalizeControllerRepoPathValue(" /repo ")
	if message != "" {
		t.Fatalf("normalizeControllerRepoPathValue message: %s", message)
	}
	if repoPath != "/repo" {
		t.Fatalf("repoPath = %q, want /repo", repoPath)
	}

	if _, message := normalizeControllerRepoPathValue("   "); message == "" {
		t.Fatal("blank repo_path accepted")
	}
	if _, message := normalizeControllerRepoPathValue(strings.Repeat("x", maxControllerRepoPathLen+1)); message == "" {
		t.Fatal("too long repo_path accepted")
	}
	if _, message := normalizeControllerRepoPathValue("bad\x00path"); message == "" {
		t.Fatal("repo_path containing NUL accepted")
	}
}

func TestConflictActionBindingRequiresExpectedAttemptButAcceptsZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	bind := func(body string) (*service.ConflictActionInput, error) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		var input service.ConflictActionInput
		return &input, ctx.ShouldBindJSON(&input)
	}

	base := `{"action":"retry","session_id":"session-1","root_run_id":"11111111-1111-4111-8111-111111111111"}`
	if _, err := bind(base); err == nil {
		t.Fatal("missing expected_attempt was accepted")
	}
	input, err := bind(strings.TrimSuffix(base, "}") + `,"expected_attempt":0}`)
	if err != nil {
		t.Fatalf("explicit expected_attempt 0 rejected: %v", err)
	}
	if input.ExpectedAttempt == nil || *input.ExpectedAttempt != 0 {
		t.Fatalf("expected_attempt = %#v, want pointer to 0", input.ExpectedAttempt)
	}
}
