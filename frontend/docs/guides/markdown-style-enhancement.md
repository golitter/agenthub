# Markdown 风格增强修改报告

> 日期：2026-06-05
> 状态：方案设计

## 1. 现状分析

### 1.1 当前架构

| 层级 | 技术 | 文件 |
|------|------|------|
| Markdown 解析 | `react-markdown` + `remark-gfm` | `src/components/markdown/MarkdownRenderer.tsx` |
| 代码高亮 | `shiki`（tokyo-night 主题） | `src/components/markdown/CodeBlock.tsx` |
| 排版样式 | Tailwind `prose prose-invert` 类 | `src/components/markdown/MarkdownRenderer.tsx:130` |
| 全局主题 | CSS 变量（oklch / hex） | `src/index.css` |

### 1.2 核心问题

#### 问题 A：`@tailwindcss/typography` 未安装

`MarkdownRenderer` 外层 div 使用了 `prose prose-invert` 类：

```tsx
<div className="prose prose-invert ...">
```

但 `package.json` 中 **没有安装** `@tailwindcss/typography` 插件。Tailwind CSS 4.x 虽然内置了部分原子类，但 `prose` 系列排版类 **仍需安装 typography 插件** 才能生效。

**结果**：`prose` 类被静默忽略，所有 Markdown 元素（标题、列表、引用、链接等）退化为浏览器默认样式或完全没有样式。

#### 问题 B：`components` 覆盖不完整

`MarkdownRenderer.tsx` 的 `components` 对象仅覆盖了 5 种元素：

| 已覆盖 | 缺失 |
|--------|------|
| `code` / `pre` | `h1` `h2` `h3` `h4` `h5` `h6` |
| `table` / `th` / `td` | `blockquote` `a`（链接） |
| | `ul` `ol` `li`（列表） |
| | `hr`（分隔线） |
| | `strong` `em`（粗体/斜体） |
| | `img`（图片） |
| | `p`（段落） |

标题层级之间没有视觉区分，引用块无左边框，链接没有下划线或颜色区分，列表缺乏缩进和标记样式。

#### 问题 C：暗色模式下对比度不足

暗色主题 `--foreground: #E8EBF0` 作为正文色尚可，但：
- 标题缺少层级色彩变化（应比正文更亮或带 brand 色调）
- 行内 `code` 的 `bg-code`（`#0D0F14`）与代码块背景几乎相同，辨识度低
- `blockquote` 无背景/边框区分
- 链接颜色与正文相同，无法识别

### 1.3 截图对比（现状 vs 预期）

**现状**：
- `# Hello` → 与正文相同的白色小字
- `> quote` → 无缩进无边框，混在正文中
- `- item` → 无圆点标记，缩进不一致
- `[link](url)` → 与正文同色，无下划线
- `**bold**` → 粗细差异极小
- `` `code` `` → 暗底暗字，几乎看不出来

**预期**：
- `# Hello` → 大号加粗，带层级色彩
- `> quote` → 左侧 3px 品牌色竖线 + 半透明背景
- `- item` → 清晰圆点 + 适当缩进
- `[link](url)` → 品牌蓝紫色 + 下划线
- `**bold**` → 明显加粗 + 更高对比度
- `` `code` `` → 半透明背景 + 等宽字体 + 高对比

---

## 2. 修改方案

### 方案概述

采用 **"安装 typography 插件 + 自定义 prose 变量 + 补全 components"** 三层策略：

```
Layer 1: @tailwindcss/typography  → 提供 prose 基础排版
Layer 2: CSS 变量覆盖            → 暗色主题下的精细配色
Layer 3: React components 覆盖   → 标题锚点、引用装饰等高级效果
```

### 2.1 Layer 1：安装 typography 插件

```bash
cd frontend
pnpm add -D @tailwindcss/typography
```

在 `src/index.css` 顶部增加导入：

```css
@import "tailwindcss";
@import "@tailwindcss/typography";  /* ← 新增 */
@import "tw-animate-css";
@import "shadcn/tailwind.css";
```

> Tailwind CSS 4.x 通过 `@import` 方式加载插件，不需要在 `vite.config.ts` 中额外配置。

### 2.2 Layer 2：CSS 变量覆盖（暗色主题 prose 配色）

在 `src/index.css` 的 `.dark { ... }` 块内追加 prose 语义变量：

```css
.dark {
  /* ... 已有变量 ... */

  /* ─── Markdown prose 增强变量 ─── */
  --prose-heading: #F0F2F7;           /* 标题：比正文更亮 */
  --prose-heading-h1: #FFFFFF;        /* H1 最亮 */
  --prose-link: #818CF8;              /* 链接：indigo-400 */
  --prose-link-hover: #A5B4FC;        /* 链接 hover：indigo-300 */
  --prose-bold: #F0F2F7;              /* 粗体：更亮 */
  --prose-blockquote-border: #6366F1; /* 引用左边框：品牌色 */
  --prose-blockquote-bg: rgba(99, 102, 241, 0.06); /* 引用背景 */
  --prose-code-bg: rgba(99, 102, 241, 0.12);       /* 行内代码背景 */
  --prose-code-text: #C7D2FE;         /* 行内代码文字：indigo-200 */
  --prose-hr: rgba(255, 255, 255, 0.08);           /* 分隔线 */
  --prose-li-marker: #6366F1;         /* 列表标记颜色 */
}
```

然后在 `@layer base` 或单独的 `@layer components` 块中添加 prose 覆盖样式：

```css
/* ─── Markdown prose 增强样式 ─── */
@layer components {
  .prose {
    --tw-prose-body: var(--foreground);
    --tw-prose-headings: var(--prose-heading, #F0F2F7);
    --tw-prose-links: var(--prose-link, #818CF8);
    --tw-prose-bold: var(--prose-bold, #F0F2F7);
    --tw-prose-quotes: var(--text-secondary, #8B91A0);
    --tw-prose-code: var(--prose-code-text, #C7D2FE);
    --tw-prose-counters: var(--text-tertiary, #5A6070);
    --tw-prose-bullets: var(--prose-li-marker, #6366F1);
    --tw-prose-hr: var(--prose-hr, rgba(255, 255, 255, 0.08));
  }
}
```

### 2.3 Layer 3：补全 React `components` 覆盖

在 `MarkdownRenderer.tsx` 的 `components` 对象中增加以下元素：

```tsx
const components: Components = {
  // ─── 标题 ───
  h1({ children }) {
    return (
      <h1 className="mt-6 mb-3 text-2xl font-bold tracking-tight text-[var(--prose-heading-h1)]">
        {children}
      </h1>
    )
  },
  h2({ children }) {
    return (
      <h2 className="mt-5 mb-2.5 text-xl font-semibold tracking-tight text-[var(--prose-heading)] border-b border-white/5 pb-2">
        {children}
      </h2>
    )
  },
  h3({ children }) {
    return (
      <h3 className="mt-4 mb-2 text-lg font-semibold text-[var(--prose-heading)]">
        {children}
      </h3>
    )
  },
  h4({ children }) {
    return (
      <h4 className="mt-3 mb-1.5 text-base font-semibold text-[var(--prose-heading)]">
        {children}
      </h4>
    )
  },

  // ─── 段落 ───
  p({ children }) {
    return <p className="mb-3 leading-7">{children}</p>
  },

  // ─── 链接 ───
  a({ href, children }) {
    return (
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        className="text-[var(--prose-link)] underline decoration-[var(--prose-link)]/40 underline-offset-2 transition-colors hover:text-[var(--prose-link-hover)] hover:decoration-[var(--prose-link-hover)]/60"
      >
        {children}
      </a>
    )
  },

  // ─── 引用块 ───
  blockquote({ children }) {
    return (
      <blockquote className="my-3 border-l-[3px] border-[var(--prose-blockquote-border)] bg-[var(--prose-blockquote-bg)] pl-4 py-2 rounded-r-md">
        {children}
      </blockquote>
    )
  },

  // ─── 列表 ───
  ul({ children }) {
    return <ul className="my-2 ml-4 list-disc space-y-1 marker:text-[var(--prose-li-marker)]">{children}</ul>
  },
  ol({ children }) {
    return <ol className="my-2 ml-4 list-decimal space-y-1">{children}</ol>
  },
  li({ children }) {
    return <li className="leading-7">{children}</li>
  },

  // ─── 分隔线 ───
  hr() {
    return <hr className="my-6 border-[var(--prose-hr)]" />
  },

  // ─── 粗体 / 斜体 ───
  strong({ children }) {
    return <strong className="font-bold text-[var(--prose-bold)]">{children}</strong>
  },
  em({ children }) {
    return <em className="italic text-[var(--text-secondary)]">{children}</em>
  },

  // ─── 图片 ───
  img({ src, alt }) {
    return (
      <img
        src={src}
        alt={alt}
        className="my-3 max-w-full rounded-lg border border-white/5"
      />
    )
  },

  // ─── 行内代码（保持已有逻辑）───
  code({ className, children, ...props }) {
    const match = /language-(\w+)/.exec(className ?? '')
    const code = String(children).replace(/\n$/, '')

    if (match) {
      return <CodeBlock code={code} language={match[1]} />
    }

    if (code.includes('\n')) {
      return <CodeBlock code={code} />
    }

    return (
      <code
        className="inline rounded-md bg-[var(--prose-code-bg)] px-1.5 py-0.5 text-[13px] text-[var(--prose-code-text)] [overflow-wrap:anywhere]"
        style={{
          fontFamily: "'Geist Mono', monospace",
          letterSpacing: 0,
        }}
        {...props}
      >
        {children}
      </code>
    )
  },

  // ─── 表格（增强已有样式）───
  table({ children }) {
    return (
      <div className="my-3 overflow-x-auto rounded-lg border border-white/5">
        <table className="w-full border-collapse text-sm">{children}</table>
      </div>
    )
  },
  th({ children }) {
    return (
      <th className="border-b border-white/8 bg-[var(--prose-blockquote-bg)] px-4 py-2.5 text-left text-sm font-medium text-[var(--text-secondary)]">
        {children}
      </th>
    )
  },
  td({ children }) {
    return <td className="border-b border-white/5 px-4 py-2.5 text-sm">{children}</td>
  },

  // ─── pre（保持已有逻辑）───
  pre({ children }) {
    return <>{children}</>
  },
}
```

### 2.4 修改文件清单

| 文件 | 修改内容 |
|------|----------|
| `frontend/package.json` | 新增 `@tailwindcss/typography` 依赖 |
| `frontend/src/index.css` | 导入 typography 插件 + 新增 prose CSS 变量 + prose 覆盖样式 |
| `frontend/src/components/markdown/MarkdownRenderer.tsx` | 补全 `components` 对象中 14 个元素的渲染组件 |

### 2.5 兼容性说明

- **Tailwind CSS 4.x**：使用 `@import "@tailwindcss/typography"` 方式加载，无需修改 `vite.config.ts`
- **现有代码块高亮**：`CodeBlock.tsx` 使用 shiki，不受影响
- **`prose-invert`**：安装插件后生效，配合 CSS 变量覆盖实现暗色优化
- **`fenceTreeBlocks` 预处理**：不受影响，仍在 `ReactMarkdown` 之前运行

---

## 3. 视觉效果预期

### 3.1 各元素增强效果

| 元素 | 修改前 | 修改后 |
|------|--------|--------|
| **H1** | 与正文同色，无大小差异 | `text-2xl`，纯白，加粗 |
| **H2** | 同上 | `text-xl`，底部 5% 白色分隔线 |
| **H3/H4** | 同上 | 渐小但仍然比正文亮 |
| **粗体** | 几乎无差异 | `font-bold` + 更亮的颜色 |
| **斜体** | 与正文相同 | 斜体 + `text-secondary` 色调 |
| **链接** | 与正文同色，无装饰 | indigo-400 + 半透明下划线 |
| **行内代码** | 暗底暗字 | indigo 半透明底 + indigo-200 文字 |
| **引用块** | 无边框无背景 | 3px indigo 左边框 + 半透明底 |
| **有序/无序列表** | 无标记/缩进不一致 | 清晰圆点/数字 + 统一缩进 |
| **分隔线** | 几乎不可见 | 8% 白色线条 |
| **表格** | 基础边框 | 圆角外框 + 半透明表头背景 |

### 3.2 排版节奏

```
正文行高: 1.75 (leading-7)
标题上间距: h1=6 h2=5 h3=4 h4=3 (Tailwind spacing scale)
段落下间距: 3 (mb-3)
列表项间距: 1 (space-y-1)
代码块圆角: rounded-lg (8px)
```

---

## 4. 实施步骤

```
1. pnpm add -D @tailwindcss/typography
2. src/index.css → 添加 @import + CSS 变量 + prose 覆盖
3. MarkdownRenderer.tsx → 替换 components 对象
4. 验证：启动前端，发送包含各类 Markdown 元素的消息
5. 可选：参考 demo 页面进行视觉回归测试
```

预计影响范围：仅 `markdown/` 目录 + `index.css`，不涉及其他组件。
