/** Intentional public DTOs: packages/api and apps/server/internal/httpapi. */
export interface Project {
  id: string
  name: string
  issuePrefix: string
  sourceType: 'local' | 'git'
  cloneUrl: string | null
  sourceRef: string | null
  repositoryPath: string
  defaultBranch: string
  workflowSettings: Record<string, unknown>
  allowInternalRunner: boolean
  createdAt: string
  updatedAt: string
}

export interface Runner {
  id: string
  projectId: string | null
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

export interface ProjectRunnerSettings {
  runnerIds: string[]
  projectRunners: Runner[]
  sharedRunners: Runner[]
}

export interface RepositorySettings {
  defaultRepositoryPath: string
  repositoryRoots: string[]
}

export interface IssueCreator {
  type: 'HUMAN' | 'AGENT'
  id: string
  name: string | null
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
  createdBy: IssueCreator | null
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
  agentId: string | null
  workspaceId: string | null
  runtimeInstanceId: string | null
  correlationId: string | null
  parentEventId: string | null
  sequence: number | null
  actor: Record<string, unknown>
  payload: Record<string, unknown>
  createdAt: string
}

export interface QuestionAnswerResponse {
  question: Question
  decisionId: string
  run: Run
  resumeJobId?: string | null
}
