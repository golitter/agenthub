package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ===================== parsePath =====================

func TestParsePath(t *testing.T) {
	exePath := "/abs/worktrees/task-123/sess-abc/.claude/skills/taskctl/exe"
	taskID, sessionID, sharedDir, _, err := parsePath(exePath)
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
	taskID, sessionID, sharedDir, _, err := parsePath(exePath)
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

func TestParsePathPi(t *testing.T) {
	exePath := "/abs/worktrees/task-789/sess-ghi/.pi/skills/taskctl/exe"
	_, _, _, agentType, err := parsePath(exePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentType != "pi" {
		t.Fatalf("agentType = %q, want %q", agentType, "pi")
	}
}

func TestParsePathInvalid(t *testing.T) {
	exePath := "/usr/local/bin/taskctl/exe"
	_, _, _, _, err := parsePath(exePath)
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

func TestPhase2IntegrationEnabledDoesNotDependOnInstallPath(t *testing.T) {
	t.Setenv("AGENTHUB_INTEGRATION_OPERATION_ID", "operation-1")
	t.Setenv("AGENTHUB_INTEGRATION_SERVICE_EXECUTE_ENABLED", "1")
	if !phase2IntegrationEnabled() {
		t.Fatal("phase 2 integration should be enabled from opaque operation identity")
	}

	t.Setenv("AGENTHUB_INTEGRATION_SERVICE_EXECUTE_ENABLED", "0")
	if phase2IntegrationEnabled() {
		t.Fatal("disabled IntegrationService should not select phase 2")
	}
}

func TestValidIntegrationOperationID(t *testing.T) {
	if !validIntegrationOperationID("11111111-1111-4111-8111-111111111111") {
		t.Fatal("expected UUID operation id to be valid")
	}
	for _, value := range []string{
		"operation-1",
		"11111111/../../secret",
		"11111111-1111-4111-8111-11111111111z",
	} {
		if validIntegrationOperationID(value) {
			t.Fatalf("expected invalid operation id %q", value)
		}
	}
}

func TestIntegrationExitCode(t *testing.T) {
	if got := integrationExitCode("conflict"); got != exitCodeIntegrationConflict {
		t.Fatalf("conflict exit code = %d, want %d", got, exitCodeIntegrationConflict)
	}
	if got := integrationExitCode("failed"); got != exitCodeIntegrationFailed {
		t.Fatalf("failed exit code = %d, want %d", got, exitCodeIntegrationFailed)
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

	configYaml := "task_id: test-001\ntasks:\n- task_id: task-001\n  session_id: sess-abc\n  file: plans/task-001.md\n- task_id: task-002\n  session_id: sess-def\n  file: plans/task-002.md\n"
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYaml), 0644)

	plansDir := filepath.Join(tmpDir, "plans")
	os.MkdirAll(plansDir, 0755)
	os.WriteFile(filepath.Join(plansDir, "overview.md"), []byte("overview text"), 0644)
	os.WriteFile(filepath.Join(plansDir, "task-001.md"), []byte("task for abc"), 0644)
	os.WriteFile(filepath.Join(plansDir, "task-002.md"), []byte("task for def"), 0644)

	output := captureOutput(func() { cmdSummary(tmpDir, "sess-abc") })
	if !strings.Contains(output, "=== config.yaml ===") {
		t.Errorf("expected output to contain '=== config.yaml ===', got %q", output)
	}
	if !strings.Contains(output, "=== plans/overview.md ===") {
		t.Errorf("expected output to contain '=== plans/overview.md ===', got %q", output)
	}
	if !strings.Contains(output, "=== plans/task-001.md ===") {
		t.Errorf("expected output to contain '=== plans/task-001.md ===', got %q", output)
	}
	if strings.Contains(output, "=== plans/task-002.md ===") {
		t.Errorf("sess-abc should NOT see task-002.md, got %q", output)
	}
}

func TestCmdSummaryOtherSession(t *testing.T) {
	tmpDir := t.TempDir()

	configYaml := "task_id: test-001\ntasks:\n- task_id: task-001\n  session_id: sess-abc\n  file: plans/task-001.md\n- task_id: task-002\n  session_id: sess-def\n  file: plans/task-002.md\n"
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYaml), 0644)

	plansDir := filepath.Join(tmpDir, "plans")
	os.MkdirAll(plansDir, 0755)
	os.WriteFile(filepath.Join(plansDir, "overview.md"), []byte("overview"), 0644)
	os.WriteFile(filepath.Join(plansDir, "task-001.md"), []byte("task abc"), 0644)
	os.WriteFile(filepath.Join(plansDir, "task-002.md"), []byte("task def"), 0644)

	output := captureOutput(func() { cmdSummary(tmpDir, "sess-def") })
	if !strings.Contains(output, "=== plans/task-002.md ===") {
		t.Errorf("expected output to contain '=== plans/task-002.md ===', got %q", output)
	}
	if strings.Contains(output, "=== plans/task-001.md ===") {
		t.Errorf("sess-def should NOT see task-001.md, got %q", output)
	}
}

func TestCmdSummaryNoMatchingSession(t *testing.T) {
	tmpDir := t.TempDir()

	configYaml := "task_id: test-001\ntasks:\n- task_id: task-001\n  session_id: sess-abc\n  file: plans/task-001.md\n"
	os.WriteFile(filepath.Join(tmpDir, "config.yaml"), []byte(configYaml), 0644)

	plansDir := filepath.Join(tmpDir, "plans")
	os.MkdirAll(plansDir, 0755)
	os.WriteFile(filepath.Join(plansDir, "overview.md"), []byte("overview"), 0644)
	os.WriteFile(filepath.Join(plansDir, "task-001.md"), []byte("task abc"), 0644)

	output := captureOutput(func() { cmdSummary(tmpDir, "unknown-session") })
	if !strings.Contains(output, "=== config.yaml ===") {
		t.Errorf("expected config.yaml in output, got %q", output)
	}
	if !strings.Contains(output, "=== plans/overview.md ===") {
		t.Errorf("expected overview.md in output, got %q", output)
	}
	if strings.Contains(output, "=== plans/task-001.md ===") {
		t.Errorf("unknown session should NOT see any task, got %q", output)
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

func TestWriteIntegrationResultIsRunAddressedAndAtomic(t *testing.T) {
	tmpDir := t.TempDir()
	result := IntegrationResult{
		Version:       1,
		RunID:         "run-123",
		RootRunID:     "root-1",
		ParentRunID:   "parent-1",
		TaskID:        "task-1",
		SessionID:     "session-1",
		Attempt:       1,
		Status:        "conflict",
		SourceBranch:  "agent/session-1/task-1",
		SourceCommit:  "source-sha",
		TargetBranch:  "task/task-1",
		TargetCommit:  "target-sha",
		MergeBase:     "base-sha",
		ConflictFiles: []string{"1.md"},
		Aborted:       true,
		ErrorCode:     "merge_conflict",
	}

	if err := writeIntegrationResult(tmpDir, result); err != nil {
		t.Fatalf("writeIntegrationResult() error = %v", err)
	}
	path := filepath.Join(tmpDir, "integration-results", "run-123.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	var decoded IntegrationResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if decoded.RunID != result.RunID || decoded.Status != "conflict" || !decoded.Aborted {
		t.Fatalf("decoded result = %#v", decoded)
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "integration-results", ".tmp")); !os.IsNotExist(err) {
		t.Fatalf("unexpected fixed temp path result: %v", err)
	}
}

func TestIntegrationResultVersionUsesOperationIdentity(t *testing.T) {
	t.Setenv("AGENTHUB_INTEGRATION_OPERATION_ID", "operation-1")
	if got := integrationResultVersion(); got != 2 {
		t.Fatalf("integrationResultVersion() = %d, want 2", got)
	}
	t.Setenv("AGENTHUB_INTEGRATION_OPERATION_ID", "")
	if got := integrationResultVersion(); got != 1 {
		t.Fatalf("integrationResultVersion() = %d, want 1", got)
	}
}

func TestDecodeIntegrationProjection(t *testing.T) {
	payload := []byte(`{"integration_operation_id":"op-1","plan_task_id":"task-001","run_id":"run-1","attempt":0,"status":"conflict","conflict_id":"conflict-1","conflict_files":["README.md"],"error_code":"merge_conflict"}`)
	projection, err := decodeIntegrationProjection(payload)
	if err != nil {
		t.Fatalf("decodeIntegrationProjection() error = %v", err)
	}
	if projection.Status != "conflict" || projection.ConflictID != "conflict-1" {
		t.Fatalf("projection = %#v", projection)
	}
}

func TestDecodeIntegrationProjectionRequiresStatus(t *testing.T) {
	if _, err := decodeIntegrationProjection([]byte(`{"operation_id":"op-1"}`)); err == nil {
		t.Fatal("expected missing status error")
	}
}

func TestNonEmptyLinesRemovesBlankLines(t *testing.T) {
	got := nonEmptyLines("\n1.md\n\n  src/main.go  \n")
	want := []string{"1.md", "src/main.go"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("nonEmptyLines() = %#v, want %#v", got, want)
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
