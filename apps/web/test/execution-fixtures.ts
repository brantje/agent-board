import type { ArtifactEvidence, EventEvidence, Project, Question, Review, ReviewDetail, Run, RunEvidence } from '../app/types/api'

export const project: Project = {
  id: 'project-a',
  name: 'Workspace',
  issuePrefix: 'AB',
  repositoryPath: '/repo',
  defaultBranch: 'main',
  workflowSettings: {},
  createdAt: '2026-01-01T00:00:00.000Z',
  updatedAt: '2026-01-01T00:00:00.000Z'
}

export const run: Run = {
  id: 'run-1',
  projectId: project.id,
  issueId: 'AB-1',
  workspaceId: 'workspace-1',
  agentId: 'agent-1',
  attempt: 1,
  status: 'RUNNING',
  queueReason: null,
  failureReason: null,
  createdAt: '2026-01-01T00:00:00.000Z',
  startedAt: '2026-01-01T00:01:00.000Z',
  completedAt: null,
  updatedAt: '2026-01-01T00:01:00.000Z'
}

export function event(partial: Partial<EventEvidence> & Pick<EventEvidence, 'id' | 'type'>): EventEvidence {
  return {
    schemaVersion: 1,
    occurredAt: '2026-01-01T00:01:00.000Z',
    projectId: project.id,
    issueId: null,
    runId: null,
    sequence: 1,
    agentId: 'agent-1',
    workspaceId: 'workspace-1',
    runtimeInstanceId: 'instance-1',
    correlationId: null,
    parentEventId: null,
    actor: { type: 'AGENT' },
    payload: {},
    ...partial
  }
}

export function evidence(overrides: Partial<RunEvidence> = {}): RunEvidence {
  return {
    run,
    provenance: { schemaVersion: 1, context: { runtime: { id: 'runtime-1', name: 'Docker' } } },
    runtimeInstances: [{
      id: 'instance-1',
      runtimeId: 'runtime-1',
      status: 'RUNNING',
      runnerStatus: 'CONNECTED',
      createdAt: '2026-01-01T00:01:00.000Z',
      startedAt: '2026-01-01T00:01:00.000Z',
      stoppedAt: null,
      updatedAt: '2026-01-01T00:01:00.000Z'
    }],
    commands: [{
      id: 'session-1',
      runtimeInstanceId: 'instance-1',
      status: 'COMPLETED',
      cwd: '/workspace',
      command: ['git', 'status'],
      exitCode: 0,
      createdAt: '2026-01-01T00:01:00.000Z',
      startedAt: '2026-01-01T00:01:00.000Z',
      completedAt: '2026-01-01T00:01:05.000Z',
      updatedAt: '2026-01-01T00:01:05.000Z'
    }],
    events: [],
    tests: [],
    fileChanges: [],
    rawOutput: [{
      id: 'chunk-1',
      stream: 'STDOUT',
      sequence: 1,
      sizeBytes: 12,
      digest: null,
      contentPath: 'raw/chunk-1',
      createdAt: '2026-01-01T00:01:00.000Z'
    }],
    artifacts: [],
    ...overrides
  }
}

export function question(partial: Partial<Question> = {}): Question {
  return {
    id: 'question-1',
    projectId: project.id,
    issueId: 'AB-1',
    runId: run.id,
    prompt: 'Which strategy?',
    kind: 'SINGLE_CHOICE',
    options: [{ id: 'safe', label: 'Safe' }, { id: 'fast', label: 'Fast' }],
    recommendation: 'safe',
    custom: true,
    blocking: true,
    status: 'OPEN',
    createdAt: '2026-01-01T00:02:00.000Z',
    ...partial
  }
}

export function review(partial: Partial<Review> = {}): Review {
  return {
    id: 'review-1',
    projectId: project.id,
    issueId: 'AB-1',
    runId: run.id,
    status: 'PENDING',
    requestedAt: '2026-01-01T00:10:00.000Z',
    decidedAt: null,
    createdAt: '2026-01-01T00:10:00.000Z',
    updatedAt: '2026-01-01T00:10:00.000Z',
    ...partial
  }
}

export const candidateArtifacts: ArtifactEvidence[] = [
  { id: 'art-staged', name: 'candidate-staged.patch', kind: 'candidate_patch', mediaType: 'text/plain', sizeBytes: 10, digest: null, safeMetadata: {}, contentPath: 'artifacts/staged', createdAt: '2026-01-01T00:09:00.000Z' },
  { id: 'art-unstaged', name: 'candidate-unstaged.patch', kind: 'candidate_patch', mediaType: 'text/plain', sizeBytes: 10, digest: null, safeMetadata: {}, contentPath: 'artifacts/unstaged', createdAt: '2026-01-01T00:09:00.000Z' },
  { id: 'art-manifest', name: 'candidate-manifest.json', kind: 'candidate_manifest', mediaType: 'application/json', sizeBytes: 8, digest: null, safeMetadata: {}, contentPath: 'artifacts/manifest', createdAt: '2026-01-01T00:09:00.000Z' },
  { id: 'art-file', name: 'new.txt', kind: 'candidate_file', mediaType: 'text/plain', sizeBytes: 4, digest: null, safeMetadata: {}, contentPath: 'artifacts/new', createdAt: '2026-01-01T00:09:00.000Z' }
]

export function reviewDetail(overrides: Partial<ReviewDetail> = {}): ReviewDetail {
  const fileEvents = [
    event({ id: 'file-del', type: 'file.deleted', sequence: 4, payload: { path: 'gone.txt' } }),
    event({ id: 'file-ren', type: 'file.renamed', sequence: 5, payload: { path: 'renamed.txt', oldPath: 'old.txt' } })
  ]
  return {
    review: review(),
    decision: null,
    testStatus: 'NOT_RUN',
    evidence: evidence({
      run: { ...run, status: 'READY_FOR_REVIEW', completedAt: '2026-01-01T00:10:00.000Z' },
      artifacts: candidateArtifacts,
      fileChanges: fileEvents,
      events: [
        event({ id: 'msg-1', type: 'agent.message', sequence: 2, payload: { kind: 'summary', message: 'Candidate is ready.' } }),
        ...fileEvents
      ],
      commands: evidence().commands,
      tests: []
    }),
    ...overrides
  }
}

export class MockEventSource {
  static instances: MockEventSource[] = []
  static reset() {
    this.instances = []
  }

  url: string
  onopen: ((event: Event) => void) | null = null
  onmessage: ((event: MessageEvent<string>) => void) | null = null
  onerror: ((event: Event) => void) | null = null
  closed = false
  readyState = 1
  private named = new Map<string, Array<(event: MessageEvent<string>) => void>>()

  constructor(url: string) {
    this.url = url
    MockEventSource.instances.push(this)
    queueMicrotask(() => {
      if (!this.closed) this.onopen?.(new Event('open'))
    })
  }

  emit(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) } as MessageEvent<string>)
  }

  emitNamed(type: string, data = '{}') {
    for (const listener of this.named.get(type) ?? []) {
      listener({ data } as MessageEvent<string>)
    }
  }

  addEventListener(type: string, listener: EventListener) {
    const list = this.named.get(type) ?? []
    list.push(listener as (event: MessageEvent<string>) => void)
    this.named.set(type, list)
  }

  fail() {
    this.onerror?.(new Event('error'))
  }

  close() {
    this.closed = true
    this.readyState = 2
  }
}
