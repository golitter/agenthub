package impl

import (
	"strings"
	"testing"
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
