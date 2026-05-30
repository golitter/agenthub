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
  if (!lines.some(isTreeLikeLine)) {
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
  pre({ children }) {
    return <>{children}</>
  },
  code({ className, children, ...props }) {
    const match = /language-(\w+)/.exec(className ?? '')
    const code = String(children).replace(/\n$/, '')

    if (match) {
      return <CodeBlock code={code} language={match[1]} />
    }

    return (
      <code
        className="inline rounded bg-code px-1.5 py-0.5 text-[13px] [overflow-wrap:anywhere]"
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
  table({ children }) {
    return (
      <div className="overflow-x-auto">
        <table className="w-full border-collapse text-sm border-border">{children}</table>
      </div>
    )
  },
  th({ children }) {
    return (
      <th className="border-b border-border bg-accent px-3 py-2 text-left font-medium text-muted-foreground">
        {children}
      </th>
    )
  },
  td({ children }) {
    return <td className="border-b border-border px-3 py-2">{children}</td>
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
