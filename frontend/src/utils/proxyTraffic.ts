export function formatProxyTraffic(
  bytes: number | null | undefined,
  period: 'today' | '30d' = 'today',
): string {
  if (typeof bytes !== 'number' || !Number.isFinite(bytes) || bytes < 0) return '-'

  const gigabyte = 1024 ** 3
  const useGigabytes = period === '30d' || bytes >= gigabyte
  const unit = useGigabytes ? 'G' : 'MB'
  const divisor = useGigabytes ? gigabyte : 1024 ** 2
  return `${(bytes / divisor).toFixed(2)} ${unit}`
}
