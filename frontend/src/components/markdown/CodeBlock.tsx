import type { HighlighterCore, LanguageRegistration } from '@shikijs/core'
import { memo, useEffect, useState } from 'react'

interface CodeBlockProps {
  code: string
  language?: string
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

type LanguageLoader = () => Promise<{ default: LanguageRegistration[] }>

const LANGUAGE_LOADERS = {
  javascript: () => import('@shikijs/langs/javascript'),
  typescript: () => import('@shikijs/langs/typescript'),
  jsx: () => import('@shikijs/langs/jsx'),
  tsx: () => import('@shikijs/langs/tsx'),
  html: () => import('@shikijs/langs/html'),
  css: () => import('@shikijs/langs/css'),
  json: () => import('@shikijs/langs/json'),
  markdown: () => import('@shikijs/langs/markdown'),
  bash: () => import('@shikijs/langs/bash'),
  python: () => import('@shikijs/langs/python'),
  go: () => import('@shikijs/langs/go'),
  rust: () => import('@shikijs/langs/rust'),
  yaml: () => import('@shikijs/langs/yaml'),
  sql: () => import('@shikijs/langs/sql'),
  diff: () => import('@shikijs/langs/diff'),
} satisfies Record<string, LanguageLoader>

type SupportedLanguage = keyof typeof LANGUAGE_LOADERS

let highlighterPromise: Promise<HighlighterCore> | undefined
const languagePromises = new Map<SupportedLanguage, Promise<void>>()

function normalizeLanguage(language?: string): SupportedLanguage | 'text' {
  const requested = language?.toLowerCase() ?? 'text'
  const normalized = LANGUAGE_ALIASES[requested] ?? requested
  return normalized in LANGUAGE_LOADERS ? (normalized as SupportedLanguage) : 'text'
}

function getHighlighter(): Promise<HighlighterCore> {
  highlighterPromise ??= Promise.all([
    import('@shikijs/core'),
    import('@shikijs/engine-javascript'),
    import('@shikijs/themes/github-light'),
    import('@shikijs/themes/tokyo-night'),
  ])
    .then(([{ createHighlighterCore }, { createJavaScriptRegexEngine }, lightTheme, darkTheme]) =>
      createHighlighterCore({
        engine: createJavaScriptRegexEngine(),
        themes: [lightTheme.default, darkTheme.default],
      }),
    )
    .catch((err) => {
      // core、engine 或主题初始化失败时清空 promise，允许下次代码块重新尝试。
      highlighterPromise = undefined
      throw err
    })
  return highlighterPromise
}

function loadLanguage(highlighter: HighlighterCore, language: SupportedLanguage): Promise<void> {
  const existing = languagePromises.get(language)
  if (existing) return existing

  const loader = LANGUAGE_LOADERS[language]
  const promise = loader()
    .then((module) => highlighter.loadLanguage(module.default))
    .catch((error) => {
      // 失败的 grammar 不应毒化后续代码块；下一次渲染仍可重新请求。
      languagePromises.delete(language)
      throw error
    })
  languagePromises.set(language, promise)
  return promise
}

function CodeBlockComponent({ code, language }: CodeBlockProps) {
  const [highlighted, setHighlighted] = useState<{
    code: string
    language?: string
    html: string | null
  } | null>(null)

  useEffect(() => {
    let cancelled = false

    if (!language) {
      return () => {
        cancelled = true
      }
    }

    async function highlight() {
      try {
        const normalizedLanguage = normalizeLanguage(language)
        if (normalizedLanguage === 'text') {
          if (!cancelled) setHighlighted({ code, language, html: null })
          return
        }
        const highlighter = await getHighlighter()
        await loadLanguage(highlighter, normalizedLanguage)
        const result = highlighter.codeToHtml(code, {
          lang: normalizedLanguage,
          themes: {
            light: 'github-light',
            dark: 'tokyo-night',
          },
          defaultColor: false,
        })

        if (!cancelled) {
          setHighlighted({ code, language, html: result })
        }
      } catch {
        if (!cancelled) {
          setHighlighted({ code, language, html: null })
        }
      }
    }

    highlight()

    return () => {
      cancelled = true
    }
  }, [code, language])

  const lines = code.split('\n')
  const html =
    highlighted?.code === code && highlighted.language === language ? highlighted.html : null

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
          className="code-theme min-w-0 max-w-full overflow-x-auto [&_.shiki]:m-0 [&_.shiki]:min-w-max [&_.shiki]:overflow-x-visible [&_.shiki]:p-4 [&_.shiki_pre]:m-0"
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

// 记忆化：code/language 是稳定字符串；避免在流式传输期间父组件重新渲染时
// 重复执行 Shiki 高亮 effect。
export const CodeBlock = memo(CodeBlockComponent)
