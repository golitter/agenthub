package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

	_ = taskID

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
		cmdCommonMemory(sharedDir)

	case "sub-memory":
		cmdSubMemory(sharedDir, sessionID)

	case "write-sub-memory":
		cmdWriteSubMemory(sharedDir, sessionID)

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
  ls                    查看目录结构
  summary               查看 config.yaml + plans
  common-memory         查看公共记忆
  sub-memory            查看当前Agent私有记忆
  write-sub-memory      写入私有记忆`)
}

// ===================== ls =====================

func cmdLs(sharedDir string) {
	entries, err := listTree(sharedDir, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取目录失败: %v\n", err)
		return
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
		fmt.Println("(无 plans)")
		return
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
func cmdCommonMemory(sharedDir string) {
	memDir := filepath.Join(sharedDir, "memory", "common")

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
func cmdSubMemory(sharedDir, sessionID string) {
	memDir := filepath.Join(sharedDir, "memory", sessionID)

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
	if len(os.Args) < 4 {
		fmt.Println("用法: taskctl write-sub-memory <文件名> <内容>")
		return
	}

	fileName := os.Args[2]
	content := os.Args[3]

	memDir := filepath.Join(sharedDir, "memory", sessionID)

	err := os.MkdirAll(memDir, 0755)
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建目录失败: %v\n", err)
		os.Exit(1)
	}

	filePath := filepath.Join(memDir, fileName)

	err = os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "写入失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("已写入私有记忆: %s\n", fileName)
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