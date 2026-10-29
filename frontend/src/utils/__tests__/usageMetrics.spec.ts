import { describe, expect, it } from 'vitest'
import { calculateUsageOutputTps, formatUsageOutputTps } from '../usageMetrics'

describe('usage output TPS', () => {
  it.each(['stream', 'ws_v2'])('excludes first-token latency for %s requests', (request_type) => {
    const usage = { request_type, output_tokens: 186, duration_ms: 9180, first_token_ms: 3420 }
    expect(calculateUsageOutputTps(usage)).toBeCloseTo(32.2916667)
    expect(formatUsageOutputTps(usage)).toBe('32.29 tok/s')
  })

  it('uses the explicit sync type instead of a conflicting legacy stream flag', () => {
    expect(formatUsageOutputTps({
      request_type: 'sync', stream: true, output_tokens: 100, duration_ms: 2000, first_token_ms: 1000,
    })).toBe('50.00 tok/s')
  })

  it('supports legacy streams and missing first-token timing', () => {
    expect(formatUsageOutputTps({
      stream: true, output_tokens: 100, duration_ms: 2000, first_token_ms: 1000,
    })).toBe('100.00 tok/s')
    expect(formatUsageOutputTps({ stream: true, output_tokens: 100, duration_ms: 2000 })).toBe('50.00 tok/s')
  })

  it('shows zero throughput for a valid token request with no output', () => {
    expect(formatUsageOutputTps({ output_tokens: 0, duration_ms: 2000 })).toBe('0.00 tok/s')
  })

  it.each([
    {},
    { output_tokens: 100 },
    { output_tokens: 100, duration_ms: 0 },
    { output_tokens: 100, duration_ms: -1 },
    { output_tokens: -1, duration_ms: 1000 },
    { output_tokens: Number.NaN, duration_ms: 1000 },
    { output_tokens: 100, duration_ms: Number.POSITIVE_INFINITY },
    { stream: true, output_tokens: 100, duration_ms: 1000, first_token_ms: 1000 },
    { stream: true, output_tokens: 100, duration_ms: 1000, first_token_ms: 2000 },
    { stream: true, output_tokens: 100, duration_ms: 1000, first_token_ms: -1 },
    { stream: true, output_tokens: 100, duration_ms: 1000, first_token_ms: Number.NaN },
  ])('does not fabricate throughput from invalid timing or token data: %j', (usage) => {
    expect(calculateUsageOutputTps(usage)).toBeNull()
    expect(formatUsageOutputTps(usage)).toBe('-')
  })
})
