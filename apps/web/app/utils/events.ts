import type { EventEvidence } from '../types/api'
import { statusLabel } from './issues'

const agentMessageKinds: Record<string, string> = {
  message: 'Message',
  plan: 'Plan',
  rationale: 'Rationale',
  progress: 'Progress',
  discovery: 'Discovery',
  summary: 'Summary'
}

const knownFamilies = new Set(['run', 'agent', 'runtime', 'question', 'decision', 'tool', 'file', 'test', 'artifact', 'issue', 'review'])

export function compareEvents(left: EventEvidence, right: EventEvidence) {
  const leftSequence = left.sequence ?? Number.MAX_SAFE_INTEGER
  const rightSequence = right.sequence ?? Number.MAX_SAFE_INTEGER
  if (leftSequence !== rightSequence) return leftSequence - rightSequence
  return left.occurredAt.localeCompare(right.occurredAt) || left.id.localeCompare(right.id)
}

export function mergeEvents(existing: EventEvidence[], incoming: EventEvidence[]) {
  const byId = new Map<string, EventEvidence>()
  for (const item of existing) byId.set(item.id, item)
  for (const item of incoming) {
    if (!byId.has(item.id)) byId.set(item.id, item)
  }
  return [...byId.values()].sort(compareEvents)
}

export function maxSequence(events: EventEvidence[]) {
  return events.reduce((max, item) => item.sequence != null && item.sequence > max ? item.sequence : max, 0)
}

export function refetchTargets(type: string) {
  const family = type.split('.')[0]
  return {
    run: family === 'run',
    questions: family === 'question',
    reviews: family === 'review',
    issue: family === 'issue'
  }
}

const boardActivityFamilies = new Set(['issue', 'run', 'question', 'review', 'decision'])

export function isBoardActivityEvent(type: string) {
  return boardActivityFamilies.has(type.split('.')[0] || '')
}

export function eventTitle(event: EventEvidence) {
  if (event.type === 'agent.message') {
    const kind = typeof event.payload?.kind === 'string' ? event.payload.kind : 'message'
    return agentMessageKinds[kind] ?? 'Message'
  }
  const family = event.type.split('.')[0]
  if (family && knownFamilies.has(family)) return statusLabel(event.type.replaceAll('.', '_'))
  return event.type
}

export function eventDescription(event: EventEvidence) {
  const payload = event.payload || {}
  if (typeof payload.message === 'string') return payload.message
  if (typeof payload.path === 'string') {
    return typeof payload.oldPath === 'string' && payload.oldPath ? `${payload.oldPath} → ${payload.path}` : payload.path
  }
  if (Array.isArray(payload.command)) return payload.command.map(String).join(' ')
  if (typeof payload.name === 'string') return payload.name
  if (typeof payload.status === 'string') return statusLabel(payload.status)
  return ''
}

export function isUnknownEvent(event: EventEvidence) {
  return eventTitle(event) === event.type
}

export function parseEventMessage(data: string): EventEvidence | undefined {
  try {
    const parsed = JSON.parse(data) as EventEvidence
    if (!parsed || typeof parsed !== 'object' || typeof parsed.id !== 'string' || typeof parsed.type !== 'string') return undefined
    if (!parsed.payload || typeof parsed.payload !== 'object' || Array.isArray(parsed.payload)) parsed.payload = {}
    return parsed
  } catch {
    return undefined
  }
}
