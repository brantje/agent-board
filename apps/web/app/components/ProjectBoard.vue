<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Issue, Project, Run } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { boardColumns, latestRun, placeIssueOnBoard } from '../utils/issues'
import { isBoardActivityEvent, applyCurrentBranchToIssues } from '../utils/events'
import { useResource } from '../composables/useResource'
import { useProjectEvents } from '../composables/useProjectEvents'

const props = withDefaults(defineProps<{ projectId: string; canMutate?: boolean }>(), { canMutate: true })
const project = useResource<Project>(() => apiPath('projects', undefined, props.projectId))
const issues = useResource<Issue[]>(() => apiPath('issues', props.projectId))
const runs = useResource<Run[]>(() => apiPath('runs', props.projectId))
const search = ref('')
const open = ref(false)
const draggingIssueId = ref<string | null>(null)
const moving = ref(false)
const moveError = ref<Error | null>(null)

const title = computed(() => project.data.value ? `${project.data.value.name} / Board` : 'Project Board')
const columns = computed(() => boardColumns(issues.data.value || [], search.value))
const pending = computed(() => project.pending.value || issues.pending.value)
const error = computed(() => project.error.value || issues.error.value)
const runsError = computed(() => runs.error.value)
const canReorder = computed(() => props.canMutate && !moving.value && !search.value.trim())

type BoardMoveDirection = 'up' | 'down' | 'left' | 'right'

function runStatus(issue: Issue) {
  const items = runs.data.value
  if (!Array.isArray(items)) return undefined
  return latestRun(items, issue.id)?.status
}

async function refreshAll() {
  await Promise.all([project.refresh(), issues.refresh(), runs.refresh()])
}

async function created() {
  open.value = false
  await refreshAll()
}

function startDrag(event: DragEvent, issueId: string) {
  if (!canReorder.value) {
    event.preventDefault()
    return
  }
  draggingIssueId.value = issueId
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', issueId)
  }
}

function endDrag() {
  draggingIssueId.value = null
}

function allowDrop(event: DragEvent) {
  if (!canReorder.value || !draggingIssueId.value) return
  event.preventDefault()
  if (event.dataTransfer) event.dataTransfer.dropEffect = 'move'
}

async function persistPlacement(issueId: string, status: string, beforeIssueId: string | null = null) {
  const current = issues.data.value
  if (!issueId || !canReorder.value || !Array.isArray(current)) return

  const optimistic = placeIssueOnBoard(current, issueId, status, beforeIssueId)
  if (optimistic === current) return

  issues.data.value = optimistic
  moving.value = true
  moveError.value = null
  try {
    await apiRequest<Issue>(`${apiPath('issues', props.projectId, issueId)}/board`, {
      method: 'PATCH',
      body: { status, beforeIssueId }
    })
    await Promise.all([issues.refresh(), runs.refresh()])
  } catch (value) {
    moveError.value = value instanceof Error ? value : new Error('Unable to move issue.')
    // Re-read durable state rather than restoring a stale pre-mutation snapshot.
    // A realtime refresh may have advanced the board while this request was in flight.
    await Promise.all([issues.refresh(), runs.refresh()])
  } finally {
    moving.value = false
    draggingIssueId.value = null
  }
}

function dropBefore(event: DragEvent, status: string, beforeIssueId: string) {
  const issueId = draggingIssueId.value
  if (!canReorder.value || !issueId) return
  event.preventDefault()
  void persistPlacement(issueId, status, beforeIssueId)
}

function dropAtEnd(event: DragEvent, status: string) {
  const issueId = draggingIssueId.value
  if (!canReorder.value || !issueId) return
  event.preventDefault()
  void persistPlacement(issueId, status)
}

function keyboardPlacement(issueId: string, direction: BoardMoveDirection) {
  if (!canReorder.value) return null
  const sourceColumnIndex = columns.value.findIndex(column => column.issues.some(issue => issue.id === issueId))
  if (sourceColumnIndex < 0) return null
  const sourceColumn = columns.value[sourceColumnIndex]
  if (!sourceColumn) return null
  const sourceIndex = sourceColumn.issues.findIndex(issue => issue.id === issueId)
  if (sourceIndex < 0) return null

  if (direction === 'up') {
    const before = sourceColumn.issues[sourceIndex - 1]
    return before ? { status: sourceColumn.status, beforeIssueId: before.id } : null
  }
  if (direction === 'down') {
    if (sourceIndex >= sourceColumn.issues.length - 1) return null
    return { status: sourceColumn.status, beforeIssueId: sourceColumn.issues[sourceIndex + 2]?.id ?? null }
  }

  const targetColumnIndex = direction === 'left' ? sourceColumnIndex - 1 : sourceColumnIndex + 1
  const targetColumn = columns.value[targetColumnIndex]
  if (!targetColumn) return null
  return {
    status: targetColumn.status,
    beforeIssueId: targetColumn.issues[Math.min(sourceIndex, targetColumn.issues.length)]?.id ?? null
  }
}

function canKeyboardMove(issueId: string, direction: BoardMoveDirection) {
  return keyboardPlacement(issueId, direction) !== null
}

function moveWithKeyboard(issueId: string, direction: BoardMoveDirection) {
  const placement = keyboardPlacement(issueId, direction)
  if (!placement) return
  void persistPlacement(issueId, placement.status, placement.beforeIssueId)
}

useProjectEvents(() => props.projectId, async event => {
  if (event.type === 'git.branch_checked_out') {
    const next = applyCurrentBranchToIssues(issues.data.value || [], event)
    if (next !== issues.data.value) {
      issues.data.value = next
      return
    }
  }
  if (!isBoardActivityEvent(event.type)) return
  await refreshAll()
})
</script>

<template>
  <PageFrame :title="title" description="Durable work · six Issue workflow states">
    <template #actions>
      <UInput v-model="search" aria-label="Filter issues" placeholder="Filter issues…" icon="i-lucide-search" />
      <UButton label="Refresh" variant="outline" color="neutral" @click="refreshAll" />
      <UButton v-if="canMutate" label="New issue" icon="i-lucide-plus" @click="open = true" />
    </template>

    <AsyncState :pending="pending" :error="error" @retry="refreshAll">
      <UAlert
        v-if="runsError"
        title="Run status unavailable"
        :description="runsError.message"
        color="warning"
        class="mb-3"
        :actions="[{ label: 'Retry', onClick: () => runs.refresh() }]"
      />
      <UAlert
        v-if="moveError"
        title="Unable to move issue"
        :description="moveError.message"
        color="error"
        class="mb-3"
      />
      <p v-if="canMutate && search.trim()" class="mb-3 text-xs text-muted">Clear the filter to reorder issues.</p>
      <div class="flex min-h-[calc(100dvh-10rem)] gap-3 overflow-x-auto pb-3" role="region" aria-label="Issue board" tabindex="0">
        <section
          v-for="column in columns"
          :key="column.status"
          :data-status="column.status"
          :class="['w-64 min-w-64 flex-1 border border-default', column.surface]"
          @dragover="allowDrop"
          @drop="dropAtEnd($event, column.status)"
        >
          <header class="flex items-center justify-between gap-2 border-b border-default p-3">
            <h2 class="section-label flex min-w-0 items-center gap-1.5">
              <UIcon :name="column.icon" :class="['size-3.5 shrink-0', column.textClass]" aria-hidden="true" />
              {{ column.label }}
            </h2>
            <UBadge :label="String(column.issues.length)" color="neutral" variant="subtle" />
          </header>
          <div class="min-h-16 space-y-2 p-2">
            <div
              v-for="issue in column.issues"
              :key="issue.id"
              :data-board-issue="issue.id"
              :draggable="canReorder"
              :class="{ 'cursor-grab': canReorder, 'opacity-50': draggingIssueId === issue.id }"
              @dragstart="startDrag($event, issue.id)"
              @dragend="endDrag"
              @dragover.stop="allowDrop"
              @drop.stop="dropBefore($event, column.status, issue.id)"
            >
              <IssueCard
                :issue="issue"
                :run-status="runStatus(issue)"
              />
              <div v-if="canMutate && !search.trim()" class="mt-1 flex justify-end gap-1" aria-label="Board position controls">
                <button
                  type="button"
                  class="rounded px-1 text-xs text-muted disabled:opacity-30"
                  :aria-label="`Move ${issue.id} left`"
                  :disabled="!canKeyboardMove(issue.id, 'left')"
                  @click="moveWithKeyboard(issue.id, 'left')"
                >←</button>
                <button
                  type="button"
                  class="rounded px-1 text-xs text-muted disabled:opacity-30"
                  :aria-label="`Move ${issue.id} up`"
                  :disabled="!canKeyboardMove(issue.id, 'up')"
                  @click="moveWithKeyboard(issue.id, 'up')"
                >↑</button>
                <button
                  type="button"
                  class="rounded px-1 text-xs text-muted disabled:opacity-30"
                  :aria-label="`Move ${issue.id} down`"
                  :disabled="!canKeyboardMove(issue.id, 'down')"
                  @click="moveWithKeyboard(issue.id, 'down')"
                >↓</button>
                <button
                  type="button"
                  class="rounded px-1 text-xs text-muted disabled:opacity-30"
                  :aria-label="`Move ${issue.id} right`"
                  :disabled="!canKeyboardMove(issue.id, 'right')"
                  @click="moveWithKeyboard(issue.id, 'right')"
                >→</button>
              </div>
            </div>
            <p v-if="!column.issues.length" class="px-2 py-4 text-xs text-muted">{{ search ? 'No matching issues' : 'No issues' }}</p>
          </div>
        </section>
      </div>
    </AsyncState>

    <UModal v-if="canMutate" v-model:open="open" title="New issue" description="Create work in this Project.">
      <template #body>
        <IssueEditor :project-id="projectId" @saved="created" @cancel="open = false" />
      </template>
    </UModal>
  </PageFrame>
</template>
