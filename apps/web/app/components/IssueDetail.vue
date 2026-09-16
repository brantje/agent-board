<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Assignee, AssignmentResponse, Issue, IssueExecutionState, Run } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { issueRuns, statusLabel } from '../utils/issues'
import { assigneeIdentityKind, assigneeTypeLabel } from '../utils/identity'
import { isBoardActivityEvent, applyCurrentBranchToIssue } from '../utils/events'
import { useResource } from '../composables/useResource'
import { useProjectEvents } from '../composables/useProjectEvents'
import IssueRelationships from './IssueRelationships.vue'

const props = withDefaults(defineProps<{ projectId: string; issueId: string; canMutate?: boolean }>(), { canMutate: true })
const { data: issue, pending, error, refresh } = useResource<Issue>(() => apiPath('issues', props.projectId, props.issueId))
const assignees = useResource<Assignee[]>(() => apiPath('assignees', props.projectId))
const runs = useResource<Run[]>(() => apiPath('runs', props.projectId))
const execution = useResource<IssueExecutionState>(() => `${apiPath('issues', props.projectId, props.issueId)}/execution`)
const selected = ref('')
const assigning = ref(false)
const assignmentError = ref<Error>()
const assignmentResult = ref<AssignmentResponse>()
const startingRun = ref(false)
const startRunError = ref<Error>()
const startRunResult = ref<Run>()
const editing = ref(false)

const unassignedChoice = '__UNASSIGNED__'
const issueRunHistory = computed(() => issueRuns(runs.data.value || [], props.issueId))
const latest = computed(() => issueRunHistory.value[0])
const choices = computed(() => [
  { label: 'Unassigned', value: unassignedChoice },
  ...(assignees.data.value || []).map(assignee => ({
    label: `${assignee.name} · ${assigneeTypeLabel(assignee.type)}`,
    value: `${assignee.type}:${assignee.id}`
  }))
])
const assignedName = computed(() => issue.value?.assignedTo?.name)
const assignedKind = computed(() => issue.value?.assignedTo ? assigneeIdentityKind(issue.value.assignedTo.type) : 'user')
const assignedTypeLabel = computed(() => issue.value?.assignedTo ? assigneeTypeLabel(issue.value.assignedTo.type) : '')
const canStartRun = computed(() => Boolean(props.canMutate && execution.data.value?.canStart))
const runActionLabel = computed(() => issueRunHistory.value.length ? 'Run again' : 'Start Run')
const executionFeedback = computed(() => {
  const state = execution.data.value
  if (!state || state.state === 'NOT_AGENT_OWNED') return undefined
  if (state.state === 'BACKLOG') {
    return { title: 'Execution parked', description: 'This Issue is in Backlog. Move it out of Backlog before starting a Run.' }
  }
  if (state.state === 'CONFIGURATION_UNAVAILABLE') {
    return { title: 'Execution unavailable', description: 'The current ownership is valid, but its execution configuration prevents a new Run from being created.' }
  }
  if (state.state === 'READY') {
    return { title: 'Execution available', description: 'No active Run exists for the current execution Agent. You can start a Run.' }
  }
  const run = state.activeRun
  if (!run) return { title: 'Execution active', description: 'The current execution Agent already has an active Run for this Issue.' }
  if (run.status === 'QUEUED') {
    const wait = run.queueReason ? ` Queue reason: ${run.queueReason}.` : ''
    return { title: 'Execution queued', description: `Attempt ${run.attempt} is waiting for scheduler admission.${wait}` }
  }
  return { title: 'Execution active', description: `Attempt ${run.attempt} is already ${statusLabel(run.status)}.` }
})
const creatorLabel = computed(() => {
  const creator = issue.value?.createdBy
  if (!creator) return 'Unknown'
  const kind = creator.type === 'AGENT' ? 'Agent' : 'User'
  return creator.name?.trim() ? `${creator.name} · ${kind}` : `${kind} unavailable`
})
const assignmentDescription = computed(() => {
  if (!assignmentResult.value) return undefined
  return `Board status: ${statusLabel(assignmentResult.value.issue.status)}. Ownership updated.`
})
const startRunDescription = computed(() => {
  const result = startRunResult.value
  if (!result) return undefined
  const queue = result.queueReason ? ` Queue reason: ${result.queueReason}.` : ''
  return `Attempt ${result.attempt} · ${statusLabel(result.status)}.${queue}`
})

const questionsPanel = ref<{ refresh?: () => Promise<unknown> }>()

async function reload() {
  await Promise.all([refresh(), assignees.refresh(), runs.refresh(), execution.refresh(), questionsPanel.value?.refresh?.()])
}

useProjectEvents(() => props.projectId, async event => {
  if (event.type === 'git.branch_checked_out' && issue.value) {
    const patched = applyCurrentBranchToIssue(issue.value, event)
    if (patched) {
      issue.value = patched
      return
    }
  }
  if (!isBoardActivityEvent(event.type)) return
  await reload()
})

async function assign() {
  if (!props.canMutate || !selected.value || assigning.value) return

  const selectedAssignee = selected.value === unassignedChoice
    ? null
    : assignees.data.value?.find(assignee => `${assignee.type}:${assignee.id}` === selected.value)
  if (selected.value !== unassignedChoice && !selectedAssignee) return

  assigning.value = true
  assignmentError.value = undefined
  assignmentResult.value = undefined

  let result: AssignmentResponse
  try {
    result = await apiRequest<AssignmentResponse>(`${apiPath('issues', props.projectId, props.issueId)}/assignment`, {
      method: 'POST',
      body: {
        assignedTo: selectedAssignee
          ? { type: selectedAssignee.type, id: selectedAssignee.id }
          : null
      }
    })
  } catch (failure) {
    assignmentError.value = failure as Error
    assigning.value = false
    return
  }

  assignmentResult.value = result
  issue.value = result.issue
  selected.value = ''
  try {
    await Promise.all([runs.refresh(), execution.refresh()])
  } finally {
    assigning.value = false
  }
}

async function startRun() {
  if (!canStartRun.value || startingRun.value) return

  startingRun.value = true
  startRunError.value = undefined
  startRunResult.value = undefined
  try {
    const result = await apiRequest<Run>(`${apiPath('issues', props.projectId, props.issueId)}/runs`, {
      method: 'POST'
    })
    startRunResult.value = result
    await Promise.all([runs.refresh(), execution.refresh()])
  } catch (failure) {
    startRunError.value = failure as Error
  } finally {
    startingRun.value = false
  }
}

async function saved(savedIssue: Issue) {
  issue.value = savedIssue
  editing.value = false
  await reload()
}
</script>

<template>
  <PageFrame :title="issue?.title || 'Issue'">
    <template #actions>
      <UButton label="Board" :to="`/projects/${projectId}/board`" variant="outline" />
      <UButton v-if="issue && canMutate" label="Edit issue" @click="editing = true" />
    </template>

    <AsyncState :pending="pending" :error="error" @retry="reload">
      <div v-if="issue" class="detail-grid">
        <section class="min-w-0 space-y-4">
          <UCard>
            <h2 class="section-label mb-3">Description</h2>
            <p class="whitespace-pre-wrap break-words">{{ issue.description || 'No description provided.' }}</p>
          </UCard>

          <IssueRelationships :project-id="projectId" :issue-id="issueId" :can-mutate="canMutate" />

          <UCard>
            <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
              <h2 class="section-label">Runs</h2>
              <UButton
                v-if="canStartRun"
                :label="runActionLabel"
                icon="i-lucide-play"
                :loading="startingRun"
                @click="startRun"
              />
            </div>
            <UAlert v-if="execution.error.value" title="Execution state unavailable" :description="execution.error.value.message" color="error" class="mb-3" />
            <UAlert v-else-if="executionFeedback" :title="executionFeedback.title" :description="executionFeedback.description" class="mb-3" />
            <UAlert v-if="startRunError" title="Unable to start Run" :description="startRunError.message" color="error" class="mb-3" />
            <UAlert v-if="startRunResult" title="Run accepted" :description="startRunDescription" color="success" class="mb-3" />
            <AsyncState
              :pending="runs.pending.value"
              :error="runs.error.value"
              :empty="!issueRunHistory.length"
              empty-title="No Runs yet"
              empty-description="Execution attempts will appear here."
              @retry="runs.refresh"
            >
              <div v-if="issueRunHistory.length" class="space-y-3">
                <div v-for="run in issueRunHistory" :key="run.id" class="rounded-md border border-default p-3">
                  <div class="flex flex-wrap items-center gap-3">
                    <RunStatus :status="run.status" :label="statusLabel(run.status)" />
                    <span>Attempt {{ run.attempt }}</span>
                    <UButton label="Open Run" :to="`/projects/${projectId}/runs/${run.id}`" variant="outline" />
                  </div>
                  <p v-if="run.queueReason" class="mt-3 text-sm">Queue reason: {{ run.queueReason }}</p>
                  <p v-if="run.failureReason" class="mt-3 text-sm text-error">Execution failure: {{ run.failureReason }}</p>
                </div>
              </div>
            </AsyncState>
          </UCard>

          <slot name="questions">
            <QuestionPanel ref="questionsPanel" :project-id="projectId" :issue-id="issueId" :can-mutate="canMutate" />
          </slot>
        </section>

        <aside class="space-y-4">
          <UCard>
            <h2 class="section-label mb-3">Properties</h2>
            <dl class="space-y-3 text-sm">
              <div>
                <dt class="text-muted">Board status</dt>
                <dd>{{ statusLabel(issue.status) }}</dd>
              </div>
              <div>
                <dt class="text-muted">Branch</dt>
                <dd><WorkspaceBranch v-if="issue.currentBranch" :branch="issue.currentBranch" /><span v-else>None</span></dd>
              </div>
              <div>
                <dt class="text-muted">Priority</dt>
                <dd>Priority {{ issue.priority }}</dd>
              </div>
              <div>
                <dt class="text-muted">Assignee</dt>
                <dd class="flex items-center gap-2">
                  <template v-if="assignedName">
                    <IdentityAvatar :kind="assignedKind" :name="assignedName" size="xs" />
                    <span>{{ assignedName }} · {{ assignedTypeLabel }}</span>
                  </template>
                  <span v-else-if="issue.assignedTo?.id">Assignee unavailable</span>
                  <span v-else>Unassigned</span>
                </dd>
              </div>
              <div>
                <dt class="text-muted">Created by</dt>
                <dd class="flex items-center gap-2">
                  <IdentityAvatar
                    v-if="issue.createdBy"
                    :kind="issue.createdBy.type === 'AGENT' ? 'agent' : 'user'"
                    :name="issue.createdBy.name || creatorLabel"
                    size="xs"
                  />
                  <span>{{ creatorLabel }}</span>
                </dd>
              </div>
              <div>
                <dt class="text-muted">Latest Run</dt>
                <dd v-if="latest">
                  <NuxtLink :to="`/projects/${projectId}/runs/${latest.id}`" class="hover:text-primary focus-visible:outline-2 focus-visible:outline-primary">
                    Attempt {{ latest.attempt }} · {{ statusLabel(latest.status) }}
                  </NuxtLink>
                </dd>
                <dd v-else>None</dd>
              </div>
              <div>
                <dt class="text-muted">Issue</dt>
                <dd class="font-mono break-all">{{ issue.id }}</dd>
              </div>
            </dl>
          </UCard>

          <UCard v-if="canMutate">
            <h2 class="section-label mb-3">Assignment</h2>
            <AsyncState :pending="assignees.pending.value" :error="assignees.error.value" @retry="assignees.refresh">
              <UForm :state="{ selected }" class="space-y-3" @submit="assign">
                <UAlert v-if="assignmentError" title="Unable to update assignee" :description="assignmentError.message" color="error" />
                <UAlert v-if="assignmentResult" title="Assignment accepted" :description="assignmentDescription" color="success" />
                <UFormField label="Assignee" name="assignee" description="Choose an eligible User, Agent, or Squad, or leave the Issue unassigned.">
                  <USelect v-model="selected" :items="choices" :disabled="assigning" class="w-full" />
                </UFormField>
                <p class="text-sm text-muted">Assignment changes ownership only; it does not change the board status. Agent or Squad ownership may enqueue execution according to backend policy.</p>
                <UButton label="Update assignee" type="submit" :loading="assigning" :disabled="!selected" />
              </UForm>
            </AsyncState>
          </UCard>
        </aside>
      </div>
    </AsyncState>

    <UModal v-if="canMutate" v-model:open="editing" title="Edit issue" description="Update the durable Issue.">
      <template #body>
        <IssueEditor v-if="issue" :project-id="projectId" :issue="issue" @saved="saved" @cancel="editing = false" />
      </template>
    </UModal>
  </PageFrame>
</template>
