import type { Issue } from '../types/api'

const dragPrefix = 'board-issue:'
const dropPrefix = 'board-gap:'

export type BoardPlacementPreview = {
  issues: Issue[]
  beforeId: string | null
  afterId: string | null
  sourceStatus: string
  destinationStatus: string
  changed: boolean
}

export function boardDragId(issueId: string) {
  return `${dragPrefix}${issueId}`
}

export function issueIdFromBoardDragId(value: string) {
  return value.startsWith(dragPrefix) ? value.slice(dragPrefix.length) : null
}

export function boardDropZoneId(status: string, index: number) {
  return `${dropPrefix}${status}:${index}`
}

export function parseBoardDropZoneId(value: string) {
  if (!value.startsWith(dropPrefix)) return null
  const body = value.slice(dropPrefix.length)
  const separator = body.lastIndexOf(':')
  if (separator <= 0) return null
  const status = body.slice(0, separator)
  const index = Number(body.slice(separator + 1))
  if (!Number.isInteger(index) || index < 0) return null
  return { status, index }
}

export function previewBoardPlacement(issues: Issue[], issueId: string, destinationStatus: string, dropIndex: number): BoardPlacementPreview | null {
  const dragged = issues.find(issue => issue.id === issueId)
  if (!dragged) return null

  const originalDestination = issues.filter(issue => issue.status === destinationStatus)
  const sourceIndex = dragged.status === destinationStatus
    ? originalDestination.findIndex(issue => issue.id === issueId)
    : -1
  let insertionIndex = dropIndex
  if (sourceIndex >= 0 && sourceIndex < dropIndex) insertionIndex -= 1

  const remaining = issues.filter(issue => issue.id !== issueId)
  const destination = remaining.filter(issue => issue.status === destinationStatus)
  insertionIndex = Math.max(0, Math.min(insertionIndex, destination.length))
  const beforeId = insertionIndex > 0 ? destination[insertionIndex - 1]!.id : null
  const afterId = insertionIndex < destination.length ? destination[insertionIndex]!.id : null
  const moved = { ...dragged, status: destinationStatus }

  let next = [...remaining]
  if (afterId) {
    const fullIndex = next.findIndex(issue => issue.id === afterId)
    next.splice(fullIndex, 0, moved)
  } else if (beforeId) {
    const fullIndex = next.findIndex(issue => issue.id === beforeId)
    next.splice(fullIndex + 1, 0, moved)
  } else {
    next.push(moved)
  }

  const changed = next.length !== issues.length || next.some((issue, index) => issue.id !== issues[index]?.id || issue.status !== issues[index]?.status)
  return { issues: next, beforeId, afterId, sourceStatus: dragged.status, destinationStatus, changed }
}
