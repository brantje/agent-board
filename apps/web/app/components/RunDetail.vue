<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ArtifactEvidence, EventEvidence } from '../types/api'
import { apiPath, apiText } from '../utils/api'
import { commandLabel, formatElapsed, runStatusLabel } from '../utils/runs'
import { useRunEvents } from '../composables/useRunEvents'

const props = defineProps<{ projectId: string; runId: string }>()
const { evidence, events, reviews, connection, pending, error, refresh } = useRunEvents(() => props.projectId, () => props.runId)
const logs = ref<Record<string, string>>({})
const logError = ref<Error>()

const run = computed(() => evidence.value?.run)
const matchingReview = computed(() => evidence.value?.run.issueId && reviews.value.find(item => item.runId === props.runId)?.id)

const commandItems = computed(() => (evidence.value?.commands || []).map(session => ({
  label: commandLabel(session.command) || session.id,
  slot: 'command',
  session
})))
const testItems = computed(() => (evidence.value?.tests || []).map(event => ({
  label: testLabel(event),
  slot: 'test',
  event
})))
const fileItems = computed(() => (evidence.value?.fileChanges || []).map(event => ({
  label: fileLabel(event),
  slot: 'file',
  event
})))
const rawItems = computed(() => (evidence.value?.rawOutput || []).map(chunk => ({
  label: `${chunk.stream} · ${chunk.sequence}`,
  slot: 'raw',
  chunk
})))
const artifactHref = (artifact: ArtifactEvidence) => `/api/projects/${props.projectId}/runs/${props.runId}/artifacts/${artifact.id}`

function testLabel(event: EventEvidence) {
  const command = commandLabel(event.payload?.command)
  const status = typeof event.payload?.status === 'string' ? event.payload.status : event.type
  return [command, status].filter(Boolean).join(' · ')
}

function fileLabel(event: EventEvidence) {
  const path = typeof event.payload?.path === 'string' ? event.payload.path : event.type
  const oldPath = typeof event.payload?.oldPath === 'string' ? event.payload.oldPath : undefined
  return oldPath ? `${oldPath} → ${path}` : path
}

function fileArtifact(event: EventEvidence) {
  const id = typeof event.payload?.artifactId === 'string' ? event.payload.artifactId : undefined
  return evidence.value?.artifacts.find(artifact => artifact.id === id)
}

async function loadChunk(id: string) {
  logError.value = undefined
  try {
    logs.value = { ...logs.value, [id]: await apiText(`${apiPath('runs', props.projectId, props.runId)}/raw-output/${id}`) }
  } catch (failure) {
    logError.value = failure as Error
  }
}

function provenanceText() {
  if (!evidence.value?.provenance) return 'No provenance recorded.'
  return JSON.stringify(evidence.value.provenance, null, 2)
}
</script>

<template>
  <PageFrame :title="run ? `Run attempt ${run.attempt}` : 'Run'">
    <template #actions>
      <UButton v-if="run" label="Issue" :to="`/projects/${projectId}/issues/${run.issueId}`" variant="outline" />
      <UButton v-if="matchingReview" label="Review" :to="`/projects/${projectId}/reviews/${matchingReview}`" variant="outline" />
    </template>
    <AsyncState :pending="pending" :error="error" @retry="refresh">
      <div v-if="run" class="detail-grid">
        <section class="min-w-0 space-y-4">
          <UAlert
            v-if="connection === 'reconnecting'"
            title="Reconnecting"
            description="Live updates disconnected. This is not a Run failure. The timeline will resume from the last persisted sequence."
            color="warning"
          />
          <UAlert v-else-if="connection === 'live'" title="Live updates" description="live" color="neutral" />
          <UCard>
            <h2 class="section-label mb-3">Working state</h2>
            <div class="flex flex-wrap items-center gap-3">
              <UBadge color="neutral" variant="subtle" :label="runStatusLabel(run.status)" />
              <span>Elapsed {{ formatElapsed(run.startedAt, run.completedAt) }}</span>
            </div>
            <p class="mt-3 text-sm">Agent <span class="font-mono">{{ run.agentId || 'Unassigned' }}</span></p>
            <p class="mt-1 text-sm">Runtime instance <span class="font-mono">{{ evidence?.runtimeInstances[0]?.id || 'None' }}</span></p>
            <p class="mt-1 text-sm">Commands {{ evidence?.commands.length || 0 }} · Files {{ evidence?.fileChanges.length || 0 }} · Tests {{ evidence?.tests.length || 0 }}</p>
            <p v-if="run.queueReason" class="mt-3 text-sm">Queue reason: {{ run.queueReason }}</p>
            <p v-if="run.failureReason" class="mt-3 text-sm text-error">Execution failure: {{ run.failureReason }}</p>
          </UCard>
          <UCard>
            <h2 class="section-label mb-3">Activity</h2>
            <ActivityTimeline :events="events" />
          </UCard>
          <UCard>
            <h2 class="section-label mb-3">Commands</h2>
            <UAccordion :items="commandItems">
              <template #command="{ item }">
                <dl class="space-y-1 py-2 text-sm">
                  <p>Status: {{ item.session.status }}</p>
                  <p class="font-mono break-all">{{ commandLabel(item.session.command) }}</p>
                  <p>Exit code: {{ item.session.exitCode ?? 'None' }}</p>
                </dl>
              </template>
            </UAccordion>
          </UCard>
          <UCard>
            <h2 class="section-label mb-3">Files</h2>
            <UAccordion :items="fileItems">
              <template #file="{ item }">
                <p class="py-2 text-sm">{{ fileLabel(item.event) }}</p>
                <a v-if="fileArtifact(item.event)" class="text-sm hover:text-primary" :href="artifactHref(fileArtifact(item.event)!)">{{ fileArtifact(item.event)!.name }}</a>
              </template>
            </UAccordion>
          </UCard>
          <UCard>
            <h2 class="section-label mb-3">Tests</h2>
            <UAccordion :items="testItems">
              <template #test="{ item }">
                <p class="py-2 text-sm">{{ testLabel(item.event) }}</p>
              </template>
            </UAccordion>
          </UCard>
          <QuestionPanel :project-id="projectId" :run-id="runId" />
          <UCard>
            <h2 class="section-label mb-3">Raw logs</h2>
            <UAlert v-if="logError" title="Unable to load log" :description="logError.message" color="error" class="mb-3" />
            <p class="mb-3 text-sm text-muted">Raw output is secondary and loaded one bounded chunk at a time.</p>
            <UAccordion :items="rawItems">
              <template #raw="{ item }">
                <div class="space-y-2 py-2">
                  <UButton label="Load log" variant="outline" @click="loadChunk(item.chunk.id)" />
                  <pre v-if="logs[item.chunk.id]">{{ logs[item.chunk.id] }}</pre>
                </div>
              </template>
            </UAccordion>
          </UCard>
        </section>
        <aside class="space-y-4">
          <UCard>
            <h2 class="section-label mb-3">Properties</h2>
            <dl class="space-y-3 text-sm">
              <div>
                <dt class="text-muted">Issue</dt>
                <dd>
                  <NuxtLink :to="`/projects/${projectId}/issues/${run.issueId}`" class="font-mono break-all hover:text-primary focus-visible:outline-2 focus-visible:outline-primary">{{ run.issueId }}</NuxtLink>
                </dd>
              </div>
              <div>
                <dt class="text-muted">Agent</dt>
                <dd class="font-mono break-all">{{ run.agentId || 'Unassigned' }}</dd>
              </div>
              <div>
                <dt class="text-muted">Workspace</dt>
                <dd class="font-mono break-all">{{ run.workspaceId }}</dd>
              </div>
              <div>
                <dt class="text-muted">Attempt</dt>
                <dd>{{ run.attempt }}</dd>
              </div>
            </dl>
          </UCard>
          <UCard>
            <h2 class="section-label mb-3">Runtime instances</h2>
            <div v-for="instance in evidence?.runtimeInstances" :key="instance.id" class="mb-3 text-sm">
              <p class="font-mono break-all">{{ instance.id }}</p>
              <p>Runtime <span class="font-mono">{{ instance.runtimeId }}</span></p>
              <p>{{ instance.status }} · runner {{ instance.runnerStatus }}</p>
            </div>
            <p v-if="!evidence?.runtimeInstances.length" class="text-sm text-muted">None</p>
          </UCard>
          <UCard>
            <h2 class="section-label mb-3">Provenance</h2>
            <pre>{{ provenanceText() }}</pre>
          </UCard>
          <UCard>
            <h2 class="section-label mb-3">Artifacts</h2>
            <ul class="space-y-2 text-sm">
              <li v-for="artifact in evidence?.artifacts" :key="artifact.id">
                <a :href="artifactHref(artifact)" class="hover:text-primary focus-visible:outline-2 focus-visible:outline-primary">{{ artifact.name }}</a>
              </li>
            </ul>
          </UCard>
        </aside>
      </div>
    </AsyncState>
  </PageFrame>
</template>
