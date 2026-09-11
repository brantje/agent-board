<script setup lang="ts">
import { computed } from 'vue'
import { runAgentInfo } from '../utils/runs'

const props = defineProps<{ provenance?: Record<string, unknown> | null }>()
const agent = computed(() => runAgentInfo(props.provenance))
</script>

<template>
  <UCard data-run-agent>
    <h2 class="section-label mb-3">Agent</h2>
    <div v-if="agent" class="space-y-4 text-sm">
      <div class="flex min-w-0 items-center gap-2">
        <UAvatar v-if="agent.name" :alt="agent.name" size="xs" class="issue-identity" />
        <p class="min-w-0 font-medium">{{ agent.name || 'Unnamed agent' }}</p>
      </div>
      <dl class="space-y-2">
        <div v-if="agent.engine" class="flex items-start justify-between gap-3">
          <dt class="shrink-0 text-muted">Engine</dt>
          <dd class="min-w-0 text-right">{{ agent.engine }}</dd>
        </div>
        <div v-if="agent.model" class="flex items-start justify-between gap-3">
          <dt class="shrink-0 text-muted">Model</dt>
          <dd class="min-w-0 break-all text-right font-mono">{{ agent.model }}</dd>
        </div>
        <div v-if="agent.provider" class="flex items-start justify-between gap-3">
          <dt class="shrink-0 text-muted">Provider</dt>
          <dd class="min-w-0 text-right">{{ agent.provider }}</dd>
        </div>
        <div v-if="agent.runtime" class="flex items-start justify-between gap-3">
          <dt class="shrink-0 text-muted">Runtime</dt>
          <dd class="min-w-0 text-right">{{ agent.runtime }}</dd>
        </div>
        <div v-if="agent.runner" class="flex items-start justify-between gap-3">
          <dt class="shrink-0 text-muted">Runner</dt>
          <dd class="min-w-0 text-right">{{ agent.runner }}</dd>
        </div>
      </dl>
    </div>
    <p v-else class="text-sm text-muted">No agent recorded.</p>
  </UCard>
</template>
