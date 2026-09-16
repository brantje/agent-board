import { describe, expect, it } from 'vitest'
import type { Issue } from '../app/types/api'
import { boardDragId, boardDropZoneId, issueIdFromBoardDragId, parseBoardDropZoneId, previewBoardPlacement } from '../app/utils/board-order'

function issue(id: string, status: string): Issue {
  return {
    id,
    projectId: 'p',
    number: 1,
    title: id,
    description: '',
    status,
    priority: 0,
    assignedTo: null,
    createdBy: null,
    createdAt: '',
    updatedAt: '',
    currentBranch: null,
    lastEvent: null
  }
}

describe('board placement previews', () => {
  it('round-trips dnd identifiers without confusing issues and gaps', () => {
    expect(issueIdFromBoardDragId(boardDragId('AB-12'))).toBe('AB-12')
    expect(parseBoardDropZoneId(boardDropZoneId('IN_PROGRESS', 3))).toEqual({ status: 'IN_PROGRESS', index: 3 })
    expect(issueIdFromBoardDragId('board-gap:TODO:0')).toBeNull()
    expect(parseBoardDropZoneId('board-issue:AB-12')).toBeNull()
  })

  it('derives exact same-column neighbors after removing the dragged issue', () => {
    const issues = [issue('A', 'TODO'), issue('B', 'TODO'), issue('C', 'TODO')]
    const moved = previewBoardPlacement(issues, 'B', 'TODO', 3)!
    expect(moved.issues.filter(value => value.status === 'TODO').map(value => value.id)).toEqual(['A', 'C', 'B'])
    expect(moved.beforeId).toBe('C')
    expect(moved.afterId).toBeNull()
    expect(moved.changed).toBe(true)
  })

  it('never emits the moved issue as its own anchor for a two-card column', () => {
    const issues = [issue('TT-1', 'TODO'), issue('TT-2', 'TODO')]

    for (const movedId of ['TT-1', 'TT-2']) {
      for (const dropIndex of [0, 1, 2]) {
        const preview = previewBoardPlacement(issues, movedId, 'TODO', dropIndex)
        expect(preview).not.toBeNull()
        expect(preview?.beforeId).not.toBe(movedId)
        expect(preview?.afterId).not.toBe(movedId)
      }
    }
  })

  it('derives exact cross-column anchors including empty destinations', () => {
    const issues = [issue('A', 'TODO'), issue('R1', 'REVIEW'), issue('R2', 'REVIEW')]
    const middle = previewBoardPlacement(issues, 'A', 'REVIEW', 1)!
    expect(middle.beforeId).toBe('R1')
    expect(middle.afterId).toBe('R2')
    expect(middle.issues.filter(value => value.status === 'REVIEW').map(value => value.id)).toEqual(['R1', 'A', 'R2'])

    const empty = previewBoardPlacement([issue('A', 'TODO')], 'A', 'DONE', 0)!
    expect(empty.beforeId).toBeNull()
    expect(empty.afterId).toBeNull()
    expect(empty.issues[0]?.status).toBe('DONE')
  })

  it('never emits the moved issue as its own cross-column anchor', () => {
    const issues = [issue('TT-2', 'TODO'), issue('TT-1', 'REVIEW')]
    for (const dropIndex of [0, 1]) {
      const preview = previewBoardPlacement(issues, 'TT-2', 'REVIEW', dropIndex)
      expect(preview).not.toBeNull()
      expect(preview?.beforeId).not.toBe('TT-2')
      expect(preview?.afterId).not.toBe('TT-2')
    }
  })

  it('recognizes a drop into the current slot as a no-op', () => {
    const issues = [issue('A', 'TODO'), issue('B', 'TODO'), issue('C', 'TODO')]
    const moved = previewBoardPlacement(issues, 'B', 'TODO', 1)!
    expect(moved.changed).toBe(false)
    expect(moved.beforeId).toBe('A')
    expect(moved.afterId).toBe('C')
  })
})
