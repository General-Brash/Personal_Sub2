import { describe, expect, it } from 'vitest'
import { formatProxyTraffic } from '../proxyTraffic'

describe('formatProxyTraffic', () => {
  it('uses MB below 1024 MB and G at or above the boundary for today', () => {
    expect(formatProxyTraffic(0)).toBe('0.00 MB')
    expect(formatProxyTraffic(512 * 1024 ** 2)).toBe('512.00 MB')
    expect(formatProxyTraffic(1024 ** 3 - 1)).toBe('1024.00 MB')
    expect(formatProxyTraffic(1024 ** 3)).toBe('1.00 G')
    expect(formatProxyTraffic(1.5 * 1024 ** 3)).toBe('1.50 G')
  })

  it('always uses G for the last 30 days', () => {
    expect(formatProxyTraffic(0, '30d')).toBe('0.00 G')
    expect(formatProxyTraffic(512 * 1024 ** 2, '30d')).toBe('0.50 G')
    expect(formatProxyTraffic(30 * 1024 ** 3, '30d')).toBe('30.00 G')
  })

  it('does not present missing or invalid statistics as zero usage', () => {
    for (const bytes of [undefined, null, -1, NaN, Infinity]) {
      expect(formatProxyTraffic(bytes)).toBe('-')
      expect(formatProxyTraffic(bytes, '30d')).toBe('-')
    }
  })
})
