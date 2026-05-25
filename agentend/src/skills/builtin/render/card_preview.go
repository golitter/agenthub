package main

import "fmt"

// cmdPreview 输出 preview 卡片，URL 由调用方提供
// 用法: render preview <url>
func cmdPreview(args []string) {
	if len(args) < 1 {
		fatal("preview: 需要提供预览 URL")
	}
	url := args[0]
	fmt.Printf("```%s\ntype: preview\nurl: %s\n```\n", blockMarker(), url)
}
