import {
  requestTypeToLegacyStream,
  resolveUsageRequestType,
  type UsageRequestTypeLike,
} from '@/utils/usageRequestType'
import { BILLING_MODE_TOKEN } from '@/utils/billingMode'

interface UsageTimingLike extends UsageRequestTypeLike {
  output_tokens?: number | null
  duration_ms?: number | null
  first_token_ms?: number | null
  billing_mode?: string | null
  media_type?: string | null
  image_count?: number | null
  image_output_tokens?: number | null
  image_output_cost?: number | null
  image_output_size?: string | null
}

function textOutputTokensForTps(usage: UsageTimingLike): number | null {
  if (usage.billing_mode && usage.billing_mode !== BILLING_MODE_TOKEN) return null
  const mediaType = usage.media_type?.trim().toLowerCase()
  if (mediaType && mediaType !== 'text' && mediaType !== 'image') return null

  const outputTokens = usage.output_tokens
  if (typeof outputTokens !== 'number' || !Number.isFinite(outputTokens) || outputTokens < 0) {
    return null
  }
  for (const value of [usage.image_count, usage.image_output_tokens, usage.image_output_cost]) {
    if (value != null && (typeof value !== 'number' || !Number.isFinite(value) || value < 0)) {
      return null
    }
  }

  const imageTokens = usage.image_output_tokens ?? 0
  const hasImageOutput = mediaType === 'image' || (usage.image_count ?? 0) > 0
    || (usage.image_output_cost ?? 0) > 0
    || Boolean(usage.image_output_size?.trim())
  if (imageTokens > outputTokens || (hasImageOutput && imageTokens <= 0)) return null
  // Mixed token output is usable only when the recorded image portion can be separated.
  if (imageTokens > 0 && imageTokens === outputTokens) return null
  return outputTokens - imageTokens
}

function isStreamingUsage(usage: UsageTimingLike): boolean {
  return (requestTypeToLegacyStream(resolveUsageRequestType(usage)) ?? usage.stream) === true
}

export function calculateUsageOutputTps(usage: UsageTimingLike): number | null {
  const outputTokens = textOutputTokensForTps(usage)
  const { duration_ms: durationMs, first_token_ms: firstTokenMs } = usage
  if (outputTokens == null
    || typeof durationMs !== 'number' || !Number.isFinite(durationMs) || durationMs <= 0) {
    return null
  }

  let generationMs = durationMs
  // Exclude the initial wait from streamed generation, but retain a total-time fallback for older logs.
  if (isStreamingUsage(usage) && firstTokenMs != null) {
    if (typeof firstTokenMs !== 'number' || !Number.isFinite(firstTokenMs) || firstTokenMs < 0) return null
    generationMs -= firstTokenMs
  }
  if (generationMs <= 0) return null

  const tps = outputTokens / generationMs * 1000
  return Number.isFinite(tps) ? tps : null
}

export function isUsageOutputTpsEstimated(usage: UsageTimingLike): boolean {
  return isStreamingUsage(usage) && usage.first_token_ms == null
    && calculateUsageOutputTps(usage) != null
}

export function formatUsageOutputTps(usage: UsageTimingLike): string {
  const tps = calculateUsageOutputTps(usage)
  return tps == null ? '-' : `${tps.toFixed(2)} tok/s`
}
