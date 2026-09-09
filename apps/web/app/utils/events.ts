import type { EventEvidence } from '../types/api'
import { statusLabel } from './issues'

const agentMessageKinds: Record<string, string> = {
  message: 'Message',
  plan: 'Plan',
  rationale: 'Rationale',
  progress: 'Progress',
  discovery: 'Discovery',
  summary: 'Summary',
  reasoning: 'Thought'
}

const knownFamilies = new Set(['run', 'agent', 'runtime', 'question', 'decision', 'tool', 'file', 'test', 'artifact', 'issue', 'review'])

export type ThoughtActivityItem = {
  kind: 'thought'
  id: string
  occurredAt: string
  sequence: number | null
  message: string
}

export type ToolActivityItem = {
  kind: 'tool'
  id: string
  occurredAt: string
  sequence: number | null
  toolCallId: string
  name: string
  label: string
  target: string
  status: 'running' | 'completed' | 'failed'
  input?: Record<string, unknown>
  summary?: string
  resultPreview?: string
  reason?: string
}

export type GenericActivityItem = {
  kind: 'event'
  id: string
  occurredAt: string
  sequence: number | null
  title: string
  description: string
  event: EventEvidence
  unknown: boolean
}

export type RunActivityItem = ThoughtActivityItem | ToolActivityItem | GenericActivityItem

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

const boardActivityFamilies = new Set(['issue', 'run', 'question', 'review', 'decision', 'project'])

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

const toolLabels: Record<string, string> = {
  read: 'Read',
  edit: 'Edit',
  write: 'Write',
  bash: 'Bash',
  grep: 'Search',
  glob: 'Search'
}

export function toolActivityLabel(name: string) {
  const normalized = name.trim().toLowerCase()
  if (!normalized) return 'Tool'
  if (toolLabels[normalized]) return toolLabels[normalized]
  return normalized.charAt(0).toUpperCase() + normalized.slice(1).replaceAll('_', ' ')
}

export function toolActivityTarget(input: unknown) {
  if (!input || typeof input !== 'object' || Array.isArray(input)) return ''
  const values = input as Record<string, unknown>
  for (const key of ['filePath', 'path', 'command', 'pattern', 'query']) {
    const value = values[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
    if (Array.isArray(value) && value.length) return value.map(String).join(' ')
  }
  return ''
}

function toolStatus(type: string): ToolActivityItem['status'] {
  if (type === 'tool.failed') return 'failed'
  if (type === 'tool.completed') return 'completed'
  return 'running'
}

function stringValue(value: unknown) {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

export function projectRunActivity(events: EventEvidence[]): RunActivityItem[] {
  const items: RunActivityItem[] = []
  const toolIndexes = new Map<string, number>()

  for (const event of [...events].sort(compareEvents)) {
    const payload = event.payload || {}
    if (event.type === 'agent.message' && payload.kind === 'reasoning' && typeof payload.message === 'string' && payload.message.trim()) {
      items.push({ kind: 'thought', id: event.id, occurredAt: event.occurredAt, sequence: event.sequence, message: payload.message })
      continue
    }

    const toolCallId = event.type.startsWith('tool.') ? stringValue(payload.toolCallId) : undefined
    if (toolCallId) {
      const input = payload.input && typeof payload.input === 'object' && !Array.isArray(payload.input)
        ? payload.input as Record<string, unknown>
        : undefined
      const existingIndex = toolIndexes.get(toolCallId)
      if (existingIndex != null) {
        const existing = items[existingIndex]
        if (existing?.kind === 'tool') {
          items[existingIndex] = {
            ...existing,
            name: stringValue(payload.name) || existing.name,
            label: toolActivityLabel(stringValue(payload.name) || existing.name),
            target: toolActivityTarget(input) || existing.target,
            status: toolStatus(event.type),
            input: input || existing.input,
            summary: stringValue(payload.summary) || existing.summary,
            resultPreview: stringValue(payload.resultPreview) || existing.resultPreview,
            reason: stringValue(payload.reason) || existing.reason
          }
          continue
        }
      }

      const name = stringValue(payload.name) || 'tool'
      toolIndexes.set(toolCallId, items.length)
      items.push({
        kind: 'tool',
        id: event.id,
        occurredAt: event.occurredAt,
        sequence: event.sequence,
        toolCallId,
        name,
        label: toolActivityLabel(name),
        target: toolActivityTarget(input),
        status: toolStatus(event.type),
        input,
        summary: stringValue(payload.summary),
        resultPreview: stringValue(payload.resultPreview),
        reason: stringValue(payload.reason)
      })
      continue
    }

    items.push({
      kind: 'event',
      id: event.id,
      occurredAt: event.occurredAt,
      sequence: event.sequence,
      title: eventTitle(event),
      description: eventDescription(event),
      event,
      unknown: isUnknownEvent(event)
    })
  }
  return items
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