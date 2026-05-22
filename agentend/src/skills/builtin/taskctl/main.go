package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
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

	taskID, sessionID, sharedDir, err := parsePath(exePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "路径解析失败: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		printHelp()
		return
	}

	cmd := os.Args[1]

	switch cmd {

	case "help":
		printHelp()

	case "ls":
		cmdLs(sharedDir)

	case "summary":
		cmdSummary(sharedDir)

	case "common-memory":
		cmdCommonMemory(sharedDir, os.Args[2:])

	case "sub-memory":
		cmdSubMemory(sharedDir, sessionID, os.Args[2:])

	case "write-sub-memory":
		cmdWriteSubMemory(sharedDir, sessionID)

	case "merge":
		cmdMerge(taskID, sessionID)

	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", cmd)
		printHelp()
		os.Exit(1)
	}
}

// ===================== 路径解析 =====================

func parsePath(exePath string) (taskID, sessionID, sharedDir string, err error) {
	current := filepath.Dir(exePath)

	skillsDir := filepath.Dir(current)
	agentTypeDir := filepath.Dir(skillsDir)

	sessionDir := filepath.Dir(agentTypeDir)
	sessionID = filepath.Base(sessionDir)

	taskDir := filepath.Dir(sessionDir)
	taskID = filepath.Base(taskDir)

	worktreesDir := filepath.Dir(taskDir)

	if filepath.Base(worktreesDir) != "worktrees" {
		return "", "", "", fmt.Errorf("未找到 worktrees 目录")
	}

	sharedDir = filepath.Join(worktreesDir, taskID, "shared", ".agent")

	return
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

func cmdMerge(taskID, sessionID string) {
	agentBranch := fmt.Sprintf("agent/%s/%s", sessionID, taskID)
	taskBranch := fmt.Sprintf("task/%s", taskID)

	// 检查是否有未提交的改动
	out, _ := exec.Command("git", "status", "--porcelain").Output()
	if len(out) > 0 {
		if err := runGit("add", "-A"); err != nil {
			fatal("git add 失败: %v", err)
		}
		if err := runGit("commit", "-m", "auto: merge前自动提交"); err != nil {
			fatal("自动提交失败: %v", err)
		}
	}

	// 切到 task 分支
	if err := runGit("checkout", taskBranch); err != nil {
		fatal("切换到 %s 失败: %v", taskBranch, err)
	}

	// 合并 agent 分支
	if err := runGit("merge", agentBranch); err != nil {
		// 合并冲突：abort 并切回 agent 分支
		exec.Command("git", "merge", "--abort").Run()
		exec.Command("git", "checkout", agentBranch).Run()
		fmt.Fprintf(os.Stderr, "合并冲突: %s → %s 失败，已回退到 %s\n", agentBranch, taskBranch, agentBranch)
		os.Exit(1)
	}

	// 合并成功：切回 agent 分支
	if err := runGit("checkout", agentBranch); err != nil {
		fatal("切回 %s 失败: %v", agentBranch, err)
	}

	fmt.Printf("merged to %s\n", taskBranch)
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
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

func cmdSummary(sharedDir string) {
	configPath := filepath.Join(sharedDir, "config.yaml")

	data, err := os.ReadFile(configPath)
	if err == nil {
		fmt.Printf("=== config.yaml ===\n%s\n\n", string(data))
	}

	plansDir := filepath.Join(sharedDir, "plans")

	entries, err := os.ReadDir(plansDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取 plans 目录失败: %v\n", err)
		os.Exit(1)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		path := filepath.Join(plansDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		fmt.Printf("=== plans/%s ===\n%s\n\n", e.Name(), string(data))
	}
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