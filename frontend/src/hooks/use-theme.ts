import { useCallback, useEffect, useState } from 'react'

export type Theme = 'system' | 'dark' | 'light'
export type ResolvedTheme = 'dark' | 'light'

const STORAGE_KEY = 'theme'
const DARK_CLASS = 'dark'
const DEFAULT: Theme = 'dark'
const SYSTEM_QUERY = '(prefers-color-scheme: dark)'
const THEME_CHANGE_EVENT = 'agenthub:theme-change'
const THEME_COLORS: Record<ResolvedTheme, string> = {
  dark: '#0b1110',
  light: '#fbfcfb',
}

function getSystemPrefersDark(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function'
    ? window.matchMedia(SYSTEM_QUERY).matches
    : true
}

export function resolveTheme(theme: Theme, prefersDark = getSystemPrefersDark()): ResolvedTheme {
  if (theme === 'system') return prefersDark ? 'dark' : 'light'
  return theme
}

function applyTheme(theme: Theme, prefersDark = getSystemPrefersDark()) {
  const resolvedTheme = resolveTheme(theme, prefersDark)
  const html = document.documentElement
  html.classList.toggle(DARK_CLASS, resolvedTheme === 'dark')
  html.style.colorScheme = resolvedTheme
  document
    .querySelector<HTMLMetaElement>('meta[name="theme-color"]')
    ?.setAttribute('content', THEME_COLORS[resolvedTheme])
}

export function readStoredTheme(): Theme {
  // 隐私模式 / localStorage 禁用时 getItem 会抛错，需防御。
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY)
    if (stored === 'dark' || stored === 'light' || stored === 'system') return stored
  } catch {
    // 忽略，回退到默认主题。
  }
  return DEFAULT
}

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(readStoredTheme)
  const [systemPrefersDark, setSystemPrefersDark] = useState(getSystemPrefersDark)
  const resolvedTheme = resolveTheme(theme, systemPrefersDark)

  const setTheme = useCallback((next: Theme) => {
    const nextSystemPrefersDark = getSystemPrefersDark()
    // 隐私模式 / 存储配额满时 setItem 抛 QuotaExceededError，
    // 此时仅让当前会话生效（applyTheme + setThemeState），不阻断交互。
    try {
      window.localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // 持久化失败不影响本次切换。
    }
    setSystemPrefersDark(nextSystemPrefersDark)
    applyTheme(next, nextSystemPrefersDark)
    setThemeState(next)
    window.dispatchEvent(new CustomEvent<Theme>(THEME_CHANGE_EVENT, { detail: next }))
  }, [])

  // useTheme 可能同时被设置面板、CodeMirror 等多个组件使用；偏好变化需要
  // 广播给同页的其他消费者，否则 DOM 已变色但已有编辑器仍会保留旧主题。
  useEffect(() => {
    const handleThemeChange = (event: Event) => {
      const detail = (event as CustomEvent<unknown>).detail
      const nextTheme =
        detail === 'dark' || detail === 'light' || detail === 'system' ? detail : readStoredTheme()
      setThemeState(nextTheme)
      setSystemPrefersDark(getSystemPrefersDark())
    }
    const handleStorageChange = (event: StorageEvent) => {
      if (event.key === STORAGE_KEY) handleThemeChange(event)
    }

    window.addEventListener(THEME_CHANGE_EVENT, handleThemeChange)
    window.addEventListener('storage', handleStorageChange)
    return () => {
      window.removeEventListener(THEME_CHANGE_EVENT, handleThemeChange)
      window.removeEventListener('storage', handleStorageChange)
    }
  }, [])

  // 挂载时应用（防止内联脚本未执行的情况）；system 模式才订阅系统变化。
  useEffect(() => {
    applyTheme(theme)
    const mediaQuery =
      theme === 'system' && typeof window.matchMedia === 'function'
        ? window.matchMedia(SYSTEM_QUERY)
        : null

    if (!mediaQuery) return

    const handleChange = (event: MediaQueryListEvent) => {
      setSystemPrefersDark(event.matches)
      applyTheme(theme, event.matches)
    }
    if (typeof mediaQuery.addEventListener === 'function') {
      mediaQuery.addEventListener('change', handleChange)
      return () => mediaQuery.removeEventListener('change', handleChange)
    }
    if (
      typeof mediaQuery.addListener !== 'function' ||
      typeof mediaQuery.removeListener !== 'function'
    ) {
      return
    }
    mediaQuery.addListener(handleChange)
    return () => mediaQuery.removeListener(handleChange)
  }, [theme])

  return { theme, resolvedTheme, setTheme } as const
}
