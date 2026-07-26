# Theme — 主题与样式系统

## 实现了什么

基于 CSS 变量的 Light/Dark 双主题系统（浅色为 Teal 品牌色、暖白背景；暗色为亮 Teal 品牌色、深绿黑背景），通过 Tailwind CSS 4 的 `@theme inline` 机制将 CSS 变量映射为 Tailwind 工具类。所有颜色通过 CSS 自定义属性控制，组件中不硬编码颜色值。`@tailwindcss/typography` 插件通过 `@plugin` 指令加载，提供 `prose` 排版基础，并额外通过 `--prose-*` 变量覆盖暗色 prose 配色。

## 怎么实现的

### 全局样式入口 (`src/index.css`)

顶层引入 Tailwind、shadcn 主题、Geist 字体，通过 `@theme inline` 将 CSS 变量注册为 Tailwind 可用的颜色 token：

```css
@import "tailwindcss";
@plugin "@tailwindcss/typography";
@import "tw-animate-css";
@import "shadcn/tailwind.css";
@import "@fontsource-variable/geist";

@custom-variant dark (&:is(.dark *));

@theme inline {
    --font-heading: var(--font-sans);
    --font-sans: 'Geist Variable', sans-serif;
    --color-background: var(--background);
    --color-foreground: var(--foreground);
    --color-card: var(--card);
    --color-primary: var(--primary);
    --color-muted-foreground: var(--muted-foreground);
    --color-border: var(--border);
    --color-tertiary: var(--text-tertiary);
    --color-code-bg: var(--code-bg);
    --color-agent-claude: var(--agent-claude);
    --color-agent-opencode: var(--agent-opencode);
    --color-agent-orchestrator: var(--agent-orchestrator);
    --color-primary-soft: var(--primary-soft);
    --color-primary-border: var(--primary-border);
    /* 自定义背景色阶 → bg-bg-canvas / bg-bg-hover / bg-bg-active 等工具类 */
    --color-bg-canvas: var(--bg-canvas);
    --color-bg-sidebar: var(--bg-sidebar);
    --color-bg-card: var(--bg-card);
    --color-bg-hover: var(--bg-hover);
    --color-bg-active: var(--bg-active);
    /* 文字色阶 */
    --color-text-primary: var(--text-primary);
    --color-text-secondary: var(--text-secondary);
    /* ... 更多映射（diff / shadow-popup / chart 等） */
}
```

`:root` 定义浅色模式变量（暖白背景 + Teal 品牌色 `#0F766E`），`.dark` 覆盖为暗色模式变量（深绿黑背景 + 亮 Teal 品牌色 `#5EEAD4`）：

```css
:root {
    --background: #FBFCFB;
    --foreground: #17211F;
    --card: #FFFFFF;
    --primary: #0F766E;
    --primary-foreground: #F7FFFC;
    --ring: #0F766E;
    --color-brand: #0F766E;
    --border: #DDE6E2;
    --muted-foreground: #64716E;
    /* ... */
}

.dark {
    --background: #0B1110;
    --foreground: #E6EFEC;
    --card: #141C1A;
    --popover: #192321;
    --primary: #5EEAD4;
    --primary-foreground: #05201C;
    --destructive: #F97066;
    --border: rgba(255,255,255,0.06);
    --sidebar: #101715;
    --text-tertiary: #697773;
    /* ... */
}
```

### 暗色模式色彩体系

基础背景色：

| 变量 | 值 | 用途 |
|------|------|------|
| `--background` | `#0B1110` | 主画布背景 |
| `--sidebar` | `#101715` | 侧栏背景 |
| `--card` | `#141C1A` | 卡片 / Agent 消息气泡背景 |
| `--accent` | `#1E2A27` | hover 背景 / 搜索框背景 / 表头背景 |
| `--popover` | `#192321` | 弹出层背景 |
| `--bg-hover` | `#1E2A27` | 自定义 hover 背景（对应工具类 `bg-bg-hover`） |
| `--bg-active` | `#273531` | 自定义 active 背景 |

文本色：

| 变量 | 值 | 用途 |
|------|------|------|
| `--foreground` | `#E6EFEC` | 主文本 |
| `--muted-foreground` | `#92A09C` | 次要文本（时间、描述等） |
| `--text-tertiary` | `#697773` | 占位符、辅助信息 |

功能色与品牌色（**暗色 `.dark` 下的值**；浅色主题语义色截然不同，见下表）：

| 变量 | 暗色值 | 浅色值 | 用途 |
|------|------|------|------|
| `--primary` | `#5EEAD4` | `#0F766E` | 品牌 / 主色调（亮 Teal / 深 Teal） |
| `--destructive` | `#F97066` | `#B42318` | 错误 |
| `--border` | `rgba(255,255,255,0.06)` | `rgba(15,23,22,0.08)` | 统一边框色 |
| `--color-success` | `#4ADE80` | `#218358` | 成功 / 就绪 |
| `--color-warning` | `#FBBF24` | `#B7791F` | 警告 / 运行中 |
| `--color-error` | `#F97066` | `#B42318` | 错误（与 `--destructive` 同源） |

### Agent 专属色

`.dark` 块中定义的 Agent 品牌色，通过 `@theme inline` 映射为 Tailwind 工具类。注意暗色模式下 `--agent-codex` 与 `--primary` 同为亮 Teal `#5EEAD4`：

```css
.dark {
    --agent-claude: #DA7756;
    --agent-opencode: #10B981;
    --agent-orchestrator: #EAB308;
    --agent-codex: #5EEAD4;
    --primary-soft: rgba(94, 234, 212, 0.10);
    --primary-border: rgba(94, 234, 212, 0.20);
    --code-bg: #0E1715;
    --color-danger-bg: rgba(249, 112, 102, 0.10);
    --diff-insert-bg: rgba(74, 222, 128, 0.08);
    --diff-delete-bg: rgba(249, 112, 102, 0.08);
    --diff-insert-bg-strong: rgba(74, 222, 128, 0.10);
    --diff-delete-bg-strong: rgba(249, 112, 102, 0.10);
}
```

浅色模式下 `--agent-codex` 同样跟随主色（`#0F766E`）。这些颜色用于 `AgentAvatar` 背景色、`MessageBubble` 左侧竖线、状态指示灯。`--primary-soft` 用于用户消息气泡背景，`--primary-border` 用于用户消息气泡边框。

### 字体

UI 字体通过 `@theme inline` 声明，全局生效：

```css
@theme inline {
    --font-sans: 'Geist Variable', sans-serif;
}
```

代码字体在组件中内联指定 `fontFamily: "'Geist Mono', monospace"`（CodeBlock 和行内代码）。

### 边框策略

暗色模式统一使用 `rgba(255,255,255,0.06)` 作为边框色，在深色背景上提供微妙的分割线效果。组件中的写法：

```tsx
<div className="border-b border-border">
```

### Base 层全局样式

`@layer base` 中设置全局边框、背景和字体（body 关闭字间距收紧并启用 `optimizeLegibility`，滚动条也在 `*` 中统一处理）：

```css
@layer base {
  * {
    @apply border-border outline-ring/50;
    scrollbar-color: var(--scrollbar-thumb) transparent;
    scrollbar-width: thin;
  }
  body {
    @apply bg-background text-foreground;
    letter-spacing: 0;
    text-rendering: optimizeLegibility;
  }
  html {
    @apply font-sans;
  }
  /* 滚动条 webkit 伪元素见下方「滚动条样式」一节 */
}
```

### 聊天画布点阵背景 (`.chat-canvas`)

`ChatArea` 容器使用 `.chat-canvas` 类，叠加一层 1px 圆点径向渐变纹理（22px 间距、透明度 7%），让背景比纯色更有质感：

```css
@layer components {
  .chat-canvas {
    background-color: var(--background);
    background-image: radial-gradient(
      circle at 1px 1px,
      color-mix(in srgb, var(--foreground) 7%, transparent) 1px,
      transparent 0
    );
    background-size: 22px 22px;
  }
}
```

### Markdown prose 配色覆盖

`@tailwindcss/typography` 默认的 prose 配色不匹配本主题，因此通过 `--prose-*` CSS 变量在 `:root` / `.dark` 各自定义，再用 `@layer components` 覆盖 `--tw-prose-*`：

```css
:root {
  --prose-heading: #17211F;
  --prose-link: #0F766E;
  --prose-bq-border: #0F766E;
  --prose-code-bg: rgba(15, 118, 110, 0.10);
  /* ... */
}
.dark {
  --prose-heading: #F0F6F4;
  --prose-link: #5EEAD4;
  --prose-bq-border: #5EEAD4;
  --prose-code-bg: rgba(94, 234, 212, 0.12);
  /* ... */
}

@layer components {
  .prose {
    --tw-prose-body: var(--foreground);
    --tw-prose-headings: var(--prose-heading, #F0F6F4);
    --tw-prose-links: var(--prose-link, #5EEAD4);
    --tw-prose-quote-borders: var(--prose-bq-border, #5EEAD4);
    --tw-prose-code: var(--prose-code-text, #99F6E4);
    /* ... */
  }
}
```

`MarkdownRenderer` 使用 `prose` 基类，行内代码 / 引用块 / 链接等元素直接引用这些变量。

### 交互状态动画

通过 CSS `@keyframes` 定义状态指示灯动画，在 `AgentAvatar` 组件中通过 `style={{ animation }}` 引用：

```css
@keyframes status-ready-pulse {
  0%, 100% { opacity: 0.6; }
  50% { opacity: 1; }
}

@keyframes status-running-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
```

`ready` 状态使用脉冲动画（2s 周期），`running` 状态使用旋转动画（1.5s 周期）。流式输出的闪烁光标使用 Tailwind 内置 `animate-pulse` + `▌` 字符。

输入校验失败的抖动动画：

```css
@keyframes shake {
  0%, 100% { transform: translateX(0); }
  20% { transform: translateX(-4px); }
  40% { transform: translateX(4px); }
  60% { transform: translateX(-4px); }
  80% { transform: translateX(4px); }
}
```

搜索结果高亮动画：

```css
@keyframes search-highlight {
  0%, 100% { background: transparent; }
  30% { background: var(--warning-soft); }
  60% { background: var(--warning-soft); }
}

.animate-search-highlight {
  animation: search-highlight 800ms ease;
}
```

### react-diff-view 主题覆盖

通过 `.diff-card` 选择器覆盖 `react-diff-view` 默认样式，使 Diff 视图融入暗色主题：

```css
.diff-card .diff-gutter { color: var(--muted-foreground); background: transparent; font-size: 11px; }
.diff-card .diff-gutter-insert { color: var(--diff-insert-color); background: var(--diff-insert-bg); }
.diff-card .diff-gutter-delete { color: var(--diff-delete-color); background: var(--diff-delete-bg); }
.diff-card .diff-code { font-size: 13px; font-family: 'Geist Mono', 'Geist Variable', monospace; }
.diff-card .diff-code-insert { background: var(--diff-insert-bg-strong); }
.diff-card .diff-code-delete { background: var(--diff-delete-bg-strong); }
.diff-card .diff-hunk-header { background: var(--muted); color: var(--muted-foreground); font-size: 11px; }
.diff-card .diff-table { border-collapse: collapse; width: 100%; }
.diff-card .diff-line { height: 20px; }
.diff-card .diff-widget-content { background: var(--muted); }
```

### Hover 交互（CSS class）

悬停效果已统一交给 Tailwind/CSS class 处理，不再维护 `src/hooks/use-hover-style.ts`。组件直接使用语义 token，例如：

```tsx
<button className="transition-colors hover:bg-bg-hover hover:text-foreground">
  ...
</button>
```

这样 hover 状态能随主题 token 一起切换，也避免运行时通过 `onMouseEnter/Leave` 写入内联样式。
### 滚动条样式

全局自定义滚动条，通过 CSS 变量控制颜色，支持亮色/暗色主题自适应。变量定义：

| 变量 | 浅色值 | 暗色值 | 用途 |
|------|--------|--------|------|
| `--scrollbar-track` | `rgba(0, 0, 0, 0.04)` | `rgba(255, 255, 255, 0.025)` | 滚动条轨道背景 |
| `--scrollbar-thumb` | `rgba(0, 0, 0, 0.22)` | `rgba(139, 145, 160, 0.32)` | 滚动条滑块颜色 |
| `--scrollbar-thumb-hover` | `rgba(0, 0, 0, 0.34)` | `rgba(139, 145, 160, 0.52)` | 滚动条滑块悬停颜色 |

Firefox 通过 `scrollbar-color` / `scrollbar-width: thin` 实现（在 `@layer base` 的 `*` 选择器中）。WebKit/Blink 通过 `::-webkit-scrollbar` 系列伪元素实现：

```css
@layer base {
  * {
    scrollbar-color: var(--scrollbar-thumb) transparent;
    scrollbar-width: thin;
  }
  *::-webkit-scrollbar { width: 10px; height: 10px; }
  *::-webkit-scrollbar-track { background: transparent; }
  *::-webkit-scrollbar-thumb {
    min-height: 44px;
    border: 3px solid transparent;
    border-radius: 999px;
    background: var(--scrollbar-thumb);
    background-clip: padding-box;
  }
  *::-webkit-scrollbar-thumb:hover {
    background: var(--scrollbar-thumb-hover);
    background-clip: padding-box;
  }
  *::-webkit-scrollbar-corner { background: transparent; }
  *:hover::-webkit-scrollbar-track { background: var(--scrollbar-track); }
}
```

滑块使用 `border: 3px solid transparent` + `background-clip: padding-box` 实现圆角胶囊效果，轨道默认透明，仅在元素悬停时显示。

### 终端输出样式 (`.terminal-output`)

TerminalPanel 使用 `dangerouslySetInnerHTML` 渲染 ANSI 风格 HTML，通过 `.terminal-output` 选择器将语义 class 映射到 CSS 变量：

```css
.terminal-output .text-success { color: var(--color-success); }
.terminal-output .text-error { color: var(--color-error); }
.terminal-output .text-primary { color: var(--primary); }
.terminal-output .text-text-secondary { color: var(--text-secondary); }
.terminal-output .text-text-tertiary { color: var(--text-tertiary); }
.terminal-output .text-text-primary { color: var(--text-primary); }
```

这些规则确保终端输出中通过 `<span class="text-success">` 等 class 名着色的文本与全局主题色保持一致。

### 圆角系统

通过 CSS 变量 `--radius` 定义基础圆角（0.625rem），Tailwind 映射多个梯度：

```css
@theme inline {
    --radius-sm: calc(var(--radius) * 0.6);
    --radius-md: calc(var(--radius) * 0.8);
    --radius-lg: var(--radius);
    --radius-xl: calc(var(--radius) * 1.4);
    --radius-2xl: calc(var(--radius) * 1.8);
    --radius-3xl: calc(var(--radius) * 2.2);
    --radius-4xl: calc(var(--radius) * 2.6);
}
```
