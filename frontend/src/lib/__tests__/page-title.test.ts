import { describe, expect, it } from 'vitest'

import { formatPageTitle } from '../page-title'

describe('formatPageTitle', () => {
  it('uses the application name for a route without a subject', () => {
    expect(formatPageTitle()).toBe('AgentHub')
  })

  it('adds the application name after a route or session subject', () => {
    expect(formatPageTitle('Codex')).toBe('Codex · AgentHub')
  })
})
