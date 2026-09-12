import { describe, expect, it } from 'vitest'
import type { ArtifactEvidence, EventEvidence, RunEvidence } from '../app/types/api'
import { event } from './execution-fixtures'
import {
  buildCandidateFileItems,
  buildCandidateFileTree,
  countAddedLines,
  flattenCandidateFileTree,
  loadReviewFileStats,
  mergeDiffStats,
  parseUnifiedDiffStats,
  statsFromFileChanges,
  sumCandidateFileStats
} from '../app/utils/candidate-files'

describe('candidate-files', () => {
  it('builds sorted changed-file rows from file change events', () => {
    const fileChanges = [
      event({ id: 'b', type: 'file.modified', sequence: 2, payload: { path: 'src/b.ts' } }),
      event({ id: 'a', type: 'file.created', sequence: 1, payload: { path: 'index.html', artifactId: 'art-1' } }),
      event({ id: 'c', type: 'file.renamed', sequence: 3, payload: { path: 'new.txt', oldPath: 'old.txt' } })
    ]
    expect(buildCandidateFileItems(fileChanges)).toEqual([
      { path: 'index.html', changeType: 'created', artifactId: 'art-1', oldPath: undefined },
      { path: 'new.txt', oldPath: 'old.txt', changeType: 'renamed', artifactId: undefined },
      { path: 'src/b.ts', changeType: 'modified', oldPath: undefined, artifactId: undefined }
    ])
  })

  it('parses unified diff line stats per file', () => {
    const patch = `diff --git a/index.html b/index.html
--- a/index.html
+++ b/index.html
@@ -1,2 +1,4 @@
 line
+added one
+added two
-removed
`
    expect(parseUnifiedDiffStats(patch).get('index.html')).toEqual({ added: 2, removed: 1 })
  })

  it('merges staged and unstaged diff stats for the same path', () => {
    const merged = mergeDiffStats(
      parseUnifiedDiffStats('diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1,2 @@\n-old\n+new\n'),
      parseUnifiedDiffStats('diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1,3 @@\n+extra\n')
    )
    expect(merged.get('a.txt')).toEqual({ added: 2, removed: 1 })
  })

  it('counts added lines for new untracked file content', () => {
    expect(countAddedLines('one\n\ntwo\n')).toBe(3)
  })

  it('builds a nested folder tree and flattens it with collapse state', () => {
    const stats = new Map([
      ['index.html', { added: 2, removed: 0 }],
      ['src/app.ts', { added: 1, removed: 1 }],
      ['src/components/Button.vue', { added: 3, removed: 0 }]
    ])
    const tree = buildCandidateFileTree(buildCandidateFileItems([
      event({ id: 'root', type: 'file.modified', sequence: 1, payload: { path: 'index.html' } }),
      event({ id: 'nested', type: 'file.modified', sequence: 2, payload: { path: 'src/app.ts' } }),
      event({ id: 'deep', type: 'file.created', sequence: 3, payload: { path: 'src/components/Button.vue' } })
    ]), stats)

    expect(tree.map(node => node.kind === 'folder' ? node.name : node.name)).toEqual(['index.html', 'src'])
    const src = tree.find(node => node.kind === 'folder' && node.name === 'src')
    expect(src?.kind === 'folder' && src.stats).toEqual({ added: 4, removed: 1 })

    const expanded = flattenCandidateFileTree(tree)
    expect(expanded.map(row => row.kind === 'folder' ? row.name : row.name)).toEqual([
      'index.html',
      'src',
      'app.ts',
      'components',
      'Button.vue'
    ])

    const collapsed = flattenCandidateFileTree(tree, new Set(['src']))
    expect(collapsed.map(row => row.kind === 'folder' ? row.name : row.name)).toEqual(['index.html', 'src'])
    expect(sumCandidateFileStats(stats)).toEqual({ added: 6, removed: 1 })
  })

  it('reads git-derived line stats directly from file change payloads', () => {
    const stats = statsFromFileChanges([
      event({ id: 'modified', type: 'file.modified', sequence: 1, payload: { path: 'index.html', added: 899, removed: 499, source: 'git' } })
    ])
    expect(stats.get('index.html')).toEqual({ added: 899, removed: 499 })
  })

  it('prefers git-derived file stats over legacy patch artifacts', async () => {
    const evidence: RunEvidence = {
      run: {
        id: 'run-1',
        projectId: 'project-a',
        issueId: 'AB-1',
        workspaceId: 'workspace-1',
        agentId: 'agent-1',
        attempt: 1,
        status: 'READY_FOR_REVIEW',
        queueReason: null,
        failureReason: null,
        createdAt: '',
        startedAt: '',
        completedAt: null,
        updatedAt: ''
      },
      provenance: null,
      runtimeInstances: [],
      commands: [],
      events: [],
      tests: [],
      fileChanges: [
        event({ id: 'modified', type: 'file.modified', sequence: 1, payload: { path: 'index.html', added: 899, removed: 499, source: 'git' } })
      ],
      usage: null,
      rawOutput: [],
      artifacts: [
        { id: 'art-staged', name: 'candidate-staged.patch', kind: 'candidate_patch', mediaType: 'text/plain', sizeBytes: 1, digest: null, safeMetadata: {}, contentPath: '', createdAt: '' }
      ]
    }
    const stats = await loadReviewFileStats('project-a', evidence, async () => 'should-not-be-used')
    expect(stats.get('index.html')).toEqual({ added: 899, removed: 499 })
  })

  it('loads patch and candidate-file stats for review evidence', async () => {
    const evidence: RunEvidence = {
      run: {
        id: 'run-1',
        projectId: 'project-a',
        issueId: 'AB-1',
        workspaceId: 'workspace-1',
        agentId: 'agent-1',
        attempt: 1,
        status: 'READY_FOR_REVIEW',
        queueReason: null,
        failureReason: null,
        createdAt: '',
        startedAt: '',
        completedAt: null,
        updatedAt: ''
      },
      provenance: null,
      runtimeInstances: [],
      commands: [],
      events: [],
      tests: [],
      fileChanges: [
        event({ id: 'modified', type: 'file.modified', sequence: 1, payload: { path: 'index.html' } }),
        event({ id: 'created', type: 'file.created', sequence: 2, payload: { path: 'new.txt', artifactId: 'art-new' } })
      ],
      usage: null,
      rawOutput: [],
      artifacts: [
        { id: 'art-staged', name: 'candidate-staged.patch', kind: 'candidate_patch', mediaType: 'text/plain', sizeBytes: 1, digest: null, safeMetadata: {}, contentPath: '', createdAt: '' },
        { id: 'art-new', name: 'new.txt', kind: 'candidate_file', mediaType: 'text/plain', sizeBytes: 1, digest: null, safeMetadata: {}, contentPath: '', createdAt: '' }
      ]
    }
    const fetch = async (path: string) => {
      if (path.endsWith('/artifacts/art-staged')) {
        return 'diff --git a/index.html b/index.html\n--- a/index.html\n+++ b/index.html\n@@ -1 +1,899 @@\n' + Array.from({ length: 899 }, (_, index) => `+line ${index}`).join('\n') + '\n' + Array.from({ length: 499 }, (_, index) => `-old ${index}`).join('\n')
      }
      if (path.endsWith('/artifacts/art-new')) return 'alpha\nbeta\n'
      return ''
    }
    const stats = await loadReviewFileStats('project-a', evidence, fetch)
    expect(stats.get('index.html')).toEqual({ added: 899, removed: 499 })
    expect(stats.get('new.txt')).toEqual({ added: 2, removed: 0 })
  })
})
