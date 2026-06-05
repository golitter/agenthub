/**
 * Markdown 渲染效果 Demo 页面
 *
 * 使用方法：
 *   1. 将此文件复制到 frontend/src/pages/ 下
 *   2. 在路由中注册（或临时替换主页内容）
 *   3. 启动 make run-frontend 查看效果
 *
 * 本页面包含完整的 Markdown 元素展示，用于验证：
 *   - 标题层级 H1~H4
 *   - 粗体 / 斜体 / 粗斜体
 *   - 行内代码
 *   - 链接
 *   - 有序 / 无序列表
 *   - 引用块
 *   - 代码块（含语法高亮）
 *   - 表格
 *   - 分隔线
 *   - 图片（占位）
 *   - 混合嵌套场景
 */

import { MarkdownRenderer } from '../components/markdown/MarkdownRenderer'

// ─── 测试用 Markdown 内容 ───

const BASIC_TEXT = `
这是一段普通文本。包含 **粗体文字**、*斜体文字* 和 ***粗斜体文字***。

这是一段包含 \`行内代码\` 的文本，以及一个 [示例链接](https://example.com)。

普通文本与 \`const x = 1\` 行内代码混排时，行内代码应该有明显的背景色和等宽字体区分。
`.trim()

const HEADINGS = `
# 一级标题 H1

这是 H1 下的正文内容。一级标题应该最大最醒目。

## 二级标题 H2

H2 标题下方应该有一条细分隔线，帮助视觉分组。

### 三级标题 H3

H3 是常用的小节标题，字号介于 H2 和正文之间。

#### 四级标题 H4

H4 用于更细的分段，字号接近正文但加粗。
`.trim()

const LISTS = `
## 列表示例

### 无序列表

- 第一项：前端开发
- 第二项：后端开发
- 第三项：DevOps
  - 嵌套子项：CI/CD
  - 嵌套子项：监控告警
- 第四项：测试

### 有序列表

1. 需求分析
2. 系统设计
3. 编码实现
4. 测试验证
5. 部署上线

### 任务列表（GFM）

- [x] 已完成的任务
- [x] 另一个已完成任务
- [ ] 待办任务
- [ ] 另一个待办任务
`.trim()

const BLOCKQUOTES = `
## 引用块示例

> 这是一段引用文字。引用块应该有明显的左侧竖线和背景色，与正文形成视觉区分。

> **带格式的引用**：引用块内部也可以包含 **粗体**、*斜体* 和 \`行内代码\` 等 Markdown 格式。

> 多行引用：
> 第二行引用内容。
> 第三行引用内容，保持一致的样式。

> **注意**：这是一条重要提示，引用块使得关键信息在视觉上脱颖而出。
`.trim()

const CODE_BLOCKS = `
## 代码块示例

### TypeScript

\`\`\`typescript
interface MarkdownRendererProps {
  content: string
  theme?: 'light' | 'dark'
}

export function MarkdownRenderer({ content, theme = 'dark' }: MarkdownRendererProps) {
  const processed = fenceTreeBlocks(content)

  return (
    <div className={\`prose \${theme === 'dark' ? 'prose-invert' : ''}\`}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {processed}
      </ReactMarkdown>
    </div>
  )
}
\`\`\`

### Python

\`\`\`python
from dataclasses import dataclass
from typing import Optional

@dataclass
class AgentConfig:
    name: str
    model: str = "claude-sonnet-4-6"
    max_tokens: int = 4096
    temperature: float = 0.7

    def validate(self) -> bool:
        """验证配置是否合法"""
        return bool(self.name) and self.max_tokens > 0
\`\`\`

### Shell

\`\`\`bash
# 启动三端服务
make run

# 查看日志
tail -f logs/frontend.log
tail -f logs/backend.log
tail -f logs/agentend.log
\`\`\`
`.trim()

const TABLES = `
## 表格示例

| 属性 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| \`content\` | \`string\` | — | Markdown 原始内容 |
| \`theme\` | \`'light' \\| 'dark'\` | \`'dark'\` | 主题模式 |
| \`className\` | \`string\` | \`''\` | 额外 CSS 类名 |
| \`components\` | \`Components\` | \`{}\` | 自定义渲染组件 |

### 对比表格

| 方案 | 优点 | 缺点 |
|------|------|------|
| typography 插件 | 开箱即用，维护活跃 | 样式覆盖需理解 CSS 变量 |
| 全自定义 components | 完全控制 | 代码量大，需自行处理所有元素 |
| **混合方案（推荐）** | 基础用插件 + 关键元素自定义 | 需理解两层优先级 |
`.trim()

const HR = `
## 分隔线

上方的分隔线应该清晰可见，颜色比背景稍亮。

---

分隔线下方的文字。
`.trim()

const MIXED = `
## 混合嵌套场景

### 步骤说明

1. **安装依赖**：运行 \`pnpm add -D @tailwindcss/typography\`
2. **配置插件**：在 \`index.css\` 中添加 \`@import "@tailwindcss/typography"\`
3. **覆盖样式**：
   > 通过 CSS 变量自定义 \`prose\` 颜色，无需修改组件代码
4. **验证效果**：使用本 Demo 页面对照各元素样式

### 代码说明

渲染器使用 \`react-markdown\` 库，配合 \`remark-gfm\` 支持 GitHub 风格 Markdown：

\`\`\`tsx
// components 覆盖示例
const components: Components = {
  h1({ children }) {
    return <h1 className="text-2xl font-bold">{children}</h1>
  },
  blockquote({ children }) {
    return (
      <blockquote className="border-l-3 border-indigo-500 pl-4 bg-indigo-500/5 py-2 rounded-r-md">
        {children}
      </blockquote>
    )
  },
}
\`\`\`

> **提示**：修改 \`components\` 对象中的任何组件后，热重载会自动生效。
`.trim()

// ─── Demo 页面组件 ───

function DemoSection({ title, content }: { title: string; content: string }) {
  return (
    <section className="rounded-xl border border-white/6 bg-card p-6">
      <h2 className="mb-4 text-sm font-medium uppercase tracking-wider text-brand">{title}</h2>
      <MarkdownRenderer content={content} />
    </section>
  )
}

export default function MarkdownDemoPage() {
  const sections = [
    { title: '基础文本', content: BASIC_TEXT },
    { title: '标题层级', content: HEADINGS },
    { title: '列表示例', content: LISTS },
    { title: '引用块', content: BLOCKQUOTES },
    { title: '代码块', content: CODE_BLOCKS },
    { title: '表格', content: TABLES },
    { title: '分隔线', content: HR },
    { title: '混合嵌套', content: MIXED },
  ]

  return (
    <div className="dark min-h-screen bg-bg-canvas p-8">
      <div className="mx-auto max-w-3xl space-y-6">
        {/* 页面头部 */}
        <div className="mb-8 text-center">
          <h1 className="text-3xl font-bold text-text-primary">Markdown 渲染效果 Demo</h1>
          <p className="mt-2 text-sm text-text-secondary">
            验证各类 Markdown 元素的渲染效果，对照修改报告中的预期样式
          </p>
          <div className="mt-4 inline-flex gap-3 text-xs text-text-tertiary">
            <span className="rounded-md bg-bg-hover px-2 py-1">react-markdown</span>
            <span className="rounded-md bg-bg-hover px-2 py-1">remark-gfm</span>
            <span className="rounded-md bg-bg-hover px-2 py-1">shiki (tokyo-night)</span>
            <span className="rounded-md bg-bg-hover px-2 py-1">prose-invert</span>
          </div>
        </div>

        {/* 各节展示 */}
        {sections.map((section) => (
          <DemoSection key={section.title} title={section.title} content={section.content} />
        ))}

        {/* 页脚 */}
        <div className="py-6 text-center text-xs text-text-tertiary">
          Markdown Style Enhancement Demo · {new Date().getFullYear()}
        </div>
      </div>
    </div>
  )
}
