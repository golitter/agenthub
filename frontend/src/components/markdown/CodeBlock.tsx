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
      className="overflow-x-auto rounded-lg bg-code text-[13px] leading-[1.65]"
      style={{
        fontFamily: "'Geist Mono', monospace",
        letterSpacing: 0,
      }}
    >
      {html ? (
        <div className="min-w-0 overflow-x-auto" dangerouslySetInnerHTML={{ __html: html }} />
      ) : (
        <div className="flex">
          <div className="select-none pr-4 pl-4 text-right text-tertiary">
            {lines.map((_, i) => (
              <div key={i}>{i + 1}</div>
            ))}
          </div>
          <pre className="min-w-0 flex-1 overflow-x-auto pr-4">
            <code className="break-all">{code}</code>
          </pre>
        </div>
      )}
    </div>
  )
}
