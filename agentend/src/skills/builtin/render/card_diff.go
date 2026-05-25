package main

import "fmt"

// cmdDiff 输出 diff 卡片标记（前端通过 API 拉取实际 diff 内容）
func cmdDiff() {
	fmt.Printf("```%s\ntype: diff\n```\n", blockMarker())
}
