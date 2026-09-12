export type ProviderHealthStatus = 'UNKNOWN' | 'HEALTHY' | 'UNHEALTHY' | string

export function providerHealthBadgeColor(status: ProviderHealthStatus, checking = false) {
  if (checking) return 'warning'
  switch (status) {
    case 'HEALTHY':
      return 'success'
    case 'UNHEALTHY':
      return 'error'
    default:
      return 'neutral'
  }
}

export function providerHealthBadgeLabel(status: ProviderHealthStatus, checking = false) {
  if (checking) return 'Health: Checking'
  const readable = typeof status === 'string' && status
    ? status.toLowerCase().replaceAll('_', ' ').replace(/^./, character => character.toUpperCase())
    : 'Unavailable'
  return `Health: ${readable}`
}

export function providerModelsBadgeLabel(filtered: number, total: number) {
  return `Models: ${filtered} / ${total}`
}

export function providerModelCounts(item: { filteredModelCount?: number | null; totalModelCount?: number | null }) {
  if (item.filteredModelCount == null || item.totalModelCount == null) return undefined
  return { filtered: item.filteredModelCount, total: item.totalModelCount }
}
