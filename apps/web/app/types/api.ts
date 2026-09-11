/** Intentional public DTOs: packages/api and apps/server/internal/httpapi. */
export interface Project {
  id: string
  name: string
  issuePrefix: string
  repositoryPath: string
  defaultBranch: string
  workflowSettings: Record<string, unknown>
  allowInternalRunner: boolean
  createdAt: string
  updatedAt: string
}

export interface Runner {
  id: string
  name: string | null
  internal: boolean
  managed: boolean
  deletable: boolean
  connected: boolean
  registeredAt: string | null
  revokedAt: string | null
  lastSeenAt: string | null
  capabilities: Record<string, unknown>
  activeSessions: number | null
  reservedSessions: number
  maxActiveSessions: number
  createdAt: string
  updatedAt: string
}

export interface RepositorySettings {
  defaultRepositoryPath: string
  repositoryRoots: string[]
}

export interface Issue {
  id: string
  projectId: string
  number: number
  title: string
  description: string
  status: string
  priority: number
  assignedAgentId: string | null
  createdAt: string
  updatedAt: string
  currentBranch: string | null
  lastEvent: EventEvidence | null
}

export interface IssueRelationship {
  id: string
  projectId: string
  sourceIssueId: string
  targetIssueId: string
  type: string
  createdAt: string
}

export interface Run {
  id: string
  projectId: string
  issueId: string
  workspaceId: string
  agentId: string | null
  attempt: number
  status: string
  queueReason: string | null
  failureReason: string | null
  createdAt: string
  startedAt: string | null
  completedAt: string | null
  updatedAt: string
  currentBranch: string | null
}

export interface AssignmentResponse {
  issue: Issue
  run: Run
}

export interface Agent {
  id: string
  projectId: string | null
  name: string
  roleInstructions: string
  engine: string
  modelProfileId: string
  engineSettings: Record<string, unknown>
  concurrencyLimit: number
  state: string
}

export interface QuestionOption {
  id: string
  label: string
}

export interface Question {
  id: string
  projectId: string
  issueId: string
  runId: string
  prompt: string
  kind: 'TEXT' | 'SINGLE_CHOICE' | 'MULTI_CHOICE' | string
  options: QuestionOption[]
  recommendation?: string | null
  custom: boolean
  blocking: boolean
  status: string
  createdAt: string
  answeredAt?: string | null
}

export interface QuestionAnswer {
  kind: string
  text?: string
  optionIds?: string[]
}

export interface QuestionAnswerResponse {
  question: Question
  decisionId: string
  run: Run
  resumeJobId?: string | null
}

export interface EventEvidence {
  id: string
  schemaVersion: number
  type: string
  occurredAt: string
  projectId: string
  issueId: string | null
  runId: string | null
  sequence: number | null
  agentId: string | null
  workspaceId: string | null
  runtimeInstanceId: string | null
  correlationId: string | null
  parentEventId: string | null
  actor: Record<string, unknown>
  payload: Record<string, unknown>
}

export interface RuntimeInstanceEvidence {
  id: string
  runtimeId: string
  status: string
  runnerStatus: string
  createdAt: string
  startedAt: string | null
  stoppedAt: string | null
  updatedAt: string
}

export interface ExecutionSessionEvidence {
  id: string
  runtimeInstanceId: string | null
  runnerId: string | null
  status: string
  cwd: string
  command: string[]
  exitCode: number | null
  createdAt: string
  startedAt: string | null
  completedAt: string | null
  updatedAt: string
}

export interface RawOutputChunkEvidence {
  id: string
  stream: string
  sequence: number
  sizeBytes: number
  digest: string | null
  contentPath: string
  createdAt: string
}

export interface ArtifactEvidence {
  id: string
  name: string
  kind: string
  mediaType: string | null
  sizeBytes: number
  digest: string | null
  safeMetadata: Record<string, unknown>
  contentPath: string
  createdAt: string
}

export interface RunUsageEvidence {
  contextTokens: number
  contextLimitTokens: number | null
  inputTokens: number
  outputTokens: number
  cacheReadTokens: number
  averageWaitMs: number | null
  tokensPerSecond: number | null
}

export interface RunEvidence {
  run: Run
  provenance: Record<string, unknown> | null
  runtimeInstances: RuntimeInstanceEvidence[]
  commands: ExecutionSessionEvidence[]
  events: EventEvidence[]
  tests: EventEvidence[]
  fileChanges: EventEvidence[]
  usage: RunUsageEvidence | null
  rawOutput: RawOutputChunkEvidence[]
  artifacts: ArtifactEvidence[]
}

export interface Review {
  id: string
  projectId: string
  issueId: string
  runId: string
  status: string
  requestedAt: string
  decidedAt: string | null
  createdAt: string
  updatedAt: string
}

export interface ReviewDecision {
  id: string
  outcome: 'APPROVED' | 'CHANGES_REQUESTED' | string
  actorType: string
  actorId: string | null
  safeDetails: Record<string, unknown>
  createdAt: string
}

export interface ReviewDetail {
  review: Review
  decision: ReviewDecision | null
  testStatus: 'NOT_RUN' | 'PASSED' | 'FAILED' | 'UNKNOWN' | string
  evidence: RunEvidence
}

export interface ReviewApprovalResponse {
  review: Review
  decision: ReviewDecision
  run: Run
  issue: Issue
}

export interface ReviewRequestChangesResponse {
  review: Review
  decision: ReviewDecision
  run: Run
  issue: Issue
  jobId: string
}
