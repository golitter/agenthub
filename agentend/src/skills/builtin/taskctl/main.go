package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	exitCodeIntegrationFailed   = 1
	exitCodeIntegrationConflict = 2
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	cmd := os.Args[1]
	// Phase 2 is a thin RPC client.  Resolve the operation and capability
	// before touching the executable path: an installed taskctl path is a
	// legacy compatibility detail, not an identity authority.
	if cmd == "merge" && phase2IntegrationEnabled() {
		operationID := strings.TrimSpace(os.Getenv("AGENTHUB_INTEGRATION_OPERATION_ID"))
		projection, err := executeIntegrationService(operationID, integrationRunID())
		if err != nil {
			fmt.Fprintf(os.Stderr, "IntegrationService 执行失败: %v\n", err)
			os.Exit(exitCodeIntegrationFailed)
		}
		exitIntegrationProjection(operationID, projection)
		return
	}

	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取可执行文件路径失败: %v\n", err)
		os.Exit(1)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解析符号链接失败: %v\n", err)
		os.Exit(1)
	}

	taskID, sessionID, sharedDir, _, err := parsePath(exePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "路径解析失败: %v\n", err)
		os.Exit(1)
	}

	switch cmd {

	case "help":
		printHelp()

	case "ls":
		cmdLs(sharedDir)

	case "summary":
		cmdSummary(sharedDir, sessionID)

	case "common-memory":
		cmdCommonMemory(sharedDir, os.Args[2:])

	case "sub-memory":
		cmdSubMemory(sharedDir, sessionID, os.Args[2:])

	case "write-sub-memory":
		cmdWriteSubMemory(sharedDir, sessionID)

	case "merge":
		cmdMerge(taskID, sessionID, sharedDir)

	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", cmd)
		printHelp()
		os.Exit(1)
	}
}

// ===================== 路径解析 =====================

func parsePath(exePath string) (taskID, sessionID, sharedDir, agentType string, err error) {
	current := filepath.Dir(exePath)

	skillsDir := filepath.Dir(current)
	agentTypeDir := filepath.Dir(skillsDir)
	agentType = agentTypeFromDir(filepath.Base(agentTypeDir))

	sessionDir := filepath.Dir(agentTypeDir)
	sessionID = filepath.Base(sessionDir)

	taskDir := filepath.Dir(sessionDir)
	taskID = filepath.Base(taskDir)

	worktreesDir := filepath.Dir(taskDir)

	if filepath.Base(worktreesDir) != "worktrees" {
		return "", "", "", "", fmt.Errorf("未找到 worktrees 目录")
	}

	sharedDir = filepath.Join(worktreesDir, taskID, "shared", ".agent")

	return
}

func agentTypeFromDir(dirName string) string {
	switch dirName {
	case ".claude":
		return "claude-code"
	case ".opencode":
		return "opencode"
	case ".pi":
		return "pi"
	default:
		return dirName
	}
}

// ===================== help =====================

func printHelp() {
	fmt.Println(`taskctl - Agent共享上下文工具（MVP）

命令:
  ls                          查看目录结构
  summary                     查看 config.yaml + plans
  common-memory [file]        查看公共记忆（指定文件名则只读单个文件）
  sub-memory [file]           查看当前Agent私有记忆（指定文件名则只读单个文件）
  write-sub-memory <file> [content]  写入私有记忆（无参数时从 stdin 读取内容）
  merge                       合并当前 agent 分支到 task 分支`)
}

// ===================== merge =====================

// IntegrationResult is the machine-readable fact written for every merge
// attempt. The Orchestrator trusts this file before considering Agent text.
type IntegrationResult struct {
	Version                int      `json:"version"`
	RunID                  string   `json:"run_id"`
	RootRunID              string   `json:"root_run_id,omitempty"`
	ParentRunID            string   `json:"parent_run_id,omitempty"`
	TaskID                 string   `json:"task_id,omitempty"`
	PlanTaskID             string   `json:"plan_task_id,omitempty"`
	IntegrationOperationID string   `json:"integration_operation_id,omitempty"`
	IntegrationScopeID     string   `json:"integration_scope_id,omitempty"`
	WorkspaceID            string   `json:"workspace_id,omitempty"`
	WorkspaceHandle        string   `json:"workspace_handle,omitempty"`
	SessionID              string   `json:"session_id"`
	Attempt                int      `json:"attempt"`
	Status                 string   `json:"status"`
	SourceBranch           string   `json:"source_branch"`
	SourceCommit           string   `json:"source_commit"`
	TargetBranch           string   `json:"target_branch"`
	TargetCommit           string   `json:"target_commit"`
	MergeBase              string   `json:"merge_base"`
	ConflictFiles          []string `json:"conflict_files,omitempty"`
	Aborted                bool     `json:"aborted"`
	ErrorCode              string   `json:"error_code,omitempty"`
	ErrorMessage           string   `json:"error_message,omitempty"`
	StartedAt              string   `json:"started_at"`
	FinishedAt             string   `json:"finished_at"`
	TargetCommitAfter      string   `json:"target_commit_after,omitempty"`
}

// IntegrationProjection is the deliberately small, non-Git response returned
// by the Phase 2 IntegrationService endpoint. Git refs remain available only
// through the service's audit projection.
type IntegrationProjection struct {
	IntegrationOperationID string   `json:"integration_operation_id"`
	PlanTaskID             string   `json:"plan_task_id"`
	RunID                  string   `json:"run_id"`
	Attempt                int      `json:"attempt"`
	Status                 string   `json:"status"`
	ConflictID             string   `json:"conflict_id,omitempty"`
	ConflictFiles          []string `json:"conflict_files,omitempty"`
	ErrorCode              string   `json:"error_code,omitempty"`
	ErrorMessage           string   `json:"error_message,omitempty"`
	FinishedAt             string   `json:"finished_at,omitempty"`
}

func cmdMerge(taskID, sessionID, sharedDir string) {
	startedAt := time.Now().UTC()
	agentBranch := fmt.Sprintf("agent/%s/%s", sessionID, taskID)
	taskBranch := fmt.Sprintf("task/%s", taskID)
	taskDir := filepath.Dir(filepath.Dir(sharedDir))
	agentWorktree := filepath.Join(taskDir, sessionID)
	taskBaseWorktree := filepath.Join(taskDir, "task-base")
	operationID := strings.TrimSpace(os.Getenv("AGENTHUB_INTEGRATION_OPERATION_ID"))
	planTaskID := strings.TrimSpace(os.Getenv("AGENTHUB_PLAN_TASK_ID"))
	if operationID != "" && planTaskID == "" {
		planTaskID = taskID
	}
	workspaceHandle := strings.TrimSpace(os.Getenv("AGENTHUB_WORKSPACE_HANDLE"))
	workspaceID := ""
	if operationID == "" {
		// V1 has no operation authority; retain the legacy diagnostic value.
		workspaceID = workspaceHandle
	}
	legacyTaskID := ""
	if operationID == "" {
		legacyTaskID = taskID
	}
	result := IntegrationResult{
		Version:                integrationResultVersion(),
		RunID:                  integrationRunID(),
		RootRunID:              os.Getenv("AGENTHUB_ROOT_RUN_ID"),
		ParentRunID:            os.Getenv("AGENTHUB_PARENT_RUN_ID"),
		TaskID:                 legacyTaskID,
		PlanTaskID:             planTaskID,
		IntegrationOperationID: operationID,
		IntegrationScopeID:     taskID,
		WorkspaceID:            workspaceID,
		WorkspaceHandle:        workspaceHandle,
		SessionID:              sessionID,
		Attempt:                integrationAttempt(),
		Status:                 "failed",
		SourceBranch:           agentBranch,
		TargetBranch:           taskBranch,
		StartedAt:              startedAt.Format(time.RFC3339Nano),
	}
	if operationID != "" && integrationServiceEnabled() {
		projection, err := executeIntegrationService(operationID, result.RunID)
		if err != nil {
			result.ErrorCode = "integration_service_execute_failed"
			result.ErrorMessage = err.Error()
			exitMerge(sharedDir, result)
		}
		exitIntegrationProjection(operationID, projection)
	}

	releaseLock, err := acquireIntegrationLock(sharedDir)
	if err != nil {
		result.ErrorCode = "integration_lock_failed"
		result.ErrorMessage = err.Error()
		exitMerge(sharedDir, result)
	}
	defer releaseLock()

	// 读取谱系事实失败也要落盘，不能让上层退回自然语言猜测。
	result.SourceCommit, _ = gitOutputAt(agentWorktree, "rev-parse", agentBranch)
	result.TargetCommit, _ = gitOutputAt(taskBaseWorktree, "rev-parse", taskBranch)
	result.MergeBase, _ = gitOutputAt(taskBaseWorktree, "merge-base", taskBranch, agentBranch)
	if result.SourceCommit == "" {
		result.ErrorCode = "source_missing"
		result.ErrorMessage = "agent source branch or commit is missing"
		exitMerge(sharedDir, result)
	}

	if _, err := os.Stat(taskBaseWorktree); err != nil {
		result.ErrorCode = "target_missing"
		result.ErrorMessage = fmt.Sprintf("task-base worktree 不存在: %v", err)
		exitMerge(sharedDir, result)
	}

	if dirty, err := gitOutputAt(taskBaseWorktree, "status", "--porcelain"); err != nil {
		result.ErrorCode = "dirty_target"
		result.ErrorMessage = err.Error()
		exitMerge(sharedDir, result)
	} else if strings.TrimSpace(dirty) != "" {
		result.ErrorCode = "dirty_target"
		result.ErrorMessage = "task-base 在合入前存在未提交改动"
		exitMerge(sharedDir, result)
	}

	// 检查是否有未提交的改动，并在合入前自动提交。
	out, err := gitOutputAt(agentWorktree, "status", "--porcelain")
	if err != nil {
		result.ErrorCode = "source_missing"
		result.ErrorMessage = err.Error()
		exitMerge(sharedDir, result)
	}
	if strings.TrimSpace(out) != "" {
		if err := runGitAt(agentWorktree, "add", "-A"); err != nil {
			result.ErrorCode = "commit_failed"
			result.ErrorMessage = err.Error()
			exitMerge(sharedDir, result)
		}
		if err := runGitAt(agentWorktree, "commit", "-m", "auto: merge前自动提交"); err != nil {
			result.ErrorCode = "commit_failed"
			result.ErrorMessage = err.Error()
			exitMerge(sharedDir, result)
		}
		result.SourceCommit, _ = gitOutputAt(agentWorktree, "rev-parse", agentBranch)
		result.MergeBase, _ = gitOutputAt(taskBaseWorktree, "merge-base", taskBranch, agentBranch)
	}

	// 在 task-base worktree 合并 agent 分支，避免当前 agent worktree 抢占 task 分支。
	if err := runGitAt(taskBaseWorktree, "merge", agentBranch); err != nil {
		conflictsText, _ := gitOutputAt(taskBaseWorktree, "diff", "--name-only", "--diff-filter=U")
		result.ConflictFiles = nonEmptyLines(conflictsText)
		abortErr := runGitAt(taskBaseWorktree, "merge", "--abort")
		result.Aborted = abortErr == nil
		if abortErr != nil {
			result.ErrorCode = "merge_aborted_failed"
			result.ErrorMessage = fmt.Sprintf("%v; merge --abort failed: %v", err, abortErr)
		} else if len(result.ConflictFiles) > 0 {
			result.Status = "conflict"
			result.ErrorCode = "merge_conflict"
			result.ErrorMessage = err.Error()
		} else {
			result.ErrorCode = "merge_failed"
			result.ErrorMessage = err.Error()
		}
		fmt.Fprintf(os.Stderr, "合并冲突: %s → %s 失败，已回退 task-base\n", agentBranch, taskBranch)
		if len(result.ConflictFiles) > 0 {
			fmt.Fprintf(os.Stderr, "冲突文件:\n%s\n", strings.Join(result.ConflictFiles, "\n"))
		}
		exitMerge(sharedDir, result)
	}

	result.Status = "merged"
	result.TargetCommitAfter, _ = gitOutputAt(taskBaseWorktree, "rev-parse", taskBranch)
	result.ErrorCode = ""
	result.ErrorMessage = ""
	if err := finishIntegrationResult(sharedDir, result); err != nil {
		fatal("写入 integration result 失败: %v", err)
	}
	fmt.Printf("merged to %s\n", taskBranch)
}

func acquireIntegrationLock(sharedDir string) (func(), error) {
	path := filepath.Join(sharedDir, "integration.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return func() {}, fmt.Errorf("创建集成锁失败: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return func() {}, fmt.Errorf("获取集成锁失败: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

func integrationRunID() string {
	if value := strings.TrimSpace(os.Getenv("AGENTHUB_RUN_ID")); value != "" {
		return value
	}
	return fmt.Sprintf("legacy-%d", time.Now().UnixNano())
}

func integrationResultVersion() int {
	if strings.TrimSpace(os.Getenv("AGENTHUB_INTEGRATION_OPERATION_ID")) != "" {
		return 2
	}
	return 1
}

func integrationServiceEnabled() bool {
	return strings.TrimSpace(os.Getenv("AGENTHUB_INTEGRATION_SERVICE_EXECUTE_ENABLED")) == "1"
}

func phase2IntegrationEnabled() bool {
	return strings.TrimSpace(os.Getenv("AGENTHUB_INTEGRATION_OPERATION_ID")) != "" && integrationServiceEnabled()
}

func executeIntegrationService(operationID, runID string) (IntegrationProjection, error) {
	if !validIntegrationOperationID(operationID) {
		return IntegrationProjection{}, fmt.Errorf("integration operation id must be a UUID")
	}
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("AGENTHUB_INTEGRATION_ENDPOINT")), "/")
	capability := strings.TrimSpace(os.Getenv("AGENTHUB_INTEGRATION_CAPABILITY"))
	if endpoint == "" || capability == "" {
		return IntegrationProjection{}, fmt.Errorf("integration service endpoint or capability is missing")
	}
	body, err := json.Marshal(map[string]string{"run_id": runID})
	if err != nil {
		return IntegrationProjection{}, err
	}
	request, err := http.NewRequest(
		http.MethodPost,
		endpoint+"/"+operationID+"/execute",
		bytes.NewReader(body),
	)
	if err != nil {
		return IntegrationProjection{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+capability)
	response, err := (&http.Client{Timeout: 60 * time.Second}).Do(request)
	if err != nil {
		return IntegrationProjection{}, err
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if readErr != nil {
		return IntegrationProjection{}, readErr
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(responseBody))
		if message == "" {
			message = response.Status
		}
		return IntegrationProjection{}, fmt.Errorf("integration service returned %s: %s", response.Status, message)
	}
	projection, err := decodeIntegrationProjection(responseBody)
	if err != nil {
		return IntegrationProjection{}, err
	}
	return projection, nil
}

func validIntegrationOperationID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
			continue
		}
		if !((char >= '0' && char <= '9') ||
			(char >= 'a' && char <= 'f') ||
			(char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func decodeIntegrationProjection(payload []byte) (IntegrationProjection, error) {
	var projection IntegrationProjection
	if err := json.Unmarshal(payload, &projection); err != nil {
		return IntegrationProjection{}, fmt.Errorf("decode integration service response: %w", err)
	}
	if projection.Status == "" {
		return IntegrationProjection{}, fmt.Errorf("integration service response missing status")
	}
	return projection, nil
}

func exitIntegrationProjection(operationID string, projection IntegrationProjection) {
	message := projection.ErrorMessage
	if message == "" {
		message = projection.Status
	}
	switch projection.Status {
	case "merged":
		fmt.Printf("merged operation %s\n", operationID)
		return
	case "conflict":
		fmt.Fprintf(os.Stderr, "integration conflict operation %s: %s\n", operationID, message)
		if len(projection.ConflictFiles) > 0 {
			fmt.Fprintf(os.Stderr, "冲突文件:\n%s\n", strings.Join(projection.ConflictFiles, "\n"))
		}
		os.Exit(exitCodeIntegrationConflict)
	default:
		fmt.Fprintf(os.Stderr, "integration %s operation %s: %s\n", projection.Status, operationID, message)
		os.Exit(exitCodeIntegrationFailed)
	}
}

func integrationAttempt() int {
	value := strings.TrimSpace(os.Getenv("AGENTHUB_INTEGRATION_ATTEMPT"))
	if value == "" {
		return 0
	}
	var attempt int
	if _, err := fmt.Sscanf(value, "%d", &attempt); err != nil || attempt < 0 {
		return 0
	}
	return attempt
}

func gitOutputAt(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return strings.TrimSpace(string(out)), nil
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func writeIntegrationResult(sharedDir string, result IntegrationResult) error {
	if result.RunID == "" {
		return fmt.Errorf("run_id is empty")
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(sharedDir, "integration-results", result.RunID+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return atomicWriteFile(path, payload, 0644)
}

func finishIntegrationResult(sharedDir string, result IntegrationResult) error {
	result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return writeIntegrationResult(sharedDir, result)
}

func exitMerge(sharedDir string, result IntegrationResult) {
	if err := finishIntegrationResult(sharedDir, result); err != nil {
		fatal("%s；写入 integration result 失败: %v", result.ErrorMessage, err)
	}
	fmt.Fprintf(os.Stderr, "%s\n", result.ErrorMessage)
	os.Exit(integrationExitCode(result.Status))
}

func integrationExitCode(status string) int {
	if status == "conflict" {
		return exitCodeIntegrationConflict
	}
	return exitCodeIntegrationFailed
}

func runGitAt(cwd string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// ===================== ls =====================

func cmdLs(sharedDir string) {
	entries, err := listTree(sharedDir, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取目录失败: %v\n", err)
		os.Exit(1)
	}

	if len(entries) == 0 {
		fmt.Println("(空)")
		return
	}

	for _, e := range entries {
		fmt.Println(e)
	}
}

func listTree(root, prefix string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var result []string

	for _, e := range entries {
		path := filepath.Join(root, e.Name())
		display := prefix + e.Name()

		if e.IsDir() {
			result = append(result, display+"/")

			sub, err := listTree(path, display+"/")
			if err == nil {
				result = append(result, sub...)
			}

			continue
		}

		result = append(result, display)
	}

	return result, nil
}

// ===================== summary =====================

func cmdSummary(sharedDir, sessionID string) {
	configPath := filepath.Join(sharedDir, "config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 config.yaml 失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== config.yaml ===\n%s\n\n", string(data))

	// 从 config.yaml 中找到属于当前 sessionID 的任务文件
	myFiles := myPlanFiles(data, sessionID)

	plansDir := filepath.Join(sharedDir, "plans")

	// overview.md 始终显示
	overviewPath := filepath.Join(plansDir, "overview.md")
	if od, err := os.ReadFile(overviewPath); err == nil {
		fmt.Printf("=== plans/overview.md ===\n%s\n\n", string(od))
	}

	// 只显示当前 agent 的 task 文件
	for _, f := range myFiles {
		path := filepath.Join(plansDir, f)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		fmt.Printf("=== plans/%s ===\n%s\n\n", f, string(data))
	}
}

// 从 config.yaml 内容中提取属于指定 sessionID 的 plan 文件名
func myPlanFiles(configData []byte, sessionID string) []string {
	var config struct {
		Tasks []struct {
			SessionID string `yaml:"session_id"`
			File      string `yaml:"file"`
		} `yaml:"tasks"`
	}

	if err := yaml.Unmarshal(configData, &config); err != nil {
		return nil
	}

	var files []string
	for _, t := range config.Tasks {
		if t.SessionID == sessionID {
			// file 格式: plans/task-001.md → 只取文件名
			files = append(files, filepath.Base(t.File))
		}
	}
	return files
}

// ===================== memory =====================

type FileContent struct {
	Name    string
	Content string
}

// 公共记忆
func cmdCommonMemory(sharedDir string, args []string) {
	memDir := filepath.Join(sharedDir, "memory", "common")

	if len(args) > 0 {
		filePath := filepath.Join(memDir, args[0])
		data, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(string(data))
		return
	}

	files, err := readFiles(memDir)
	if err != nil || len(files) == 0 {
		fmt.Println("(无公共记忆)")
		return
	}

	for _, f := range files {
		fmt.Printf("=== memory/%s ===\n%s\n\n", f.Name, f.Content)
	}
}

// 私有记忆（读）
func cmdSubMemory(sharedDir, sessionID string, args []string) {
	memDir := filepath.Join(sharedDir, "memory", sessionID)

	if len(args) > 0 {
		filePath := filepath.Join(memDir, args[0])
		data, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取文件失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(string(data))
		return
	}

	files, err := readFiles(memDir)
	if err != nil || len(files) == 0 {
		fmt.Println("(无私有记忆)")
		return
	}

	for _, f := range files {
		fmt.Printf("=== memory/%s ===\n%s\n\n", f.Name, f.Content)
	}
}

// 私有记忆（写）
func cmdWriteSubMemory(sharedDir, sessionID string) {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "用法: taskctl write-sub-memory <文件名> [内容]\n")
		os.Exit(1)
	}

	fileName := os.Args[2]
	content := readContent(os.Args[2:])
	if content == "" {
		fmt.Fprintf(os.Stderr, "错误: 未提供内容（通过参数或 stdin）\n")
		os.Exit(1)
	}

	memDir := filepath.Join(sharedDir, "memory", sessionID)

	err := os.MkdirAll(memDir, 0755)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建目录失败: %v\n", err)
		os.Exit(1)
	}

	filePath := filepath.Join(memDir, fileName)

	err = atomicWriteFile(filePath, []byte(content), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "写入失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("已写入私有记忆: %s\n", fileName)
}

// ===================== stdin =====================

func readContent(args []string) string {
	stat, _ := os.Stdin.Stat()
	if stat != nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err == nil && len(data) > 0 {
			return string(data)
		}
	}

	if len(args) >= 2 {
		return strings.Join(args[1:], " ")
	}

	return ""
}

// ===================== 原子写入 =====================

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("重命名失败: %w", err)
	}

	return nil
}

// ===================== 文件读取 =====================

func readFiles(dir string) ([]FileContent, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var result []FileContent

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		result = append(result, FileContent{
			Name:    e.Name(),
			Content: string(data),
		})
	}

	return result, nil
}
