import { useEffect, useState } from 'react'
import { codeToHtml } from 'shiki'

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
        const result = await codeToHtml(code, {
          lang: language ?? 'text',
          theme: 'tokyo-night',
        })

        if (!cancelled) {
          setHtml(result)
        }
      } catch {
        // language not supported — fallback to plain text
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
      className="max-w-full overflow-x-auto rounded-lg bg-code text-[13px] leading-[1.65]"
      style={{
        fontFamily: "'Geist Mono', monospace",
        letterSpacing: 0,
      }}
    >
      {html ? (
        <div
          className="min-w-0 max-w-full overflow-x-auto [&_.shiki]:m-0 [&_.shiki]:min-w-max [&_.shiki]:overflow-x-visible [&_.shiki]:p-4 [&_.shiki_pre]:m-0"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <div className="flex min-w-max">
          <div className="select-none px-4 py-3 text-right text-tertiary">
            {lines.map((_, i) => (
              <div key={i}>{i + 1}</div>
            ))}
          </div>
          <pre className="min-w-0 flex-1 overflow-x-auto py-3 pr-4">
            <code className="whitespace-pre">{code}</code>
          </pre>
        </div>
      )}
    </div>
  )
}
