import type { Project, Question, Review, Run } from '../types/api'

export const attentionRunStatuses = ['FAILED', 'WAITING_FOR_INPUT', 'READY_FOR_REVIEW'] as const

export interface InboxItem {
  kind: 'question' | 'review' | 'run'
  id: string
  projectId: string
  projectName: string
  title: string
  status: string
  to: string
}

export function composeInbox(groups: { project: Project; questions: Question[]; reviews: Review[]; runs: Run[] }[]): InboxItem[] {
  const items: InboxItem[] = []
  for (const group of groups) {
    for (const question of group.questions) {
      if (question.status === 'OPEN' && question.blocking) {
        items.push({
          kind: 'question',
          id: question.id,
          projectId: group.project.id,
          projectName: group.project.name,
          title: question.prompt,
          status: question.status,
          to: `/projects/${group.project.id}/issues/${question.issueId}`
        })
      }
    }
    for (const review of group.reviews) {
      if (review.status === 'PENDING') {
        items.push({
          kind: 'review',
          id: review.id,
          projectId: group.project.id,
          projectName: group.project.name,
          title: `Review for attempt on ${review.issueId}`,
          status: review.status,
          to: `/projects/${group.project.id}/reviews/${review.id}`
        })
      }
    }
    for (const run of group.runs) {
      if ((attentionRunStatuses as readonly string[]).includes(run.status)) {
        items.push({
          kind: 'run',
          id: run.id,
          projectId: group.project.id,
          projectName: group.project.name,
          title: `Attempt ${run.attempt}`,
          status: run.status,
          to: `/projects/${group.project.id}/runs/${run.id}`
        })
      }
    }
  }
  return items
}
