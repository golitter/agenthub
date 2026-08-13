package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/net/html"
)

const maxHTMLArtifactSize = 25 * 1024 * 1024

// cmdHtmlRender 校验 HTML 语法并输出 html-render 卡片
// 用法: render html-render [content...]  或  echo "..." | render html-render
func cmdHtmlRender(args []string) {
	content := readContentOrStdin(args)
	if content == "" {
		fatal("html-render: 需要提供 HTML 内容（参数或 stdin）")
	}
	if len([]byte(content)) > maxHTMLArtifactSize {
		fatal("html-render: HTML 内容超过 25MiB 限制")
	}

	if err := validateHTML(content); err != nil {
		fatal("html-render: HTML 语法不合规: %v", err)
	}

	resourceID, uploadErr := uploadHTMLArtifact(content)
	if uploadErr != nil {
		fatal("html-render: 上传资源失败: %v", uploadErr)
	}
	fmt.Printf("```%s\ntype: html-render\nresourceId: %s\n```\n", blockMarker(), resourceID)
}

// validateHTML uses the HTML5 tokenizer/parser rather than a tag regex.
// Regex balancing rejects valid documents containing doctypes, comments,
// quoted angle brackets, or raw script/style text—the exact constructs used
// by complete visual artifacts. HTML5 deliberately defines recovery for
// omitted optional closing tags, so tokenizer correctness is the useful gate.
func validateHTML(content string) error {
	tokenizer := html.NewTokenizer(strings.NewReader(content))
	hasElement := false
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case html.StartTagToken, html.SelfClosingTagToken:
			hasElement = true
		case html.ErrorToken:
			if tokenizer.Err() != io.EOF {
				return tokenizer.Err()
			}
			if !hasElement {
				return fmt.Errorf("未包含合法 HTML 元素")
			}
			_, err := html.Parse(strings.NewReader(content))
			return err
		}
	}
}

func readContentOrStdin(args []string) string {
	const maxInputSize = maxHTMLArtifactSize + 1
	stat, _ := os.Stdin.Stat()
	if stat != nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, maxInputSize))
		if err == nil && len(data) > 0 {
			return string(data)
		}
	}
	if len(args) > 0 {
		content := strings.Join(args, " ")
		if len([]byte(content)) > maxInputSize {
			return content[:maxInputSize]
		}
		return content
	}
	return ""
}
