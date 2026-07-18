import { useEffect, useState } from 'react'

interface CodeBlockProps {
  code: string
  language?: string
}

type SyntaxHighlighter = {
  codeToHtml: (code: string, options: { lang: string; theme: string }) => string
}

const LANGUAGE_ALIASES: Record<string, string> = {
  js: 'javascript',
  ts: 'typescript',
  py: 'python',
  sh: 'bash',
  shell: 'bash',
  yml: 'yaml',
  md: 'markdown',
  golang: 'go',
}

const SUPPORTED_LANGUAGES = new Set([
  'javascript',
  'typescript',
  'jsx',
  'tsx',
  'html',
  'css',
  'json',
  'markdown',
  'bash',
  'python',
  'go',
  'rust',
  'yaml',
  'sql',
  'diff',
])

let highlighterPromise: Promise<SyntaxHighlighter> | undefined

function normalizeLanguage(language?: string) {
  const requested = language?.toLowerCase() ?? 'text'
  const normalized = LANGUAGE_ALIASES[requested] ?? requested
  return SUPPORTED_LANGUAGES.has(normalized) ? normalized : 'text'
}

function getHighlighter(): Promise<SyntaxHighlighter> {
  highlighterPromise ??= Promise.all([
    import('@shikijs/core'),
    import('@shikijs/engine-javascript'),
    import('@shikijs/themes/tokyo-night'),
    import('@shikijs/langs/javascript'),
    import('@shikijs/langs/typescript'),
    import('@shikijs/langs/jsx'),
    import('@shikijs/langs/tsx'),
    import('@shikijs/langs/html'),
    import('@shikijs/langs/css'),
    import('@shikijs/langs/json'),
    import('@shikijs/langs/markdown'),
    import('@shikijs/langs/bash'),
    import('@shikijs/langs/python'),
    import('@shikijs/langs/go'),
    import('@shikijs/langs/rust'),
    import('@shikijs/langs/yaml'),
    import('@shikijs/langs/sql'),
    import('@shikijs/langs/diff'),
  ]).then(([{ createHighlighterCore }, { createJavaScriptRegexEngine }, theme, ...languages]) =>
    createHighlighterCore({
      engine: createJavaScriptRegexEngine(),
      themes: [theme.default],
      langs: languages.map((language) => language.default),
    }),
  )
  return highlighterPromise
}

export function CodeBlock({ code, language }: CodeBlockProps) {
  const [html, setHtml] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false

    async function highlight() {
      try {
        const highlighter = await getHighlighter()
        const result = highlighter.codeToHtml(code, {
          lang: normalizeLanguage(language),
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
