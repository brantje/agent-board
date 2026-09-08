<script setup lang="ts">
import { computed } from 'vue'
import type { Issue } from '../types/api'
import { issuePriority } from '../utils/issues'

const props = defineProps<{ issue: Issue; agentName?: string }>()

const priority = computed(() => issuePriority(props.issue.priority))
const assignedLabel = computed(() => props.agentName || (props.issue.assignedAgentId ? 'Assigned Agent' : ''))
</script>

<template>
  <NuxtLink :to="`/projects/${issue.projectId}/issues/${issue.id}`" class="block focus-visible:outline-2 focus-visible:outline-primary">
    <UCard>
      <p class="font-mono text-xs leading-none text-dimmed">{{ issue.id }}</p>
      <h3 class="mt-1.5 text-sm font-semibold leading-snug text-highlighted break-words line-clamp-2">{{ issue.title }}</h3>
      <p v-if="issue.description.trim()" class="mt-1 truncate text-xs text-muted">{{ issue.description }}</p>
      <div class="mt-3 flex items-center gap-2">
        <UAvatar v-if="assignedLabel" :alt="assignedLabel" :aria-label="assignedLabel" size="2xs" class="issue-identity" />
        <UBadge
          :label="priority.label"
          :icon="priority.icon"
          color="warning"
          :variant="priority.variant"
          size="sm"
          class="issue-priority"
        />
      </div>
    </UCard>
  </NuxtLink>
</template>
