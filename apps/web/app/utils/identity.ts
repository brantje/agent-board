import type { Assignee } from '../types/api'

export type IdentityKind = 'agent' | 'user' | 'squad'

const userColors = ['primary', 'info', 'success', 'warning', 'secondary'] as const

export function assigneeIdentityKind(type: Assignee['type']): IdentityKind {
  if (type === 'USER') return 'user'
  if (type === 'SQUAD') return 'squad'
  return 'agent'
}

export function assigneeTypeLabel(type: Assignee['type']) {
  if (type === 'USER') return 'User'
  if (type === 'SQUAD') return 'Squad'
  return 'Agent'
}

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
