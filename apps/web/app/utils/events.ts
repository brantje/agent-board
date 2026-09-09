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

const knownFamilies = new Set(['run', 'agent', 'runtime', 'workspace', 'question', 'decision', 'tool', 'file', 'test', 'artifact', 'issue', 'review', 'git'])

export type AgentTextActivityItem = {
  kind: 'thought' | 'message'
  id: string
  occurredAt: string
  sequence: number | null
  message: string
}

export type TodoActivityItem = {
  content: string
  status: 'pending' | 'in_progress' | 'completed' | 'cancelled'
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
  todos?: TodoActivityItem[]
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
  'workspace.transfer.progress',
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

function workspaceTransferDirection(direction: unknown) {
  if (direction === 'to_runner') return 'To runner'
  if (direction === 'from_runner') return 'From runner'
  return typeof direction === 'string' && direction.trim() ? direction : undefined
}

function formatTransferBytes(value: unknown) {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) return undefined
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`
  return `${(value / (1024 * 1024)).toFixed(1)} MiB`
}

export function eventDescription(event: EventEvidence) {
  const payload = event.payload || {}
  if (event.type.startsWith('workspace.transfer.')) {
    const direction = workspaceTransferDirection(payload.direction)
    if (event.type === 'workspace.transfer.failed' && typeof payload.reason === 'string' && payload.reason.trim()) {
      return direction ? `${direction}: ${payload.reason}` : payload.reason
    }
    const transferred = formatTransferBytes(payload.bytesTransferred)
    const total = formatTransferBytes(payload.totalBytes)
    if (transferred && total) {
      const size = transferred === total ? transferred : `${transferred} / ${total}`
      return direction ? `${direction} · ${size}` : size
    }
    return direction || ''
  }
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
  glob: 'Search',
  todowrite: 'Todo',
  todo_write: 'Todo'
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
  glob: 'i-lucide-search',
  todowrite: 'i-lucide-list-todo',
  todo_write: 'i-lucide-list-todo'
}

export function toolActivityIcon(name: string) {
  return toolIcons[name.trim().toLowerCase()] || 'i-lucide-wrench'
}

const eventFamilyIcons: Record<string, string> = {
  run: 'i-lucide-play',
  runtime: 'i-lucide-box',
  workspace: 'i-lucide-folder-sync',
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

function relatedQuestion(items: RunActivityItem[], questionIndexes: Map<string, number>, questionId: string | undefined) {
  if (!questionId) return undefined
  const existingIndex = questionIndexes.get(questionId)
  if (existingIndex == null) return undefined
  const existing = items[existingIndex]
  return existing?.kind === 'question' ? existing : undefined
}

function decisionRecordedDescription(question: QuestionActivityItem | undefined) {
  if (!question) return ''
  return question.answer ? `${question.prompt} — ${question.answer}` : question.prompt
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

function isTodoToolName(name: string) {
  const normalized = name.trim().toLowerCase()
  return normalized === 'todowrite' || normalized === 'todo_write'
}

function normalizeTodoStatus(value: unknown): TodoActivityItem['status'] {
  const raw = typeof value === 'string' ? value.trim().toLowerCase().replaceAll(' ', '_').replaceAll('-', '_') : ''
  if (raw === 'in_progress' || raw === 'inprogress' || raw === 'running' || raw === 'active') return 'in_progress'
  if (raw === 'completed' || raw === 'complete' || raw === 'done') return 'completed'
  if (raw === 'cancelled' || raw === 'canceled') return 'cancelled'
  return 'pending'
}

function parseTodoItems(value: unknown): TodoActivityItem[] | undefined {
  if (Array.isArray(value)) {
    const items = value.flatMap((entry) => {
      if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return []
      const record = entry as Record<string, unknown>
      const content = stringValue(record.content) || stringValue(record.text) || stringValue(record.title)
      if (!content) return []
      const status = record.status ?? record.state
      return [{ content, status: normalizeTodoStatus(status) }]
    })
    return items.length ? items : undefined
  }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  const record = value as Record<string, unknown>
  return parseTodoItems(record.todos ?? record.result ?? record.items)
}

function parseTodoPreview(resultPreview: string | undefined) {
  if (!resultPreview) return undefined
  try {
    return parseTodoItems(JSON.parse(resultPreview))
  } catch {
    return undefined
  }
}

function parseToolTodos(input: Record<string, unknown> | undefined, resultPreview: string | undefined) {
  const fromPreview = parseTodoPreview(resultPreview)
  if (fromPreview) return fromPreview
  return input ? parseTodoItems(input.todos) : undefined
}

function mergeTodoStatuses(current: TodoActivityItem[], latest: TodoActivityItem[]) {
  const latestByContent = new Map(latest.map(todo => [todo.content, todo.status]))
  const shared = current.some(todo => latestByContent.has(todo.content))
  if (!shared) return current
  const merged = current.map(todo => ({
    ...todo,
    status: latestByContent.get(todo.content) ?? todo.status
  }))
  for (const todo of latest) {
    if (!merged.some(item => item.content === todo.content)) merged.push(todo)
  }
  return merged
}

function applyLatestTodoSnapshots(items: RunActivityItem[]) {
  const indexes = items.flatMap((item, index) => (
    item.kind === 'tool' && isTodoToolName(item.name) && item.todos?.length ? [index] : []
  ))
  const latestIndex = indexes.at(-1)
  if (latestIndex == null || indexes.length < 2) return items
  const latestItem = items[latestIndex]
  if (latestItem?.kind !== 'tool' || !latestItem.todos?.length) return items
  const latest = latestItem.todos
  const next = [...items]
  for (const priorIndex of indexes.slice(0, -1)) {
    const prior = next[priorIndex]
    if (prior?.kind !== 'tool' || !prior.todos?.length) continue
    const todos = mergeTodoStatuses(prior.todos, latest)
    next[priorIndex] = { ...prior, todos, target: todoProgressSummary(todos) }
  }
  return next
}

export function todoProgressSummary(todos: TodoActivityItem[]) {
  const completed = todos.filter(item => item.status === 'completed').length
  return `${completed}/${todos.length}`
}

function finalizeToolPresentation(
  name: string,
  fields: Record<string, unknown>,
  input: Record<string, unknown> | undefined,
  resultPreview: string | undefined,
  summary: string | undefined,
  existing?: ToolActivityItem
) {
  const mergedInput = input || existing?.input
  const mergedPreview = resultPreview || existing?.resultPreview
  const todos = isTodoToolName(name)
    ? parseToolTodos(mergedInput, mergedPreview) || existing?.todos
    : undefined
  const target = todos?.length
    ? todoProgressSummary(todos)
    : toolTarget(fields, input) || existing?.target || ''
  return {
    label: toolActivityLabel(name),
    target,
    todos,
    input: mergedInput,
    summary: summary || existing?.summary,
    resultPreview: mergedPreview
  }
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
          const resolvedName = stringValue(fields.name) || existing.name
          const presentation = finalizeToolPresentation(
            resolvedName,
            fields,
            input,
            stringValue(fields.resultPreview),
            stringValue(fields.summary),
            existing
          )
          items[existingIndex] = {
            ...existing,
            name: resolvedName,
            ...presentation,
            status,
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
      const presentation = finalizeToolPresentation(
        name,
        fields,
        input,
        stringValue(fields.resultPreview),
        stringValue(fields.summary)
      )
      items.push({
        kind: 'tool',
        id: event.id,
        occurredAt: event.occurredAt,
        sequence: event.sequence,
        toolCallId,
        name,
        ...presentation,
        status,
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
      description: event.type === 'decision.recorded'
        ? decisionRecordedDescription(relatedQuestion(items, questionIndexes, stringValue(payload.questionId)))
        : eventDescription(event),
      event,
      unknown: isUnknownEvent(event)
    })
  }
  return applyLatestTodoSnapshots(reorderThoughtsBeforeTools(items))
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