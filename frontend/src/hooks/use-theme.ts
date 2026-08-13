import { useCallback, useEffect, useState } from 'react'

export type Theme = 'dark' | 'light'

const STORAGE_KEY = 'theme'
const DARK_CLASS = 'dark'
const DEFAULT: Theme = 'dark'
const THEME_COLORS: Record<Theme, string> = {
  dark: '#0b1110',
  light: '#fbfcfb',
}

function applyTheme(theme: Theme) {
  const html = document.documentElement
  html.classList.toggle(DARK_CLASS, theme === 'dark')
  html.style.colorScheme = theme
  document
    .querySelector<HTMLMetaElement>('meta[name="theme-color"]')
    ?.setAttribute('content', THEME_COLORS[theme])
}

function readStoredTheme(): Theme {
  // 隐私模式 / localStorage 禁用时 getItem 会抛错，需防御。
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored === 'dark' || stored === 'light') return stored
  } catch {
    // 忽略，回退到默认主题。
  }
  return DEFAULT
}

export function useTheme() {
  const [theme, setThemeState] = useState<Theme>(readStoredTheme)

  const setTheme = useCallback((next: Theme) => {
    // 隐私模式 / 存储配额满时 setItem 抛 QuotaExceededError，
    // 此时仅让当前会话生效（applyTheme + setThemeState），不阻断交互。
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      // 持久化失败不影响本次切换。
    }
    applyTheme(next)
    setThemeState(next)
  }, [])

  // 挂载时应用（防止内联脚本未执行的情况）
  useEffect(() => {
    applyTheme(theme)
  }, [theme])

  return { theme, setTheme } as const
}
