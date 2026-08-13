package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateHTMLAcceptsCompleteVisualDocument(t *testing.T) {
	content := `<!doctype html>
<!-- visual artifact -->
<html><head><style>.card::before { content: "<"; }</style></head>
<body><main class="card">Hello</main><script>if (1 < 2) { console.log(">") }</script></body></html>`
	if err := validateHTML(content); err != nil {
		t.Fatalf("valid HTML was rejected: %v", err)
	}
}

func TestValidateHTMLRejectsPlainText(t *testing.T) {
	if err := validateHTML("plain text only"); err == nil {
		t.Fatal("plain text unexpectedly accepted as HTML artifact")
	}
}

func TestResolveRegularWorkspaceFileRejectsEscapingSymlinkAndControlCharacters(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}

	if _, err := resolveRegularWorkspaceFile(root, "escape.txt"); err == nil {
		t.Fatal("symlink escape unexpectedly accepted")
	}
	if _, err := resolveRegularWorkspaceFile(root, "image.png\ntype: attachment"); err == nil {
		t.Fatal("control character unexpectedly accepted")
	}
}

func TestResolveRegularWorkspaceFileReturnsCanonicalRelativePath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "chart.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := resolveRegularWorkspaceFile(root, filepath.Join("assets", ".", "chart.png"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "assets/chart.png" {
		t.Fatalf("path = %q, want canonical relative path", got)
	}
}

func TestValidatePreviewURLRejectsMarkerInjection(t *testing.T) {
	if _, err := validatePreviewURL("http://localhost:3000\ntype: attachment"); err == nil {
		t.Fatal("newline injection unexpectedly accepted")
	}
	if _, err := validatePreviewURL("javascript:alert(1)"); err == nil {
		t.Fatal("non-http scheme unexpectedly accepted")
	}
	got, err := validatePreviewURL("  http://localhost:3928/index.html  ")
	if err != nil || !strings.HasPrefix(got, "http://localhost:3928/") {
		t.Fatalf("valid preview URL rejected: %q, %v", got, err)
	}
}
