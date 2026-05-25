package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// voidElements 是 HTML5 中不需要闭合标签的元素
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

var tagRe = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)[^>]*?(/?)>`)

// cmdHtmlRender 校验 HTML 语法并输出 html-render 卡片
// 用法: render html-render [content...]  或  echo "..." | render html-render
func cmdHtmlRender(args []string) {
	content := readContentOrStdin(args)
	if content == "" {
		fatal("html-render: 需要提供 HTML 内容（参数或 stdin）")
	}

	_, err := html.Parse(strings.NewReader(content))
	if err != nil {
		fatal("html-render: HTML 语法不合规: %v", err)
	}

	if err := validateTagBalance(content); err != nil {
		fatal("html-render: HTML 语法不合规: %v", err)
	}

	fmt.Printf("```%s\ntype: html-render\n%s\n```\n", blockMarker(), content)
}

// validateTagBalance 检查所有非 void 元素的标签是否闭合
func validateTagBalance(content string) error {
	// 检查裸 < 或 >：用 tagRe 替换掉所有合法标签后，不应再残留 < 或 >
	stripped := tagRe.ReplaceAllString(content, "")
	if strings.ContainsAny(stripped, "<>") {
		return fmt.Errorf("存在未闭合的尖括号")
	}

	var stack []string
	matches := tagRe.FindAllStringSubmatch(content, -1)

	if len(matches) == 0 {
		return fmt.Errorf("未包含合法 HTML 标签")
	}

	for _, m := range matches {
		isClose := m[1] == "/"
		tagName := strings.ToLower(m[2])
		isSelfClose := m[3] == "/"

		if isClose {
			if len(stack) == 0 {
				return fmt.Errorf("多余的闭合标签 </%s>", tagName)
			}
			top := stack[len(stack)-1]
			if top != tagName {
				return fmt.Errorf("标签不匹配: 期望 </%s>，实际 </%s>", top, tagName)
			}
			stack = stack[:len(stack)-1]
			continue
		}

		if voidElements[tagName] || isSelfClose {
			continue
		}
		stack = append(stack, tagName)
	}

	if len(stack) > 0 {
		return fmt.Errorf("未闭合的标签 <%s>", stack[len(stack)-1])
	}

	return nil
}

func readContentOrStdin(args []string) string {
	stat, _ := os.Stdin.Stat()
	if stat != nil && (stat.Mode()&os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err == nil && len(data) > 0 {
			return string(data)
		}
	}
	if len(args) > 0 {
		return strings.Join(args, " ")
	}
	return ""
}
