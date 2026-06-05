import type { Components } from 'react-markdown'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { CodeBlock } from './CodeBlock'

interface MarkdownRendererProps {
  content: string
}

const TREE_CHARS_RE = /[│├└┬┼─]/
const ASCII_TREE_RE = /^\s*(?:[|` ]+)?(?:\|--|`--)|^\s*\.$/

function isTreeLikeLine(line: string): boolean {
  const trimmed = line.trimEnd()
  if (!trimmed) return false
  return TREE_CHARS_RE.test(trimmed) || ASCII_TREE_RE.test(trimmed)
}

function fenceTreeBlocks(content: string): string {
  const lines = content.split('\n')
  const hasTree = lines.some(isTreeLikeLine)
  if (!hasTree) {
    return content
  }

  const output: string[] = []
  let insideFence = false

  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i]

    // Track fenced code block boundaries — skip tree detection inside fences
    if (line.trim().startsWith('```')) {
      if (insideFence && line.trim() === '```') {
        insideFence = false
      } else if (!insideFence) {
        insideFence = true
      }
      output.push(line)
      continue
    }

    if (insideFence) {
      output.push(line)
      continue
    }

    const nextLine = lines[i + 1] ?? ''
    const isBlockStart =
      TREE_CHARS_RE.test(line) ||
      /^\s*\.$/.test(line.trim()) ||
      (ASCII_TREE_RE.test(line) && isTreeLikeLine(nextLine))

    if (!isBlockStart) {
      output.push(line)
      continue
    }

    output.push('```text')
    while (i < lines.length) {
      const current = lines[i]
      const next = lines[i + 1] ?? ''
      if (!current.trim()) {
        break
      }
      if (!(isTreeLikeLine(current) || (current.includes('/') && isTreeLikeLine(next)))) {
        break
      }
      output.push(current)
      i += 1
    }
    output.push('```')
    i -= 1
  }

  return output.join('\n')
}

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
      <h3 className="mt-4 mb-2 text-lg font-semibold text-[var(--prose-heading)]">{children}</h3>
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
      <blockquote className="my-3 border-l-[3px] border-[var(--prose-bq-border)] bg-[var(--prose-bq-bg)] pl-4 py-2 rounded-r-md">
        {children}
      </blockquote>
    )
  },

  // ─── 列表 ───
  ul({ children }) {
    return (
      <ul className="my-2 ml-4 list-disc space-y-1 marker:text-[var(--prose-li-marker)]">
        {children}
      </ul>
    )
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
    return <em className="italic text-secondary">{children}</em>
  },

  // ─── 图片 ───
  img({ src, alt }) {
    return <img src={src} alt={alt} className="my-3 max-w-full rounded-lg border border-white/5" />
  },

  // ─── 行内代码 / 代码块 ───
  pre({ children }) {
    return <>{children}</>
  },
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

  // ─── 表格 ───
  table({ children }) {
    return (
      <div className="my-3 overflow-x-auto rounded-lg border border-white/5">
        <table className="w-full border-collapse text-sm">{children}</table>
      </div>
    )
  },
  th({ children }) {
    return (
      <th className="border-b border-white/8 bg-[var(--prose-bq-bg)] px-4 py-2.5 text-left text-sm font-medium text-secondary">
        {children}
      </th>
    )
  },
  td({ children }) {
    return <td className="border-b border-white/5 px-4 py-2.5 text-sm">{children}</td>
  },
}

export function MarkdownRenderer({ content }: MarkdownRendererProps) {
  return (
    <div className="prose prose-invert min-w-0 max-w-full overflow-hidden text-sm text-foreground [overflow-wrap:anywhere] [&_ol]:min-w-0 [&_p]:min-w-0 [&_p]:whitespace-pre-wrap [&_pre]:max-w-full [&_pre]:overflow-x-auto [&_pre]:whitespace-pre [&_table]:table-fixed [&_td]:break-words [&_th]:break-words [&_ul]:min-w-0">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {fenceTreeBlocks(content)}
      </ReactMarkdown>
    </div>
  )
}
