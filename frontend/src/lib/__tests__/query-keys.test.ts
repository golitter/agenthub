import { describe, expect, it } from 'vitest'

import { isAdminQueryKey } from '../query-keys'

describe('isAdminQueryKey', () => {
  it('matches every namespaced admin query', () => {
    expect(isAdminQueryKey(['admin-sessions'])).toBe(true)
    expect(isAdminQueryKey(['admin-resources', { scope: 'all' }])).toBe(true)
    expect(isAdminQueryKey(['admin-avatar'])).toBe(true)
  })

  it('does not invalidate unrelated application data', () => {
    expect(isAdminQueryKey(['conversations'])).toBe(false)
    expect(isAdminQueryKey(['skills'])).toBe(false)
    expect(isAdminQueryKey([])).toBe(false)
  })
})
