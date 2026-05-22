package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ===================== parsePath =====================

func TestParsePath(t *testing.T) {
	exePath := "/abs/worktrees/task-123/sess-abc/.claude/skills/taskctl/exe"
	taskID, sessionID, sharedDir, err := parsePath(exePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskID != "task-123" {
		t.Errorf("taskID = %q, want %q", taskID, "task-123")
	}
	if sessionID != "sess-abc" {
		t.Errorf("sessionID = %q, want %q", sessionID, "sess-abc")
	}
	wantShared := filepath.Join("/abs/worktrees", "task-123", "shared", ".agent")
	if sharedDir != wantShared {
		t.Errorf("sharedDir = %q, want %q", sharedDir, wantShared)
	}
}

func TestParsePathOpenCode(t *testing.T) {
	exePath := "/abs/worktrees/task-456/sess-def/.opencode/skills/taskctl/exe"
	taskID, sessionID, sharedDir, err := parsePath(exePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskID != "task-456" {
		t.Errorf("taskID = %q, want %q", taskID, "task-456")
	}
	if sessionID != "sess-def" {
		t.Errorf("sessionID = %q, want %q", sessionID, "sess-def")
	}
	wantShared := filepath.Join("/abs/worktrees", "task-456", "shared", ".agent")
	if sharedDir != wantShared {
		t.Errorf("sharedDir = %q, want %q", sharedDir, wantShared)
	}
}

func TestParsePathInvalid(t *testing.T) {
	exePath := "/usr/local/bin/taskctl/exe"
	_, _, _, err := parsePath(exePath)
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

// ===================== listTree =====================

func TestListTree(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "sub"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "sub", "b.txt"), []byte("b"), 0644)

	entries, err := listTree(tmpDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"a.txt", "sub/", "sub/b.txt"}
	if len(entries) != len(expected) {
		t.Fatalf("got %d entries, want %d", len(entries), len(expected))
	}
	for i, e := range expected {
		if entries[i] != e {
			t.Errorf("entry[%d] = %q, want %q", i, entries[i], e)
		}
	}
}

func TestListTreeEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	entries, err := listTree(tmpDir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

func TestListTreeNonExistent(t *testing.T) {
	_, err := listTree("/nonexistent/path", "")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestListTreeWithPrefix(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "dir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "f.txt"), []byte("f"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "dir", "g.txt"), []byte("g"), 0644)

	entries, err := listTree(tmpDir, "prefix/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"prefix/dir/", "prefix/dir/g.txt", "prefix/f.txt"}
	if len(entries) != len(expected) {
		t.Fatalf("got %d entries, want %d", len(entries), len(expected))
	}
	for i, e := range expected {
		if entries[i] != e {
			t.Errorf("entry[%d] = %q, want %q", i, entries[i], e)
		}
	}
}

// ===================== cmdLs =====================

func TestCmdLs(t *testing.T) {
	tmpDir := t.TempDir()
	memDir := filepath.Join(tmpDir, "memory", "common")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "notes.md"), []byte("hello"), 0644)

	output := captureOutput(func() { cmdLs(tmpDir) })
	if !strings.Contains(output, "memory/") {
		t.Errorf("expected output to contain 'memory/', got %q", output)
	}
}

func TestCmdLsEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	output := captureOutput(func() { cmdLs(tmpDir) })
	if !strings.Contains(output, "(空)") {
		t.Errorf("expected '(空)' for empty dir, got %q", output)
	}
}

// ===================== cmdSummary =====================

func TestCmdSummary(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte("name: test\n"), 0644)
	plansDir := filepath.Join(tmpDir, "plans")
	os.MkdirAll(plansDir, 0755)
	os.WriteFile(filepath.Join(plansDir, "plan-1.md"), []byte("step 1"), 0644)

	output := captureOutput(func() { cmdSummary(tmpDir) })
	if !strings.Contains(output, "config.yaml") {
		t.Errorf("expected output to contain 'config.yaml', got %q", output)
	}
	if !strings.Contains(output, "plan-1.md") {
		t.Errorf("expected output to contain 'plan-1.md', got %q", output)
	}
}

// ===================== cmdCommonMemory =====================

func TestCmdCommonMemory(t *testing.T) {
	tmpDir := t.TempDir()
	memDir := filepath.Join(tmpDir, "memory", "common")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "a.md"), []byte("alpha"), 0644)
	os.WriteFile(filepath.Join(memDir, "b.md"), []byte("beta"), 0644)

	output := captureOutput(func() { cmdCommonMemory(tmpDir, nil) })
	if !strings.Contains(output, "alpha") || !strings.Contains(output, "beta") {
		t.Errorf("expected both alpha and beta in output, got %q", output)
	}
}

func TestCmdCommonMemoryEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	output := captureOutput(func() { cmdCommonMemory(tmpDir, nil) })
	if !strings.Contains(output, "无公共记忆") {
		t.Errorf("expected '(无公共记忆)', got %q", output)
	}
}

func TestCmdCommonMemorySingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	memDir := filepath.Join(tmpDir, "memory", "common")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "notes.md"), []byte("hello world"), 0644)
	os.WriteFile(filepath.Join(memDir, "other.md"), []byte("other content"), 0644)

	output := captureOutput(func() { cmdCommonMemory(tmpDir, []string{"notes.md"}) })
	if !strings.Contains(output, "hello world") {
		t.Errorf("expected 'hello world', got %q", output)
	}
	if strings.Contains(output, "other content") {
		t.Errorf("should not contain other file content, got %q", output)
	}
}

// ===================== cmdSubMemory =====================

func TestCmdSubMemory(t *testing.T) {
	tmpDir := t.TempDir()
	memDir := filepath.Join(tmpDir, "memory", "sess-abc")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "note.md"), []byte("my notes"), 0644)

	output := captureOutput(func() { cmdSubMemory(tmpDir, "sess-abc", nil) })
	if !strings.Contains(output, "my notes") {
		t.Errorf("expected 'my notes' in output, got %q", output)
	}
}

func TestCmdSubMemoryEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	output := captureOutput(func() { cmdSubMemory(tmpDir, "unknown-session", nil) })
	if !strings.Contains(output, "无私有记忆") {
		t.Errorf("expected '(无私有记忆)', got %q", output)
	}
}

func TestCmdSubMemorySingleFile(t *testing.T) {
	tmpDir := t.TempDir()
	memDir := filepath.Join(tmpDir, "memory", "sess-abc")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "log.md"), []byte("session log"), 0644)
	os.WriteFile(filepath.Join(memDir, "draft.md"), []byte("draft content"), 0644)

	output := captureOutput(func() { cmdSubMemory(tmpDir, "sess-abc", []string{"log.md"}) })
	if !strings.Contains(output, "session log") {
		t.Errorf("expected 'session log', got %q", output)
	}
	if strings.Contains(output, "draft content") {
		t.Errorf("should not contain other file content, got %q", output)
	}
}

// ===================== cmdWriteSubMemory =====================

func TestCmdWriteSubMemory(t *testing.T) {
	tmpDir := t.TempDir()

	oldArgs := os.Args
	os.Args = []string{"taskctl", "write-sub-memory", "log.md", "hello", "world"}
	defer func() { os.Args = oldArgs }()

	output := captureOutput(func() { cmdWriteSubMemory(tmpDir, "sess-abc") })

	filePath := filepath.Join(tmpDir, "memory", "sess-abc", "log.md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("file content = %q, want %q", string(data), "hello world")
	}
	if !strings.Contains(output, "已写入私有记忆") {
		t.Errorf("expected success message, got %q", output)
	}
}

// ===================== atomicWriteFile =====================

func TestAtomicWriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test.txt")

	err := atomicWriteFile(target, []byte("hello"), 0644)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("file not found: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q, want %q", string(data), "hello")
	}
}

func TestAtomicWriteFileOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test.txt")

	os.WriteFile(target, []byte("old"), 0644)

	err := atomicWriteFile(target, []byte("new"), 0644)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("file not found: %v", err)
	}
	if string(data) != "new" {
		t.Errorf("content = %q, want %q", string(data), "new")
	}
}

func TestAtomicWriteFileNoTempLeftover(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test.txt")

	atomicWriteFile(target, []byte("data"), 0644)

	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file leftover: %s", e.Name())
		}
	}
}

// ===================== readContent =====================

func TestReadContentFromArgs(t *testing.T) {
	content := readContent([]string{"log.md", "hello", "world"})
	if content != "hello world" {
		t.Errorf("content = %q, want %q", content, "hello world")
	}
}

func TestReadContentEmpty(t *testing.T) {
	content := readContent([]string{"log.md"})
	if content != "" {
		t.Errorf("content = %q, want empty", content)
	}
}

// ===================== readFiles =====================

func TestReadFiles(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "c.txt"), []byte("gamma"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("alpha"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("beta"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "subdir"), 0755)

	files, err := readFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3", len(files))
	}
	if files[0].Name != "a.txt" || files[1].Name != "b.txt" || files[2].Name != "c.txt" {
		t.Errorf("files not sorted: %v", []string{files[0].Name, files[1].Name, files[2].Name})
	}
	if files[0].Content != "alpha" {
		t.Errorf("files[0].Content = %q, want %q", files[0].Content, "alpha")
	}
}

func TestReadFilesEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	files, err := readFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("got %d files, want 0", len(files))
	}
}

func TestReadFilesNonExistent(t *testing.T) {
	_, err := readFiles("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

// ===================== printHelp =====================

func TestPrintHelp(t *testing.T) {
	output := captureOutput(func() { printHelp() })
	if !strings.Contains(output, "taskctl") {
		t.Errorf("expected help to contain 'taskctl', got %q", output)
	}
	for _, cmd := range []string{"ls", "summary", "common-memory", "sub-memory", "write-sub-memory"} {
		if !strings.Contains(output, cmd) {
			t.Errorf("expected help to contain %q, got %q", cmd, output)
		}
	}
}

// ===================== captureOutput helper =====================

func captureOutput(fn func()) string {
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout = w
	os.Stderr = w

	fn()

	w.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}
