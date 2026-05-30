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
        className="inline-block break-all rounded bg-code px-1.5 py-0.5 text-[13px]"
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
    <div className="prose-invert max-w-none break-words text-sm text-foreground">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  )
}
