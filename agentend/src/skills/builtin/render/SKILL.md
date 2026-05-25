---
name: render
description: 输出技能工具。Agent 调用对应子命令生成 aka_yhy 富媒体卡片，覆盖 HTML 渲染、图片、附件、diff、预览五种类型。
---

## 概述

`render` 是输出技能工具，提供 5 个子命令，每个对应一种富媒体卡片类型。Agent 调用后，工具执行实际操作并输出格式化的 `aka_yhy` 代码块，Agent 直接将输出包含在回复中即可。

## 命令

### `html-render [content...]`

输出 HTML 渲染卡片。支持 stdin 管道。

```bash
# 参数传入
./render html-render '<div style="padding:20px">Hello</div>'

# stdin 传入
echo '<h1>Title</h1>' | ./render html-render
```

输出示例：
```
```aka_yhy
type: html-render
<div style="padding:20px">Hello</div>
```
```

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

执行 `git diff HEAD`，输出工作区变更卡片。前端收到此卡片后会调 API 获取 diff 内容。

```bash
./render diff
```

无变更时不输出卡片。

### `preview [-port N]`

启动本地 HTTP 预览服务（serve 工作区目录），输出预览卡片。不指定端口则自动分配。重复调用时复用已有服务。

```bash
./render preview           # 自动分配端口
./render preview -port 8080
```

输出示例：
```
```aka_yhy
type: preview
url: http://localhost:3928/index.html
```
```
