package main

import (
	"fmt"
)

// cmdImage 验证图片文件存在并输出 image 卡片
// 用法: skill-output image <path>
func cmdImage(args []string) {
	if len(args) < 1 {
		fatal("image: 需要指定图片路径")
	}

	root, err := resolveWorktreeRoot()
	if err != nil {
		fatal("解析工作区失败: %v", err)
	}
	relPath, err := resolveRegularWorkspaceFile(root, args[0])
	if err != nil {
		fatal("image: %v", err)
	}

	fmt.Printf("```%s\ntype: image\npath: %s\n```\n", blockMarker(), relPath)
}
