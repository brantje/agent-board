import { issueStatusPresentation, statusLabel } from './issues'

const runActivityLabels: Record<string, string> = {
  QUEUED: 'Agent is queued',
  STARTING: 'Agent is starting',
  RUNNING: 'Agent is working',
  WAITING_FOR_INPUT: 'Agent needs input',
  PAUSED: 'Run paused',
  READY_FOR_REVIEW: 'Ready for review',
  COMPLETED: 'Run completed',
  FAILED: 'Run failed',
  CANCELLED: 'Run cancelled'
}

const runBoardStatus: Record<string, { board: string; icon?: string; live?: boolean }> = {
  QUEUED: { board: 'TODO' },
  STARTING: { board: 'IN_PROGRESS', live: true },
  RUNNING: { board: 'IN_PROGRESS', live: true },
  WAITING_FOR_INPUT: { board: 'BLOCKED' },
  PAUSED: { board: 'BLOCKED', icon: 'i-lucide-pause' },
  READY_FOR_REVIEW: { board: 'REVIEW' },
  COMPLETED: { board: 'DONE' },
  FAILED: { board: 'BLOCKED', icon: 'i-lucide-circle-x' },
  CANCELLED: { board: 'BACKLOG', icon: 'i-lucide-ban' }
}

export function runStatusLabel(status: string) {
  return `Run · ${statusLabel(status)}`
}

export function runActivityStatusLabel(status: string) {
  return runActivityLabels[status] || runStatusLabel(status)
}

export function runStatusPresentation(status: string) {
  const mapping = runBoardStatus[status]
  if (!mapping) {
    return {
      label: runActivityStatusLabel(status),
      icon: 'i-lucide-circle',
      color: 'neutral' as const,
      textClass: 'text-muted',
      boardStatus: null,
      live: false
    }
  }
  const board = issueStatusPresentation(mapping.board)
  return {
    label: runActivityStatusLabel(status),
    icon: mapping.icon ?? board.icon,
    color: board.color,
    textClass: board.textClass,
    boardStatus: mapping.board,
    live: mapping.live === true
  }
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

export interface RunAgentInfo {
  name?: string
  engine?: string
  model?: string
  provider?: string
  runtime?: string
}

function recordValue(value: unknown): Record<string, unknown> | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  return value as Record<string, unknown>
}

function textValue(value: unknown) {
  if (typeof value !== 'string') return undefined
  const trimmed = value.trim()
  return trimmed || undefined
}

export function runAgentInfo(provenance: Record<string, unknown> | null | undefined): RunAgentInfo | null {
  const root = recordValue(provenance)
  if (!root) return null
  const context = recordValue(root.context) ?? root
  const agent = recordValue(context.agent)
  const model = recordValue(context.model)
  const provider = recordValue(context.provider)
  const runtime = recordValue(context.runtime)
  const info: RunAgentInfo = {
    name: textValue(agent?.name),
    engine: textValue(agent?.engine),
    model: textValue(model?.model) || textValue(model?.name),
    provider: textValue(provider?.name) || textValue(provider?.kind),
    runtime: textValue(runtime?.name) || textValue(runtime?.kind)
  }
  if (!info.name && !info.engine && !info.model && !info.provider && !info.runtime) return null
  return info
}
