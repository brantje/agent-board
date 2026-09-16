<script setup lang="ts">
import { computed } from 'vue'
import type { Issue } from '../types/api'
import { formatUpdatedLabel, isFailedIssueRun, isLiveIssueRun, issueCardRunStatus, issuePriority } from '../utils/issues'
import { assigneeIdentityKind, assigneeTypeLabel } from '../utils/identity'

const props = defineProps<{
  issue: Issue
  runStatus?: string | null
}>()

const priority = computed(() => issuePriority(props.issue.priority))
const assignedLabel = computed(() => props.issue.assignedTo?.name || (props.issue.assignedTo ? 'Assignee unavailable' : ''))
const assignedKind = computed(() => props.issue.assignedTo ? assigneeIdentityKind(props.issue.assignedTo.type) : 'user')
const assignedTypeLabel = computed(() => props.issue.assignedTo ? assigneeTypeLabel(props.issue.assignedTo.type) : '')
const runStatusLabel = computed(() => issueCardRunStatus(props.runStatus))
const updatedLabel = computed(() => formatUpdatedLabel(props.issue.updatedAt))
const liveRun = computed(() => isLiveIssueRun(props.runStatus))
const failedRun = computed(() => isFailedIssueRun(props.runStatus))
</script>

<template>
  <NuxtLink :to="`/projects/${issue.projectId}/issues/${issue.id}`" class="block focus-visible:outline-2 focus-visible:outline-primary">
    <UCard :class="{ 'ring-1 ring-primary/35': liveRun }">
      <div class="flex items-center justify-between gap-2">
        <div class="flex min-w-0 items-center gap-1.5">
          <UIcon
            :name="priority.icon"
            class="size-3.5 shrink-0 text-warning"
            :aria-label="priority.label"
          />
          <p class="font-mono text-xs leading-none text-dimmed">{{ issue.id }}</p>
        </div>
        <UBadge
          v-if="failedRun && runStatusLabel"
          :label="runStatusLabel"
          icon="i-lucide-circle-x"
          color="error"
          variant="subtle"
          size="sm"
          class="issue-run-status shrink-0"
          data-issue-run-status
        />
        <div
          v-else-if="runStatusLabel"
          class="flex shrink-0 items-center gap-1.5 text-xs text-muted"
          data-issue-run-status
        >
          <span
            v-if="liveRun"
            class="issue-run-spinner relative inline-flex size-4 shrink-0 items-center justify-center"
            :aria-label="runStatusLabel"
          >
            <span class="issue-run-spinner-ring absolute inset-0 rounded-full border border-primary/25 border-t-primary animate-spin" aria-hidden="true" />
            <UAvatar
              icon="i-lucide-bot"
              size="3xs"
              color="neutral"
              alt=""
              class="issue-identity relative z-10 !size-3"
              aria-hidden="true"
            />
          </span>
          <span v-else class="size-1.5 shrink-0 rounded-full bg-muted" aria-hidden="true" />
          <span>{{ runStatusLabel }}</span>
        </div>
      </div>

      <h3 class="mt-1.5 text-sm font-semibold leading-snug text-highlighted break-words line-clamp-2">{{ issue.title }}</h3>
      <WorkspaceBranch v-if="issue.currentBranch" class="mt-2" :branch="issue.currentBranch" />

      <div class="mt-3 flex items-center justify-between gap-2">
        <div v-if="assignedLabel" class="flex min-w-0 items-center gap-2">
          <IdentityAvatar :kind="assignedKind" :name="assignedLabel" />
          <span class="truncate text-xs text-highlighted">{{ assignedLabel }} · {{ assignedTypeLabel }}</span>
        </div>
        <span v-if="updatedLabel" class="ml-auto shrink-0 text-xs text-muted">{{ updatedLabel }}</span>
      </div>
    </UCard>
  </NuxtLink>
</template>
