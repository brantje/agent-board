<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Agent, AssignmentResponse, Issue, Run } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { latestRun, statusLabel } from '../utils/issues'
import { useResource } from '../composables/useResource'
import { useRefresh } from '../composables/useRefresh'
import IssueRelationships from './IssueRelationships.vue'

const props = defineProps<{ projectId: string; issueId: string }>()
const { data: issue, pending, error, refresh } = useResource<Issue>(() => apiPath('issues', props.projectId, props.issueId))
const agents = useResource<Agent[]>(() => apiPath('agents', props.projectId))
const runs = useResource<Run[]>(() => apiPath('runs', props.projectId))
const selected = ref('')
const assigning = ref(false)
const assignmentError = ref<Error>()
const assignmentResult = ref<AssignmentResponse>()
const editing = ref(false)

const latest = computed(() => latestRun(runs.data.value || [], props.issueId))
const choices = computed(() => agents.data.value
  ?.filter(agent => agent.state === 'ENABLED')
  .map(agent => ({ label: agent.name, value: agent.id })) || [])
const assignedAgentName = computed(() => agents.data.value?.find(agent => agent.id === issue.value?.assignedAgentId)?.name)
const assignmentDescription = computed(() => {
  if (!assignmentResult.value) return undefined
  const result = assignmentResult.value
  return `Board status: ${statusLabel(result.issue.status)}. Run attempt ${result.run.attempt}: ${statusLabel(result.run.status)}. Execution continues server-side.`
})

async function reload() {
  await Promise.all([refresh(), agents.refresh(), runs.refresh()])
}

useRefresh(reload)

async function assign() {
  if (!selected.value || assigning.value || issue.value?.status === 'DONE') return
  assigning.value = true
  assignmentError.value = undefined
  assignmentResult.value = undefined
  try {
    const result = await apiRequest<AssignmentResponse>(`${apiPath('issues', props.projectId, props.issueId)}/assignment`, {
      method: 'POST',
      body: { agentId: selected.value }
    })
    assignmentResult.value = result
    issue.value = result.issue
    runs.data.value = [result.run, ...(runs.data.value || []).filter(run => run.id !== result.run.id)]
    selected.value = ''
    await reload()
  } catch (failure) {
    assignmentError.value = failure as Error
  } finally {
    assigning.value = false
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
      <UButton v-if="issue" label="Edit issue" @click="editing = true" />
    </template>

    <AsyncState :pending="pending" :error="error" @retry="reload">
      <div v-if="issue" class="detail-grid">
        <section class="min-w-0 space-y-4">
          <UCard>
            <h2 class="section-label mb-3">Description</h2>
            <p class="whitespace-pre-wrap break-words">{{ issue.description || 'No description provided.' }}</p>
          </UCard>

          <IssueRelationships :project-id="projectId" :issue-id="issueId" />

          <UCard>
            <h2 class="section-label mb-3">Latest Run</h2>
            <AsyncState :pending="runs.pending.value" :error="runs.error.value" :empty="!latest" empty-title="No Runs yet" @retry="runs.refresh">
              <template v-if="latest">
                <div class="flex flex-wrap items-center gap-3">
                  <UBadge :label="statusLabel(latest.status)" color="neutral" />
                  <span>Attempt {{ latest.attempt }}</span>
                  <UButton label="Open Run" :to="`/projects/${projectId}/runs/${latest.id}`" variant="outline" />
                </div>
                <p v-if="latest.queueReason" class="mt-3 text-sm">Queue reason: {{ latest.queueReason }}</p>
                <p v-if="latest.failureReason" class="mt-3 text-sm text-error">Execution failure: {{ latest.failureReason }}</p>
              </template>
            </AsyncState>
          </UCard>

          <slot name="questions">
            <QuestionPanel :project-id="projectId" :issue-id="issueId" />
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
                <dt class="text-muted">Priority</dt>
                <dd>Priority {{ issue.priority }}</dd>
              </div>
              <div>
                <dt class="text-muted">Assigned Agent</dt>
                <dd>{{ assignedAgentName || (issue.assignedAgentId ? 'Assigned Agent unavailable' : 'Unassigned') }}</dd>
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

          <UCard>
            <h2 class="section-label mb-3">Assignment</h2>
            <AsyncState :pending="agents.pending.value" :error="agents.error.value" @retry="agents.refresh">
              <UForm :state="{ selected }" class="space-y-3" @submit="assign">
                <UAlert v-if="assignmentError" title="Unable to assign" :description="assignmentError.message" color="error" />
                <UAlert v-if="assignmentResult" title="Assignment accepted" :description="assignmentDescription" color="success" />
                <UAlert v-if="!choices.length && issue.status !== 'DONE'" title="No enabled Agents" description="Create or enable an Agent in this Project before assigning work." color="neutral" />
                <UFormField label="Enabled Agent" name="agent" description="The backend performs full execution preflight when you assign work.">
                  <USelect v-model="selected" :items="choices" :disabled="assigning || issue.status === 'DONE' || !choices.length" class="w-full" />
                </UFormField>
                <p v-if="issue.status === 'DONE'" class="text-sm text-muted">Reopen this Issue before assigning work.</p>
                <p v-else-if="issue.assignedAgentId" class="text-sm text-muted">Changing Agent may cancel and replace the current attempt on the same Issue Workspace.</p>
                <p v-else class="text-sm text-muted">Assignment persists scheduling intent. Execution continues server-side after this request returns.</p>
                <UButton label="Assign Agent" type="submit" :loading="assigning" :disabled="!selected || issue.status === 'DONE'" />
              </UForm>
            </AsyncState>
          </UCard>
        </aside>
      </div>
    </AsyncState>

    <UModal v-model:open="editing" title="Edit issue" description="Update the durable Issue.">
      <template #body>
        <IssueEditor v-if="issue" :project-id="projectId" :issue="issue" @saved="saved" @cancel="editing = false" />
      </template>
    </UModal>
  </PageFrame>
</template>
