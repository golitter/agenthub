import type { Components } from 'react-markdown'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

import { CodeBlock } from './CodeBlock'

interface MarkdownRendererProps {
  content: string
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
        className="rounded px-1.5 py-0.5 text-[13px]"
        style={{
          backgroundColor: 'var(--code-bg)',
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
        <table
          className="w-full border-collapse text-sm"
          style={{ borderColor: 'rgba(255,255,255,0.06)' }}
        >
          {children}
        </table>
      </div>
    )
  },
  th({ children }) {
    return (
      <th
        className="border-b px-3 py-2 text-left font-medium"
        style={{
          borderColor: 'rgba(255,255,255,0.06)',
          backgroundColor: 'var(--bg-hover)',
          color: 'var(--text-secondary)',
        }}
      >
        {children}
      </th>
    )
  },
  td({ children }) {
    return (
      <td className="border-b px-3 py-2" style={{ borderColor: 'rgba(255,255,255,0.06)' }}>
        {children}
      </td>
    )
  },
}

export function MarkdownRenderer({ content }: MarkdownRendererProps) {
  return (
    <div className="prose-invert max-w-none text-sm" style={{ color: 'var(--text-primary)' }}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  )
}
