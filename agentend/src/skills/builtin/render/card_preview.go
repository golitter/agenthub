package main

import (
	"fmt"
	"net/url"
	"strings"
)

// cmdPreview 输出 preview 卡片，URL 由调用方提供
// 用法: render preview <url>
func cmdPreview(args []string) {
	if len(args) < 1 {
		fatal("preview: 需要提供预览 URL")
	}
	previewURL, err := validatePreviewURL(args[0])
	if err != nil {
		fatal("preview: %v", err)
	}
	fmt.Printf("```%s\ntype: preview\nurl: %s\n```\n", blockMarker(), previewURL)
}

func validatePreviewURL(rawURL string) (string, error) {
	previewURL := strings.TrimSpace(rawURL)
	parsed, err := url.ParseRequestURI(previewURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("URL 必须是有效的 http(s) 地址")
	}
	return previewURL, nil
}
