import type { ArtifactEvidence, EventEvidence, RunEvidence } from '../types/api'
import { apiPath, apiText } from './api'

export type CandidateFileItem = {
  path: string
  oldPath?: string
  changeType: 'created' | 'modified' | 'deleted' | 'renamed'
  artifactId?: string
}

export type CandidateFileStats = {
  added: number
  removed: number
}

const changeTypeByEvent: Record<string, CandidateFileItem['changeType']> = {
  'file.created': 'created',
  'file.modified': 'modified',
  'file.deleted': 'deleted',
  'file.renamed': 'renamed'
}

export function buildCandidateFileItems(fileChanges: EventEvidence[]): CandidateFileItem[] {
  const items = fileChanges.flatMap((event) => {
    const changeType = changeTypeByEvent[event.type]
    const path = typeof event.payload?.path === 'string' ? event.payload.path : undefined
    if (!changeType || !path) return []
    const oldPath = typeof event.payload?.oldPath === 'string' ? event.payload.oldPath : undefined
    const artifactId = typeof event.payload?.artifactId === 'string' ? event.payload.artifactId : undefined
    return [{ path, oldPath, changeType, artifactId }]
  })
  return items.sort((left, right) => left.path.localeCompare(right.path))
}

export function parseUnifiedDiffStats(patch: string): Map<string, CandidateFileStats> {
  const stats = new Map<string, CandidateFileStats>()
  let currentPath = ''
  for (const line of patch.split('\n')) {
    if (line.startsWith('+++ ')) {
      const raw = line.slice(4).trim()
      currentPath = raw === '/dev/null' ? '' : raw.replace(/^(?:a|b)\//, '')
      if (currentPath && !stats.has(currentPath)) {
        stats.set(currentPath, { added: 0, removed: 0 })
      }
      continue
    }
    if (!currentPath || line.startsWith('@@') || line.startsWith('diff ') || line.startsWith('---') || line.startsWith('index ')) {
      continue
    }
    const entry = stats.get(currentPath)
    if (!entry) continue
    if (line.startsWith('+')) entry.added += 1
    else if (line.startsWith('-')) entry.removed += 1
  }
  return stats
}

export function mergeDiffStats(...sources: Array<Map<string, CandidateFileStats>>): Map<string, CandidateFileStats> {
  const merged = new Map<string, CandidateFileStats>()
  for (const source of sources) {
    for (const [path, stats] of source) {
      const existing = merged.get(path) || { added: 0, removed: 0 }
      merged.set(path, { added: existing.added + stats.added, removed: existing.removed + stats.removed })
    }
  }
  return merged
}

export function countAddedLines(content: string) {
  if (!content) return 0
  const normalized = content.endsWith('\n') ? content.slice(0, -1) : content
  if (!normalized) return 0
  return normalized.split('\n').length
}

function artifactByName(artifacts: ArtifactEvidence[], name: string) {
  return artifacts.find(artifact => artifact.name === name)
}

function artifactById(artifacts: ArtifactEvidence[], id: string) {
  return artifacts.find(artifact => artifact.id === id)
}

export async function loadReviewFileStats(
  projectId: string,
  evidence: RunEvidence,
  fetchText: (path: string) => Promise<string> = path => apiText(path)
) {
  const runId = evidence.run.id
  const artifactPath = (artifact: ArtifactEvidence) => `${apiPath('runs', projectId, runId)}/artifacts/${artifact.id}`
  const patchSources: Array<Map<string, CandidateFileStats>> = []

  for (const name of ['candidate-staged.patch', 'candidate-unstaged.patch']) {
    const artifact = artifactByName(evidence.artifacts, name)
    if (!artifact) continue
    const patch = await fetchText(artifactPath(artifact))
    if (patch.trim()) patchSources.push(parseUnifiedDiffStats(patch))
  }

  const stats = mergeDiffStats(...patchSources)

  for (const item of buildCandidateFileItems(evidence.fileChanges)) {
    if (stats.has(item.path) || item.changeType === 'deleted') continue
    const artifact = item.artifactId ? artifactById(evidence.artifacts, item.artifactId) : artifactByName(evidence.artifacts, item.path)
    if (!artifact || artifact.kind !== 'candidate_file') continue
    const content = await fetchText(artifactPath(artifact))
    stats.set(item.path, { added: countAddedLines(content), removed: 0 })
  }

  return stats
}

export function formatCandidateFileStats(stats?: CandidateFileStats) {
  if (!stats || (stats.added === 0 && stats.removed === 0)) return ''
  const parts: string[] = []
  if (stats.added > 0) parts.push(`+${stats.added}`)
  if (stats.removed > 0) parts.push(`-${stats.removed}`)
  return parts.join(' ')
}

export type CandidateFileTreeFile = {
  kind: 'file'
  path: string
  name: string
  oldPath?: string
  changeType: CandidateFileItem['changeType']
  stats?: CandidateFileStats
}

export type CandidateFileTreeFolder = {
  kind: 'folder'
  name: string
  path: string
  children: CandidateFileTreeNode[]
  stats: CandidateFileStats
}

export type CandidateFileTreeNode = CandidateFileTreeFile | CandidateFileTreeFolder

export type CandidateFileTreeRow =
  | {
    kind: 'folder'
    path: string
    name: string
    depth: number
    stats: CandidateFileStats
    expanded: boolean
  }
  | {
    kind: 'file'
    path: string
    name: string
    depth: number
    oldPath?: string
    changeType: CandidateFileItem['changeType']
    stats?: CandidateFileStats
  }

type FolderBuilder = {
  name: string
  path: string
  folders: Map<string, FolderBuilder>
  files: CandidateFileTreeFile[]
}

export function sumCandidateFileStats(stats: Map<string, CandidateFileStats>): CandidateFileStats {
  let added = 0
  let removed = 0
  for (const value of stats.values()) {
    added += value.added
    removed += value.removed
  }
  return { added, removed }
}

function sumTreeNodeStats(nodes: CandidateFileTreeNode[]): CandidateFileStats {
  let added = 0
  let removed = 0
  for (const node of nodes) {
    if (node.kind === 'file') {
      added += node.stats?.added ?? 0
      removed += node.stats?.removed ?? 0
      continue
    }
    added += node.stats.added
    removed += node.stats.removed
  }
  return { added, removed }
}

function sortTreeNodes(nodes: CandidateFileTreeNode[]) {
  return [...nodes].sort((left, right) => left.name.localeCompare(right.name))
}

function finalizeFolderBuilder(builder: FolderBuilder): CandidateFileTreeFolder {
  const children = sortTreeNodes([
    ...[...builder.folders.values()].map(finalizeFolderBuilder),
    ...builder.files
  ])
  return {
    kind: 'folder',
    name: builder.name,
    path: builder.path,
    children,
    stats: sumTreeNodeStats(children)
  }
}

function insertCandidateFile(root: FolderBuilder, item: CandidateFileItem, stats: Map<string, CandidateFileStats>) {
  const parts = item.path.split('/').filter(Boolean)
  const fileName = parts.pop()
  if (!fileName) return

  const file: CandidateFileTreeFile = {
    kind: 'file',
    path: item.path,
    name: fileName,
    oldPath: item.oldPath,
    changeType: item.changeType,
    stats: stats.get(item.path)
  }

  if (!parts.length) {
    root.files.push(file)
    return
  }

  let current = root
  let currentPath = ''
  for (const part of parts) {
    currentPath = currentPath ? `${currentPath}/${part}` : part
    let folder = current.folders.get(part)
    if (!folder) {
      folder = { name: part, path: currentPath, folders: new Map(), files: [] }
      current.folders.set(part, folder)
    }
    current = folder
  }
  current.files.push(file)
}

export function buildCandidateFileTree(
  items: CandidateFileItem[],
  stats: Map<string, CandidateFileStats> = new Map()
): CandidateFileTreeNode[] {
  const root: FolderBuilder = { name: '', path: '', folders: new Map(), files: [] }
  for (const item of items) insertCandidateFile(root, item, stats)

  return sortTreeNodes([
    ...[...root.folders.values()].map(finalizeFolderBuilder),
    ...root.files
  ])
}

export function flattenCandidateFileTree(
  nodes: CandidateFileTreeNode[],
  collapsed: ReadonlySet<string> = new Set(),
  depth = 0
): CandidateFileTreeRow[] {
  const rows: CandidateFileTreeRow[] = []
  for (const node of nodes) {
    if (node.kind === 'folder') {
      const expanded = !collapsed.has(node.path)
      rows.push({
        kind: 'folder',
        path: node.path,
        name: node.name,
        depth,
        stats: node.stats,
        expanded
      })
      if (expanded) rows.push(...flattenCandidateFileTree(node.children, collapsed, depth + 1))
      continue
    }
    rows.push({
      kind: 'file',
      path: node.path,
      name: node.name,
      depth,
      oldPath: node.oldPath,
      changeType: node.changeType,
      stats: node.stats
    })
  }
  return rows
}
