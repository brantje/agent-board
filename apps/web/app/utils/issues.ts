import type { Issue, Run } from '../types/api'

export const issueStatuses = ['BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED', 'REVIEW', 'DONE'] as const

export type StatusColor = 'neutral' | 'warning' | 'error' | 'primary' | 'success'

const issueStatusPresentations: Record<typeof issueStatuses[number], { icon: string; color: StatusColor; textClass: string }> = {
  BACKLOG: { icon: 'i-lucide-inbox', color: 'neutral', textClass: 'text-muted' },
  TODO: { icon: 'i-lucide-circle', color: 'neutral', textClass: 'text-muted' },
  IN_PROGRESS: { icon: 'i-lucide-play', color: 'warning', textClass: 'text-warning' },
  BLOCKED: { icon: 'i-lucide-octagon-alert', color: 'error', textClass: 'text-error' },
  REVIEW: { icon: 'i-lucide-scan-eye', color: 'success', textClass: 'text-success' },
  DONE: { icon: 'i-lucide-circle-check', color: 'primary', textClass: 'text-primary' }
}

const issuePriorities = [
  { label: 'Low', variant: 'subtle' as const, icon: 'i-lucide-signal-low' },
  { label: 'Low', variant: 'subtle' as const, icon: 'i-lucide-signal-low' },
  { label: 'Medium', variant: 'subtle' as const, icon: 'i-lucide-signal-medium' },
  { label: 'High', variant: 'solid' as const, icon: 'i-lucide-signal-high' },
  { label: 'Highest', variant: 'solid' as const, icon: 'i-lucide-signal' }
] as const

export function issuePriority(priority: number) {
  return issuePriorities[priority] ?? { label: `Priority ${priority}`, variant: 'subtle' as const, icon: 'i-lucide-signal-low' }
}

export function statusLabel(status: string) {
  return status
    .toLowerCase()
    .replaceAll('_', ' ')
    .replace(/(^|\s)\S/g, value => value.toUpperCase())
}

export function boardColumnSurface(status: string) {
  return `board-column board-column-${status.toLowerCase().replaceAll('_', '-')}`
}

export function issueStatusPresentation(status: string) {
  const presentation = issueStatusPresentations[status as typeof issueStatuses[number]]
  return {
    label: statusLabel(status),
    icon: presentation?.icon ?? 'i-lucide-circle',
    color: presentation?.color ?? 'neutral' as const,
    textClass: presentation?.textClass ?? 'text-muted',
    surface: boardColumnSurface(status)
  }
}

export function boardColumns(issues: Issue[], search = '') {
  const query = search.trim().toLowerCase()
  return issueStatuses.map(status => ({
    status,
    ...issueStatusPresentation(status),
    issues: issues.filter(issue => issue.status === status && `${issue.title} ${issue.id} ${issue.description}`.toLowerCase().includes(query))
  }))
}

export function editableStatuses(status?: string): string[] {
  if (!status) return ['BACKLOG', 'TODO']
  if (status === 'REVIEW') return ['REVIEW']
  if (status === 'DONE') return ['DONE', 'TODO']
  return ['BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED']
}

export function latestRun(runs: Run[], issueId: string) {
  return runs
    .filter(run => run.issueId === issueId)
    .sort((a, b) => b.attempt - a.attempt)[0]
}

const terminalRunStatuses = new Set(['COMPLETED', 'CANCELLED'])

export function issueCardRunStatus(runStatus?: string | null) {
  if (!runStatus || terminalRunStatuses.has(runStatus)) return null
  return statusLabel(runStatus)
}

export function isLiveIssueRun(runStatus?: string | null) {
  return runStatus === 'RUNNING' || runStatus === 'STARTING'
}

export function isFailedIssueRun(runStatus?: string | null) {
  return runStatus === 'FAILED'
}

const updatedFormatter = new Intl.RelativeTimeFormat('en', { numeric: 'always', style: 'narrow' })

export function formatUpdatedLabel(updatedAt: string, now = Date.now()) {
  const date = new Date(updatedAt)
  if (Number.isNaN(date.getTime())) return ''
  const diffMs = date.getTime() - now
  const minute = 60_000
  const hour = 60 * minute
  const day = 24 * hour
  const abs = Math.abs(diffMs)
  if (abs < hour) {
    const minutes = diffMs === 0 ? -1 : Math.round(diffMs / minute)
    const value = minutes === 0 ? -1 : minutes
    return `Updated ${updatedFormatter.format(value, 'minute')}`
  }
  if (abs < day) {
    const hours = Math.round(diffMs / hour)
    const value = hours === 0 ? -1 : hours
    return `Updated ${updatedFormatter.format(value, 'hour')}`
  }
  const days = Math.round(diffMs / day)
  const value = days === 0 ? -1 : days
  return `Updated ${updatedFormatter.format(value, 'day')}`
}
