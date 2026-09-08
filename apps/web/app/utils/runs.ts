import { statusLabel } from './issues'

export function runStatusLabel(status: string) {
  return `Run · ${statusLabel(status)}`
}

export function formatElapsed(startedAt: string | null, completedAt: string | null, now = Date.now()) {
  if (!startedAt) return 'Not started'
  const milliseconds = Math.max(0, (completedAt ? Date.parse(completedAt) : now) - Date.parse(startedAt))
  if (!Number.isFinite(milliseconds)) return 'Not started'
  const seconds = Math.floor(milliseconds / 1000)
  const minutes = Math.floor(seconds / 60)
  const hours = Math.floor(minutes / 60)
  if (hours) return `${hours}h ${minutes % 60}m`
  if (minutes) return `${minutes}m ${seconds % 60}s`
  return `${seconds}s`
}

export function commandLabel(command: unknown) {
  return Array.isArray(command) ? command.map(String).join(' ') : ''
}
