import { describe, expect, it } from 'vitest'
import { boardColumns, editableStatuses, latestRun, statusLabel } from '../app/utils/issues'

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
  it('keeps six Issue states separate from Run states and filters by title', () => {
    const columns = boardColumns([issue('First', 'BLOCKED'), issue('Second', 'TODO')], 'first')
    expect(columns.map(column => column.status)).toEqual(['BACKLOG', 'TODO', 'IN_PROGRESS', 'BLOCKED', 'REVIEW', 'DONE'])
    expect(columns.map(column => column.label)).toEqual(['Backlog', 'Todo', 'In Progress', 'Blocked', 'Review', 'Done'])
    expect(columns.find(column => column.status === 'BLOCKED')?.issues.map(value => value.id)).toEqual(['First'])
    expect(columns.find(column => column.status === 'TODO')?.issues).toEqual([])
    expect(boardColumns([issue('abc-123', 'TODO')], 'ABC-123')[1]?.issues).toHaveLength(1)
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
