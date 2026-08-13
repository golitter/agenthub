package main

import (
	"fmt"
)

// cmdAttachment 验证文件存在并输出 attachment 卡片
// 用法: skill-output attachment <path>
func cmdAttachment(args []string) {
	if len(args) < 1 {
		fatal("attachment: 需要指定文件路径")
	}

	root, err := resolveWorktreeRoot()
	if err != nil {
		fatal("解析工作区失败: %v", err)
	}
	relPath, err := resolveRegularWorkspaceFile(root, args[0])
	if err != nil {
		fatal("attachment: %v", err)
	}

	fmt.Printf("```%s\ntype: attachment\npath: %s\n```\n", blockMarker(), relPath)
}
