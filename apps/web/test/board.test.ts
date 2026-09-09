import { describe, expect, it } from 'vitest'
import { boardColumnSurface, boardColumns, editableStatuses, issuePriority, issueStatusPresentation, latestRun, statusLabel } from '../app/utils/issues'

const issue = (id: string, status: string) => ({
  id,
  status,
  title: id,
  projectId: 'p',
  description: '',
  assignedAgentId: null,
  createdAt: '',
  updatedAt: ''
})

describe('durable Issue board projection', () => {
  it('keeps six Issue states separate from Run states and filters by title, id, or description', () => {
    const columns = boardColumns([issue('First', 'BLOCKED'), issue('Second', 'TODO')], 'first')
    expect(columns.map(column => column.status)).toEqual(['BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED', 'REVIEW', 'DONE'])
    expect(columns.map(column => column.label)).toEqual(['Backlog', 'Todo', 'In Progress', 'Blocked', 'Review', 'Done'])
    expect(columns.find(column => column.status === 'BLOCKED')?.issues.map(value => value.id)).toEqual(['First'])
    expect(columns.find(column => column.status === 'TODO')?.issues).toEqual([])
    expect(boardColumns([issue('abc-123', 'TODO')], 'ABC-123')[1]?.issues).toHaveLength(1)
    expect(boardColumns([{ ...issue('AB-9', 'TODO'), title: 'Hidden', description: 'WebSocket notification system' }], 'websocket')[1]?.issues).toHaveLength(1)
  })

  it('maps each Board column to a named surface token without relying on color alone', () => {
    expect(boardColumnSurface('BACKLOG')).toBe('board-column board-column-backlog')
    expect(boardColumnSurface('TODO')).toBe('board-column board-column-todo')
    expect(boardColumnSurface('IN_PROGRESS')).toBe('board-column board-column-in-progress')
    expect(boardColumnSurface('BLOCKED')).toBe('board-column board-column-blocked')
    expect(boardColumnSurface('REVIEW')).toBe('board-column board-column-review')
    expect(boardColumnSurface('DONE')).toBe('board-column board-column-done')
  })

  it('gives each Board column a distinct icon and semantic color', () => {
    expect(issueStatusPresentation('BACKLOG')).toMatchObject({ icon: 'i-lucide-inbox', color: 'neutral', textClass: 'text-muted' })
    expect(issueStatusPresentation('TODO')).toMatchObject({ icon: 'i-lucide-circle', color: 'neutral', textClass: 'text-muted' })
    expect(issueStatusPresentation('IN_PROGRESS')).toMatchObject({ icon: 'i-lucide-play', color: 'warning', textClass: 'text-warning' })
    expect(issueStatusPresentation('BLOCKED')).toMatchObject({ icon: 'i-lucide-octagon-alert', color: 'error', textClass: 'text-error' })
    expect(issueStatusPresentation('REVIEW')).toMatchObject({ icon: 'i-lucide-scan-eye', color: 'success', textClass: 'text-success' })
    expect(issueStatusPresentation('DONE')).toMatchObject({ icon: 'i-lucide-circle-check', color: 'primary', textClass: 'text-primary' })
    expect(issueStatusPresentation('UNKNOWN')).toMatchObject({ icon: 'i-lucide-circle', color: 'neutral', label: 'Unknown' })
    expect(new Set(['BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED', 'REVIEW', 'DONE'].map(status => issueStatusPresentation(status).icon)).size).toBe(6)
  })

  it('projects Issue priority as labeled Low/High chips without relying on color alone', () => {
    expect(issuePriority(0)).toMatchObject({ label: 'Low', variant: 'subtle', icon: 'i-lucide-signal-low' })
    expect(issuePriority(2)).toMatchObject({ label: 'Medium', variant: 'subtle', icon: 'i-lucide-signal-medium' })
    expect(issuePriority(3)).toMatchObject({ label: 'High', variant: 'solid', icon: 'i-lucide-signal-high' })
    expect(issuePriority(4)).toMatchObject({ label: 'Highest', variant: 'solid', icon: 'i-lucide-signal' })
    expect(issuePriority(9)).toMatchObject({ label: 'Priority 9', variant: 'subtle' })
  })

  it('protects Review and Done and reopens only into Todo', () => {
    expect(editableStatuses('REVIEW')).toEqual(['REVIEW'])
    expect(editableStatuses('DONE')).toEqual(['DONE', 'TODO'])
    expect(editableStatuses('TODO')).not.toContain('DONE')
    expect(editableStatuses()).toEqual(['BACKLOG', 'TODO'])
  })

  it('selects latest persisted attempt for the same Issue only', () => {
    expect(latestRun([
      { id: 'old', issueId: 'i', attempt: 1 },
      { id: 'other', issueId: 'other', attempt: 8 },
      { id: 'latest', issueId: 'i', attempt: 2 }
    ] as never, 'i')?.id).toBe('latest')
    expect(latestRun([], 'i')).toBeUndefined()
  })

  it('formats Issue and Run states as readable text without relying on color', () => {
    expect(statusLabel('WAITING_FOR_INPUT')).toBe('Waiting For Input')
    expect(statusLabel('IN_PROGRESS')).toBe('In Progress')
  })
})
