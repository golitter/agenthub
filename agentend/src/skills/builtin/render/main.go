package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		return
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "help":
		printHelp()
	case "html-render":
		cmdHtmlRender(args)
	case "image":
		cmdImage(args)
	case "attachment":
		cmdAttachment(args)
	case "diff":
		cmdDiff()
	case "preview":
		cmdPreview(args)
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n", cmd)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Print(`render - 输出技能工具，生成 aka_yhy 富媒体卡片

` + "命令:\n" +
		`  help                         打印帮助
  html-render [content]        输出 HTML 渲染卡片（支持 stdin）
  image <path>                 输出图片卡片
  attachment <path>            输出附件下载卡片
  diff                         输出工作区变更卡片
  preview <url>                输出预览卡片（URL 由预览服务提供）

` + "注意: html-render 的 HTML 内容请用引号包裹，避免 shell 解析 < > 等特殊字符。\n" +
		"  例: ./render html-render '<div>hello</div>'\n" +
		"  或: echo '<div>hello</div>' | ./render html-render\n")
}

// resolveWorktreeRoot 从可执行文件位置向上查找 git worktree 根目录
func resolveWorktreeRoot() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("解析符号链接失败: %w", err)
	}

	dir := filepath.Dir(exePath)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到 git 仓库根目录")
		}
		dir = parent
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// blockMarker 返回 aka_yhy 标记
func blockMarker() string {
	return "aka_yhy"
}
