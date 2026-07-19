export function formatVersionLabel(version: string): string {
  const normalized = version.trim()
  if (!normalized) return ''
  return normalized.startsWith('v') ? normalized : `v${normalized}`
}
