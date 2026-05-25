package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cmdAttachment 验证文件存在并输出 attachment 卡片
// 用法: skill-output attachment <path>
func cmdAttachment(args []string) {
	if len(args) < 1 {
		fatal("attachment: 需要指定文件路径")
	}

	relPath := args[0]
	root, err := resolveWorktreeRoot()
	if err != nil {
		fatal("解析工作区失败: %v", err)
	}

	fullPath := filepath.Join(root, relPath)
	cleanFull := filepath.Clean(fullPath)
	cleanRoot := filepath.Clean(root) + string(os.PathSeparator)
	if !strings.HasPrefix(cleanFull, cleanRoot) && cleanFull != filepath.Clean(root) {
		fatal("路径不允许: %s", relPath)
	}

	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		fatal("文件不存在: %s", relPath)
	}

	fmt.Printf("```%s\ntype: attachment\npath: %s\n```\n", blockMarker(), relPath)
}
