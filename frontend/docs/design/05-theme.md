# Theme — 主题与样式系统

## 实现了什么

基于 CSS 变量的暗色主题系统，通过 Tailwind CSS 4 的 `@theme inline` 机制将 CSS 变量映射为 Tailwind 工具类。所有颜色通过 CSS 自定义属性控制，组件中不硬编码颜色值。

## 怎么实现的

### 全局样式入口 (`src/index.css`)

顶层引入 Tailwind、shadcn 主题、Geist 字体，通过 `@theme inline` 将 CSS 变量注册为 Tailwind 可用的颜色 token：

```css
@import "tailwindcss";
@import "tw-animate-css";
@import "shadcn/tailwind.css";
@import "@fontsource-variable/geist";

@custom-variant dark (&:is(.dark *));

@theme inline {
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
    /* ... 更多映射 */
}
```

`:root` 定义浅色模式变量（oklch 色彩空间），`.dark` 覆盖为暗色模式变量：

```css
:root {
    --background: oklch(1 0 0);
    --foreground: oklch(0.145 0 0);
    --primary: oklch(0.205 0 0);
    /* ... */
}

.dark {
    --background: #0A0B0E;
    --foreground: #E8EBF0;
    --card: #1A1D24;
    --popover: #22262F;
    --primary: #6366F1;
    --destructive: #EF4444;
    --border: rgba(255,255,255,0.06);
    --sidebar: #111318;
    --text-tertiary: #5A6070;
    /* ... */
}
```

### 暗色模式色彩体系

基础背景色：

| 变量 | 值 | 用途 |
|------|------|------|
| `--background` | `#0A0B0E` | 主画布背景 |
| `--sidebar` | `#111318` | 侧栏背景 |
| `--card` | `#1A1D24` | 卡片 / Agent 消息气泡背景 |
| `--accent` | `#22262F` | hover 背景 / 搜索框背景 / 表头背景 |
| `--popover` | `#22262F` | 弹出层背景 |

文本色：

| 变量 | 值 | 用途 |
|------|------|------|
| `--foreground` | `#E8EBF0` | 主文本 |
| `--muted-foreground` | `#8B91A0` | 次要文本（时间、描述等） |
| `--text-tertiary` | `#5A6070` | 占位符、辅助信息 |

功能色与品牌色：

| 变量 | 值 | 用途 |
|------|------|------|
| `--primary` | `#6366F1` | 品牌 / 主色调（Indigo） |
| `--destructive` | `#EF4444` | 错误 |
| `--border` | `rgba(255,255,255,0.06)` | 统一边框色 |
| `--color-success` | `#22C55E` | 成功 / 就绪 |
| `--color-warning` | `#F59E0B` | 警告 / 运行中 |

### Agent 专属色

`.dark` 块中定义的 Agent 品牌色，通过 `@theme inline` 映射为 Tailwind 工具类：

```css
.dark {
    --agent-claude: #DA7756;
    --agent-opencode: #10B981;
    --agent-orchestrator: #EAB308;
    --agent-codex: #6366F1;
    --primary-soft: rgba(99, 102, 241, 0.08);
    --primary-border: rgba(99, 102, 241, 0.15);
    --code-bg: #0D0F14;
    --color-danger-bg: rgba(239, 68, 68, 0.1);
    --diff-insert-bg: rgba(34, 197, 94, 0.08);
    --diff-delete-bg: rgba(239, 68, 68, 0.08);
    --diff-insert-bg-strong: rgba(34, 197, 94, 0.1);
    --diff-delete-bg-strong: rgba(239, 68, 68, 0.1);
}
```

这些颜色用于 `AgentAvatar` 背景色、`MessageBubble` 左侧竖线、状态指示灯。`--agent-codex` 使用与品牌色相同的 Indigo 值。`--primary-soft` 用于用户消息气泡背景，`--primary-border` 用于用户消息气泡边框。

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

`@layer base` 中设置全局边框、背景和字体：

```css
@layer base {
  * {
    @apply border-border outline-ring/50;
  }
  body {
    @apply bg-background text-foreground;
    letter-spacing: -0.01em;
  }
  html {
    @apply font-sans;
  }
}
```

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

### Hover 交互 (`src/hooks/use-hover-style.ts`)

通过 `onMouseEnter/Leave` 切换背景色实现悬停效果，默认使用 `var(--accent)` 作为 hover 背景：

```typescript
export function useHoverStyle(hoverBg = 'var(--accent)', normalBg = 'transparent') {
  return {
    onMouseEnter: (e: React.MouseEvent<HTMLElement>) => {
      e.currentTarget.style.backgroundColor = hoverBg
    },
    onMouseLeave: (e: React.MouseEvent<HTMLElement>) => {
      e.currentTarget.style.backgroundColor = normalBg
    },
  }
}
```

### 圆角系统

通过 CSS 变量 `--radius` 定义基础圆角（0.625rem），Tailwind 映射多个梯度：

```css
@theme inline {
    --radius-sm: calc(var(--radius) * 0.6);
    --radius-md: calc(var(--radius) * 0.8);
    --radius-lg: var(--radius);
    --radius-xl: calc(var(--radius) * 1.4);
    --radius-2xl: calc(var(--radius) * 1.8);
}
```
