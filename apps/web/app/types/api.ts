/** Intentional public DTOs: packages/api and apps/server/internal/httpapi. */
export interface Project {
  id: string
  name: string
  repositoryPath: string
  defaultBranch: string
  workflowSettings: Record<string, unknown>
  createdAt: string
  updatedAt: string
}

export interface Issue {
  id: string
  projectId: string
  title: string
  description: string
  status: string
  priority: number
  assignedAgentId: string | null
  createdAt: string
  updatedAt: string
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
  executorProfileId: string
  concurrencyLimit: number
  state: string
}
