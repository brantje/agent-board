import type { EventEvidence, Issue, Run } from '../types/api'
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

const knownFamilies = new Set(['run', 'agent', 'runtime', 'question', 'decision', 'tool', 'file', 'test', 'artifact', 'issue', 'review', 'git'])

export type AgentTextActivityItem = {
  kind: 'thought' | 'message'
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
  status: 'running' | 'completed' | 'failed' | 'stopped'
  input?: Record<string, unknown>
  summary?: string
  resultPreview?: string
  reason?: string
}

export type QuestionActivityItem = {
  kind: 'question'
  id: string
  occurredAt: string
  sequence: number | null
  questionId: string
  prompt: string
  choiceKind?: string
  options: Array<{ id: string; label: string }>
  status: 'open' | 'answered' | 'cancelled'
  answer?: string
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

export type RunActivityItem = AgentTextActivityItem | ToolActivityItem | QuestionActivityItem | GenericActivityItem

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

const hiddenRunActivityTypes = new Set([
  'engine.question_reply_accepted',
  'engine.question_binding_resolved',
  'engine.file_created',
  'file.created',
  'model.usage',
])

const boardActivityFamilies = new Set(['issue', 'run', 'question', 'review', 'decision', 'project'])

export function isBoardActivityEvent(type: string) {
  return boardActivityFamilies.has(type.split('.')[0] || '')
}

export function currentBranchFromEvent(event: EventEvidence) {
  if (event.type !== 'git.branch_checked_out') return undefined
  const branch = event.payload?.branch
  return typeof branch === 'string' && branch.trim() ? branch : undefined
}

export function issueKeyFromBranchEvent(event: EventEvidence) {
  if (event.type !== 'git.branch_checked_out') return undefined
  const issueKey = event.payload?.issueKey
  return typeof issueKey === 'string' && issueKey.trim() ? issueKey : undefined
}

export function applyCurrentBranchToIssue<T extends Issue>(issue: T, event: EventEvidence): T | undefined {
  const branch = currentBranchFromEvent(event)
  const issueKey = issueKeyFromBranchEvent(event)
  if (!branch || !issueKey || issue.id !== issueKey) return undefined
  return { ...issue, currentBranch: branch }
}

export function applyCurrentBranchToIssues(issues: Issue[], event: EventEvidence) {
  const branch = currentBranchFromEvent(event)
  const issueKey = issueKeyFromBranchEvent(event)
  if (!branch || !issueKey) return issues
  let changed = false
  const next = issues.map(issue => {
    if (issue.id !== issueKey) return issue
    changed = true
    return { ...issue, currentBranch: branch }
  })
  return changed ? next : issues
}

export function applyCurrentBranchToRun<T extends Run>(run: T, event: EventEvidence): T | undefined {
  const branch = currentBranchFromEvent(event)
  if (!branch || event.type !== 'git.branch_checked_out') return undefined
  if (event.runId && event.runId !== run.id) return undefined
  return { ...run, currentBranch: branch }
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
  if (event.type === 'git.branch_checked_out' && typeof payload.branch === 'string') {
    const previous = typeof payload.previousBranch === 'string' ? payload.previousBranch : undefined
    return previous ? `${previous} → ${payload.branch}` : payload.branch
  }
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

const toolIcons: Record<string, string> = {
  read: 'i-lucide-file-text',
  edit: 'i-lucide-pencil',
  write: 'i-lucide-file-plus',
  bash: 'i-lucide-square-terminal',
  grep: 'i-lucide-search',
  glob: 'i-lucide-search'
}

export function toolActivityIcon(name: string) {
  return toolIcons[name.trim().toLowerCase()] || 'i-lucide-wrench'
}

const eventFamilyIcons: Record<string, string> = {
  run: 'i-lucide-play',
  runtime: 'i-lucide-box',
  engine: 'i-lucide-cog',
  decision: 'i-lucide-check',
  agent: 'i-lucide-bot',
  file: 'i-lucide-file',
  test: 'i-lucide-flask-conical',
  artifact: 'i-lucide-package',
  issue: 'i-lucide-circle-dot',
  review: 'i-lucide-search'
}

export function eventActivityIcon(type: string) {
  return eventFamilyIcons[type.split('.')[0] || ''] || 'i-lucide-activity'
}

export function formatActivityTime(occurredAt: string, timeZone?: string, now = Date.now()) {
  const date = new Date(occurredAt)
  if (Number.isNaN(date.getTime())) return occurredAt
  const zone = timeZone ? { timeZone } : {}
  const time = new Intl.DateTimeFormat('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
    ...zone
  }).format(date)
  if (activityCalendarDay(date, timeZone) === activityCalendarDay(new Date(now), timeZone)) return time
  const day = new Intl.DateTimeFormat('en-GB', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    ...zone
  }).format(date)
  return `${day} ${time}`
}

function activityCalendarDay(date: Date, timeZone?: string) {
  return new Intl.DateTimeFormat('en-CA', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    ...(timeZone ? { timeZone } : {})
  }).format(date)
}

export function toolActivityTarget(input: unknown) {
  if (!input || typeof input !== 'object' || Array.isArray(input)) return ''
  const values = input as Record<string, unknown>
  for (const key of ['filePath', 'file_path', 'path', 'file', 'command', 'pattern', 'query', 'target']) {
    const value = values[key]
    if (typeof value === 'string' && value.trim()) return value.trim()
    if (Array.isArray(value) && value.length) return value.map(String).join(' ')
  }
  return ''
}

function toolStatus(type: string): ToolActivityItem['status'] {
  if (type === 'tool.failed') return 'failed'
  if (type === 'tool.stopped') return 'stopped'
  if (type === 'tool.completed') return 'completed'
  return 'running'
}

function isProcessStopReason(reason: string | undefined) {
  return Boolean(reason && /^process exited with code (-1|130|137|143)$/.test(reason))
}

function resolvedToolStatus(type: string, reason: string | undefined): ToolActivityItem['status'] {
  const status = toolStatus(type)
  if (status === 'failed' && isProcessStopReason(reason)) return 'stopped'
  return status
}

function stringValue(value: unknown) {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

function questionOptions(value: unknown): QuestionActivityItem['options'] {
  if (!Array.isArray(value)) return []
  return value.flatMap((option) => {
    if (!option || typeof option !== 'object' || Array.isArray(option)) return []
    const values = option as Record<string, unknown>
    const id = stringValue(values.id)
    const label = stringValue(values.label)
    return id && label ? [{ id, label }] : []
  })
}

function questionAnswer(value: unknown, options: QuestionActivityItem['options']) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  const answer = value as Record<string, unknown>
  const text = stringValue(answer.text)
  if (text) return text
  if (!Array.isArray(answer.optionIds)) return undefined
  const labels = answer.optionIds.flatMap((id) => {
    if (typeof id !== 'string') return []
    return [options.find(option => option.id === id)?.label || id]
  })
  return labels.length ? labels.join(', ') : undefined
}

function toolInput(payload: Record<string, unknown>) {
  if (payload.input && typeof payload.input === 'object' && !Array.isArray(payload.input)) {
    return payload.input as Record<string, unknown>
  }
  return undefined
}

function objectPayload(value: unknown) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  return value as Record<string, unknown>
}

function toolEventPayload(payload: Record<string, unknown>) {
  const nested = objectPayload(payload.result)
  if (!nested) return payload
  return { ...nested, ...payload }
}

function toolName(payload: Record<string, unknown>) {
  return stringValue(payload.name) || 'tool'
}

function toolReason(payload: Record<string, unknown>) {
  return stringValue(payload.reason) || stringValue(payload.error)
}

function toolTarget(payload: Record<string, unknown>, input: Record<string, unknown> | undefined) {
  return toolActivityTarget(input) || toolActivityTarget(payload) || stringValue(payload.summary) || ''
}

function isAgentTextActivity(item: RunActivityItem | undefined): item is AgentTextActivityItem {
  return item?.kind === 'thought' || item?.kind === 'message'
}

function reorderThoughtsBeforeTools(items: RunActivityItem[]) {
  const reordered = [...items]
  for (let index = 0; index < reordered.length - 1; index++) {
    const current = reordered[index]
    const next = reordered[index + 1]
    if (current?.kind === 'tool' && isAgentTextActivity(next)) {
      reordered[index] = next
      reordered[index + 1] = current
      index++
    }
  }
  return reordered
}

export function projectRunActivity(events: EventEvidence[]): RunActivityItem[] {
  const items: RunActivityItem[] = []
  const toolIndexes = new Map<string, number>()
  const pendingByName = new Map<string, number[]>()
  const questionIndexes = new Map<string, number>()

  for (const event of [...events].sort(compareEvents)) {
    if (hiddenRunActivityTypes.has(event.type)) continue
    const payload = event.payload || {}
    if (event.type === 'agent.message' && (payload.kind === 'reasoning' || payload.kind === 'message') && typeof payload.message === 'string' && payload.message.trim()) {
      items.push({
        kind: payload.kind === 'message' ? 'message' : 'thought',
        id: event.id,
        occurredAt: event.occurredAt,
        sequence: event.sequence,
        message: payload.message
      })
      continue
    }

    const questionId = event.type.startsWith('question.') ? stringValue(payload.questionId) : undefined
    if (event.type === 'question.created' && questionId) {
      const prompt = stringValue(payload.prompt)
      if (prompt) {
        questionIndexes.set(questionId, items.length)
        items.push({
          kind: 'question',
          id: event.id,
          occurredAt: event.occurredAt,
          sequence: event.sequence,
          questionId,
          prompt,
          choiceKind: stringValue(payload.kind),
          options: questionOptions(payload.options),
          status: 'open'
        })
        continue
      }
    }
    if ((event.type === 'question.answered' || event.type === 'question.cancelled') && questionId) {
      const existingIndex = questionIndexes.get(questionId)
      if (existingIndex != null) {
        const existing = items[existingIndex]
        if (existing?.kind === 'question') {
          items[existingIndex] = {
            ...existing,
            status: event.type === 'question.answered' ? 'answered' : 'cancelled',
            answer: event.type === 'question.answered' ? questionAnswer(payload.answer, existing.options) : existing.answer
          }
          continue
        }
      }
    }

    if (event.type.startsWith('tool.')) {
      const fields = toolEventPayload(payload)
      const name = toolName(fields)
      const input = toolInput(fields)
      const explicitId = stringValue(fields.toolCallId)
      const reason = toolReason(fields)
      const status = resolvedToolStatus(event.type, reason)
      let existingIndex = explicitId ? toolIndexes.get(explicitId) : undefined
      if (existingIndex == null && !explicitId && status !== 'running') {
        const pending = pendingByName.get(name)
        existingIndex = pending?.pop()
      }

      if (existingIndex != null) {
        const existing = items[existingIndex]
        if (existing?.kind === 'tool') {
          items[existingIndex] = {
            ...existing,
            name: stringValue(fields.name) || existing.name,
            label: toolActivityLabel(stringValue(fields.name) || existing.name),
            target: toolTarget(fields, input) || existing.target,
            status,
            input: input || existing.input,
            summary: stringValue(fields.summary) || existing.summary,
            resultPreview: stringValue(fields.resultPreview) || existing.resultPreview,
            reason: status === 'stopped' ? undefined : reason || existing.reason
          }
          continue
        }
      }

      const toolCallId = explicitId || event.id
      toolIndexes.set(toolCallId, items.length)
      if (status === 'running') {
        const pending = pendingByName.get(name) || []
        pending.push(items.length)
        pendingByName.set(name, pending)
      }
      items.push({
        kind: 'tool',
        id: event.id,
        occurredAt: event.occurredAt,
        sequence: event.sequence,
        toolCallId,
        name,
        label: toolActivityLabel(name),
        target: toolTarget(fields, input),
        status,
        input,
        summary: stringValue(fields.summary),
        resultPreview: stringValue(fields.resultPreview),
        reason: status === 'stopped' ? undefined : reason
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
  return reorderThoughtsBeforeTools(items)
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