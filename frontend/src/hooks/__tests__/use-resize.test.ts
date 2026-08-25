import { describe, expect, it } from 'vitest'

import { normalizeResizeWidth } from '../use-resize'

describe('normalizeResizeWidth', () => {
  it('keeps a valid stored width', () => {
    expect(normalizeResizeWidth(280, 200, 400, 300)).toBe(280)
  })

  it('clamps stored widths to the configured range', () => {
    expect(normalizeResizeWidth(100, 200, 400, 300)).toBe(200)
    expect(normalizeResizeWidth(900, 200, 400, 300)).toBe(400)
  })

  it('uses a clamped fallback for invalid legacy values', () => {
    expect(normalizeResizeWidth(0, 200, 400, 500)).toBe(400)
    expect(normalizeResizeWidth(Number.NaN, 200, 400, 100)).toBe(200)
  })
})
