<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ArtifactEvidence, ReviewApprovalResponse, ReviewDetail as ReviewDetailModel, ReviewRequestChangesResponse } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { commandLabel, runStatusLabel } from '../utils/runs'
import { statusLabel } from '../utils/issues'
import { useResource } from '../composables/useResource'
import { eventDescription, eventTitle } from '../utils/events'

const props = defineProps<{ projectId: string; reviewId: string }>()
const { data, pending, error, refresh } = useResource<ReviewDetailModel>(() => apiPath('reviews', props.projectId, props.reviewId))
const feedback = ref('')
const acting = ref(false)
const actionError = ref<Error>()
const approval = ref<ReviewApprovalResponse>()
const changes = ref<ReviewRequestChangesResponse>()

const review = computed(() => approval.value?.review || changes.value?.review || data.value?.review)
const issue = computed(() => approval.value?.issue || changes.value?.issue)
const decidedRun = computed(() => approval.value?.run || changes.value?.run || data.value?.evidence.run)
const evidence = computed(() => data.value?.evidence)
const testStatus = computed(() => data.value?.testStatus)

const candidateArtifacts = computed(() => (evidence.value?.artifacts || []).filter(artifact =>
  ['candidate-staged.patch', 'candidate-unstaged.patch', 'candidate-manifest.json'].includes(artifact.name) || artifact.kind === 'candidate_file'
))
const agentMessages = computed(() => (evidence.value?.events || []).filter(event => event.type === 'agent.message'))

function artifactHref(artifact: ArtifactEvidence) {
  const runId = evidence.value?.run.id
  return runId ? `/api/projects/${props.projectId}/runs/${runId}/artifacts/${artifact.id}` : '#'
}

function testsAreSuccess() {
  return testStatus.value === 'PASSED'
}

async function approve() {
  if (acting.value) return
  acting.value = true
  actionError.value = undefined
  try {
    approval.value = await apiRequest<ReviewApprovalResponse>(`${apiPath('reviews', props.projectId, props.reviewId)}/approve`, { method: 'POST' })
    changes.value = undefined
  } catch (failure) {
    actionError.value = failure as Error
  } finally {
    acting.value = false
  }
}

async function requestChanges() {
  if (acting.value || !feedback.value.trim()) return
  acting.value = true
  actionError.value = undefined
  try {
    changes.value = await apiRequest<ReviewRequestChangesResponse>(`${apiPath('reviews', props.projectId, props.reviewId)}/request-changes`, {
      method: 'POST',
      body: { feedback: feedback.value.trim() }
    })
    approval.value = undefined
  } catch (failure) {
    actionError.value = failure as Error
  } finally {
    acting.value = false
  }
}

function provenanceText() {
  if (!evidence.value?.provenance) return 'No provenance recorded.'
  return JSON.stringify(evidence.value.provenance, null, 2)
}
</script>

<template>
  <PageFrame :title="review ? 'Review' : 'Review'">
    <template #actions>
      <UButton v-if="review" label="Issue" :to="`/projects/${projectId}/issues/${review.issueId}`" variant="outline" />
      <UButton v-if="review" label="Run" :to="`/projects/${projectId}/runs/${review.runId}`" variant="outline" />
    </template>
    <AsyncState :pending="pending" :error="error" @retry="refresh">
      <div v-if="data" class="detail-grid">
        <section class="min-w-0 space-y-4">
          <UAlert v-if="testStatus === 'NOT_RUN'" title="Tests were not run" description="Not run is not a passing result. Missing tests must not be treated as success." color="warning" />
          <UAlert v-else-if="testStatus === 'FAILED'" title="Tests failed" description="The candidate includes failing test evidence." color="error" />
          <UAlert v-else-if="testStatus === 'UNKNOWN'" title="Test status unknown" description="Test evidence exists but did not resolve to a pass or fail." color="neutral" />
          <UAlert v-else-if="testsAreSuccess()" title="Tests passed" description="Test evidence for this attempt is passing." color="success" />
          <UAlert v-if="actionError" title="Unable to record Review decision" :description="actionError.message" color="error" />
          <UAlert v-if="approval" title="Approved" :description="`Board status: ${statusLabel(approval.issue.status)}. ${runStatusLabel(approval.run.status)}.`" color="success" />
          <UAlert v-if="changes" title="Changes requested" :description="`Board status: ${statusLabel(changes.issue.status)}. ${runStatusLabel(changes.run.status)}. Follow-up job ${changes.jobId}.`" color="neutral" />

          <UCard>
            <h2 class="section-label mb-3">Candidate</h2>
            <ul class="space-y-2 text-sm">
              <li v-for="artifact in candidateArtifacts" :key="artifact.id">
                <a :href="artifactHref(artifact)" class="hover:text-primary focus-visible:outline-2 focus-visible:outline-primary">{{ artifact.name }}</a>
                <span class="text-muted"> · {{ artifact.kind.replaceAll('_', ' ') }}</span>
              </li>
            </ul>
          </UCard>

          <UCard>
            <h2 class="section-label mb-3">Commands</h2>
            <p v-for="session in evidence?.commands" :key="session.id" class="font-mono text-sm">{{ commandLabel(session.command) }} · {{ session.status }}</p>
          </UCard>
          <UCard>
            <h2 class="section-label mb-3">Tests</h2>
            <p v-for="event in evidence?.tests" :key="event.id" class="text-sm">{{ eventTitle(event) }} · {{ eventDescription(event) }}</p>
            <p v-if="!evidence?.tests.length" class="text-sm text-muted">No test Events on this attempt.</p>
          </UCard>
          <UCard>
            <h2 class="section-label mb-3">Agent messages</h2>
            <p v-for="event in agentMessages" :key="event.id" class="mb-2 whitespace-pre-wrap break-words text-sm">{{ eventTitle(event) }}: {{ eventDescription(event) }}</p>
          </UCard>
          <UCard>
            <h2 class="section-label mb-3">Provenance</h2>
            <pre>{{ provenanceText() }}</pre>
          </UCard>
        </section>
        <aside class="space-y-4">
          <RunChangedFilesCard :project-id="projectId" :evidence="evidence" />
          <UCard>
            <h2 class="section-label mb-3">Decision</h2>
            <p class="mb-3 text-sm">{{ statusLabel(review?.status || '') }}</p>
            <UButton v-if="review?.status === 'PENDING'" label="Approve" :loading="acting" class="mb-4" @click="approve" />
            <UForm :state="{ feedback }" class="space-y-3" @submit="requestChanges">
              <UFormField label="Feedback" name="feedback">
                <UTextarea v-model="feedback" class="w-full" :disabled="acting || review?.status !== 'PENDING'" />
              </UFormField>
              <UButton label="Request changes" type="submit" :loading="acting" :disabled="!feedback.trim() || review?.status !== 'PENDING'" />
            </UForm>
            <p v-if="issue" class="mt-3 text-sm">Board status: {{ statusLabel(issue.status) }}</p>
            <p v-if="decidedRun" class="mt-2 text-sm">
              <NuxtLink :to="`/projects/${projectId}/runs/${decidedRun.id}`" class="hover:text-primary focus-visible:outline-2 focus-visible:outline-primary">{{ runStatusLabel(decidedRun.status) }}</NuxtLink>
            </p>
          </UCard>
        </aside>
      </div>
    </AsyncState>
  </PageFrame>
</template>
