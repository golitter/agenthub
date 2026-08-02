import { describe, expect, it } from 'vitest'

import { encodePathSegments, getSafeHttpUrl } from '../utils'

describe('encodePathSegments', () => {
  it('encodes each workspace path segment without encoding separators', () => {
    expect(encodePathSegments('/src/My File/#report.ts')).toBe('src/My%20File/%23report.ts')
  })

  it('does not emit empty segments for repeated leading or trailing slashes', () => {
    expect(encodePathSegments('//artifacts//image.png/')).toBe('artifacts/image.png')
  })

  it('rejects executable URL protocols', () => {
    expect(getSafeHttpUrl('javascript:alert(1)')).toBeNull()
    expect(getSafeHttpUrl('https://example.com/demo')).toBe('https://example.com/demo')
  })
})
