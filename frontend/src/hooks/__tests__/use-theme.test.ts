import { describe, expect, it } from 'vitest'

import { resolveTheme } from '../use-theme'

describe('resolveTheme', () => {
  it('keeps an explicit light or dark preference', () => {
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('dark', false)).toBe('dark')
  })

  it('follows the system preference only in system mode', () => {
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', false)).toBe('light')
  })
})
