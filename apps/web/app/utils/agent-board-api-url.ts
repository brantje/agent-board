export const DEFAULT_AGENT_BOARD_API_URL = 'http://127.0.0.1:3001'

function isLoopbackHost(hostname: string) {
  return hostname === '127.0.0.1' || hostname === 'localhost' || hostname === '::1' || hostname === '[::1]'
}

export function resolveAgentBoardApiUrl(raw?: string) {
  const trimmed = raw?.trim()
  const value = trimmed ? trimmed : DEFAULT_AGENT_BOARD_API_URL
  const parsed = new URL(value)
  if (parsed.protocol === 'https:') {
    return value.replace(/\/$/, '')
  }
  if (parsed.protocol === 'http:' && isLoopbackHost(parsed.hostname)) {
    return value.replace(/\/$/, '')
  }
  if (parsed.protocol === 'http:') {
    throw new Error(
      `AGENT_BOARD_API_URL must use HTTPS for remote hosts (${parsed.hostname}); HTTP is only permitted for loopback`
    )
  }
  throw new Error('AGENT_BOARD_API_URL must use http or https')
}
