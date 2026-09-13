export type IdentityKind = 'agent' | 'user'

const userColors = ['primary', 'info', 'success', 'warning', 'secondary'] as const

export function identityDisplayName(name: string) {
  const trimmed = name.trim()
  return trimmed || 'Unknown'
}

export function identityInitial(name: string) {
  const trimmed = name.trim()
  const match = trimmed.match(/\p{L}/u)
  return match ? match[0].toUpperCase() : '?'
}

export function userIdentityColor(name: string) {
  let hash = 0
  for (const char of name.trim()) {
    hash = (hash + char.charCodeAt(0)) % 997
  }
  return userColors[hash % userColors.length]
}
