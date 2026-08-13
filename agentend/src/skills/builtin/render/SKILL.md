---
name: render
description: 输出技能工具。Agent 调用对应子命令生成 aka_yhy 富媒体卡片，覆盖 HTML 渲染、图片、附件、diff、预览五种类型。
---

## 概述

`render` 是输出技能工具，提供 5 个子命令，每个对应一种富媒体卡片类型。Agent 调用后，工具执行实际操作并输出格式化的 `aka_yhy` 代码块，Agent 直接将输出包含在回复中即可。HTML 内容会由平台上传到资源服务，回复中只保留 `resourceId`，不要复制 HTML 实体。

## 输出规则

所有 `render` 子命令输出的 `aka_yhy` 代码块都必须遵守以下规则：

- 代码块的结束 fence ` ``` ` 必须单独占一行，后面不能紧跟任何正文。
- 如果还要继续输出普通文本，例如“已合并到 task 分支，无冲突。”，必须在结束 fence 之后另起一行再写。
- 否则前端 block parser 不会把它识别成富媒体卡片，而会退化成普通文本显示。

正确示例：

````text
这里是正文说明。

```aka_yhy
type: html-render
resourceId: 6dd9a56e-40b9-4c1d-80bf-2fd19540db88
```

这里是代码块之后的普通文本。
````

错误示例：

````text
```aka_yhy
type: html-render
resourceId: 6dd9a56e-40b9-4c1d-80bf-2fd19540db88
```这里继续写普通文本
````

## 命令

### `html-render`

输出 HTML 渲染卡片。通过 stdin 管道传入内容。

```bash
# 短内容
echo '<div style="padding:20px">Hello</div>' | ./render html-render

# 长内容用 heredoc
cat <<'EOF' | ./render html-render
<h1>Title</h1>
<p>Body text here</p>
EOF
```

启用资源服务时输出示例：
```
```aka_yhy
type: html-render
resourceId: 6dd9a56e-40b9-4c1d-80bf-2fd19540db88
```
```

资源上传上下文不可用时命令会失败；不要伪造 `resourceId`，也不要把 HTML 粘贴到后续回复中。

#### HTML 视觉与实现基线

`html-render` 面向聊天中的独立视觉作品，不是把普通 Markdown 套进一个白色方框。生成 HTML 时遵守：

- 输出完整、自包含、响应式的 HTML；允许 `<!doctype html>`、`<style>` 和语义化标签，不依赖外部字体、脚本或随机图片服务。
- 软件界面使用克制的高品质无衬线字体栈；不要使用 emoji、霓虹外发光、紫蓝 AI 渐变、三等分模板卡片或大面积纯黑。
- 全页最多使用一个强调色；以留白、字号、字重和轻量分隔建立层级，卡片只在确实需要表达层级时使用。
- 优先非对称但清晰的 Grid 布局；低于 768px 必须退化为单列，不产生横向滚动。不要使用 `100vh`，需要满高时使用 `100dvh`。
- 动效只改变 `transform` 和 `opacity`，使用自定义 cubic-bezier，并通过 `prefers-reduced-motion` 提供无动画模式；不要在滚动容器上使用大面积 blur。
- 保证正文对比度、可读字号、明确的 loading / empty / error 状态。HTML 卡片运行在无脚本权限的 sandbox 中，不要用依赖 JavaScript 才能显示的核心内容。

### `image <path>`

验证图片存在后输出图片卡片。路径相对于 workspace 根目录。

```bash
./render image chart.png
```

### `attachment <path>`

验证文件存在后输出附件下载卡片。路径相对于 workspace 根目录。

```bash
./render attachment report.pdf
```

### `diff`

输出工作区变更卡片引用。前端收到此卡片后会通过 API 获取当前 workspace 的 diff 内容。

```bash
./render diff
```

### `preview <url>`

为已经由 AgentHub Preview 服务启动的 http(s) 地址输出预览卡片。该命令只生成卡片，不负责启动 HTTP 服务。

```bash
./render preview http://localhost:3928/index.html
```

输出示例：
```
```aka_yhy
type: preview
url: http://localhost:3928/index.html
```
```
