import { useEffect, useState } from 'react'

interface CodeBlockProps {
  code: string
  language?: string
}

export function CodeBlock({ code, language }: CodeBlockProps) {
  const [html, setHtml] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function highlight() {
      try {
        const shiki = await import('shiki')
        const highlighter = await shiki.createHighlighter({
          themes: ['tokyo-night'],
          langs: language ? [language] : [],
        })

        if (cancelled) return

        const result = highlighter.codeToHtml(code, {
          lang: language ?? 'text',
          theme: 'tokyo-night',
        })

        setHtml(result)
        highlighter.dispose()
      } catch {
        // fallback to plain text
      }
    }

    if (language) {
      highlight()
    }

    return () => {
      cancelled = true
    }
  }, [code, language])

  const lines = code.split('\n')

  return (
    <div
      className="overflow-x-auto rounded-lg text-[13px] leading-[1.65]"
      style={{
        backgroundColor: 'var(--code-bg)',
        fontFamily: "'Geist Mono', monospace",
        letterSpacing: 0,
      }}
    >
      {html ? (
        <div dangerouslySetInnerHTML={{ __html: html }} />
      ) : (
        <div className="flex">
          <div className="select-none pr-4 pl-4 text-right" style={{ color: '#5A6070' }}>
            {lines.map((_, i) => (
              <div key={i}>{i + 1}</div>
            ))}
          </div>
          <pre className="flex-1 overflow-x-auto pr-4">
            <code>{code}</code>
          </pre>
        </div>
      )}
    </div>
  )
}
