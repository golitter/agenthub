# Markdown 风格增强

本文记录聊天 Markdown 渲染的当前实现和维护入口，不再保留历史截图对比与长篇修改报告。具体代码以 `frontend/src/components/markdown/`、`frontend/src/index.css` 和相关 design 文档为准。

## 当前实现

| 层 | 文件 | 作用 |
|----|------|------|
| Markdown 入口 | `src/components/markdown/MarkdownRenderer.tsx` | 使用 `react-markdown` + `remark-gfm` 渲染消息正文 |
| 代码块 | `src/components/markdown/CodeBlock.tsx` | Shiki 语法高亮（语言别名归一化 + 白名单校验），高亮完成前 fallback 纯文本 + 行号 |
| 全局样式 | `src/index.css` | Tailwind / shadcn token 与 prose 样式覆盖 |
| 消息渲染 | `src/components/chat/MessageRenderer.tsx` | 在聊天消息中接入 Markdown 与卡片 |
| Block 解析 | `src/lib/block-reducer.ts` | 把 aka_yhy 结构化块路由到卡片组件 |

## 维护规则

1. Markdown 语义优先使用 `react-markdown` 组件覆盖，不在消息组件里手写 HTML 字符串。
2. 视觉 token 优先放在 `src/index.css`，保持和 shadcn / Tailwind 主题一致。
3. 代码高亮逻辑集中在 `CodeBlock.tsx`（语言归一化 + 高亮失败 fallback），避免在多个 Markdown 调用点重复实现高亮或行号逻辑。
4. 结构化 Agent 输出优先走卡片系统，不塞进 Markdown prose 样式里硬渲染。

## 相关文档

- `frontend/docs/design/12-markdown-rendering-and-preview.md`
- `frontend/docs/design/08-block-parser.md`
- `frontend/docs/design/06-cards.md`
- `frontend/docs/reference/visual-style-guide.md`
