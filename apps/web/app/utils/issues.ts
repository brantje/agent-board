import type { Issue, Run } from '../types/api'

export const issueStatuses = ['BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED', 'REVIEW', 'DONE'] as const

export function statusLabel(status: string) {
  return status
    .toLowerCase()
    .replaceAll('_', ' ')
    .replace(/(^|\s)\S/g, value => value.toUpperCase())
}

export function boardColumns(issues: Issue[], search = '') {
  const query = search.trim().toLowerCase()
  return issueStatuses.map(status => ({
    status,
    label: statusLabel(status),
    issues: issues.filter(issue => issue.status === status && `${issue.title} ${issue.id}`.toLowerCase().includes(query))
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
