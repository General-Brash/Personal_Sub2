import { describe, expect, it } from 'vitest'

import { formatVersionLabel } from '../version'

describe('formatVersionLabel', () => {
  it('preserves the exact personal release tag', () => {
    expect(formatVersionLabel('v0.1.6-P1')).toBe('v0.1.6-P1')
  })

  it('adds the prefix to legacy API values without duplicating it', () => {
    expect(formatVersionLabel('0.1.160')).toBe('v0.1.160')
    expect(formatVersionLabel('')).toBe('')
  })
})
