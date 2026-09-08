import type { Issue, Run } from '../types/api'

export const issueStatuses = ['BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED', 'REVIEW', 'DONE'] as const

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

export function boardColumns(issues: Issue[], search = '') {
  const query = search.trim().toLowerCase()
  return issueStatuses.map(status => ({
    status,
    label: statusLabel(status),
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
