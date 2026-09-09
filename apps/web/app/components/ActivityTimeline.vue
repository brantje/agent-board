<script setup lang="ts">
import { computed } from 'vue'
import type { EventEvidence } from '../types/api'
import { projectRunActivity } from '../utils/events'

const props = defineProps<{ events: EventEvidence[] }>()
const items = computed(() => projectRunActivity(props.events))
</script>

<template>
  <UEmpty v-if="!items.length" title="No activity yet" description="Persisted Events appear here, then live updates continue from the last sequence." />
  <ol v-else class="space-y-2" data-run-activity>
    <li v-for="item in items" :key="item.id" class="min-w-0 text-sm">
      <div v-if="item.kind === 'thought'" class="flex items-start gap-2 py-1 text-muted" data-activity-kind="thought">
        <span class="i-lucide-brain mt-0.5 size-4 shrink-0 text-primary" aria-hidden="true" />
        <p class="min-w-0 italic leading-5">{{ item.message }}</p>
      </div>

      <div v-else-if="item.kind === 'tool'" class="py-1" :data-tool-status="item.status">
        <div class="flex min-w-0 items-baseline gap-2">
          <span class="text-muted" aria-hidden="true">›</span>
          <span class="font-medium">{{ item.label }}</span>
          <span v-if="item.target" class="min-w-0 truncate font-mono text-xs text-muted">{{ item.target }}</span>
          <span v-if="item.status === 'running'" class="ml-auto text-xs text-muted">running</span>
        </div>
        <p v-if="item.resultPreview" class="ml-5 mt-1 truncate text-xs text-muted">
          <span class="font-medium text-default">result:</span> {{ item.resultPreview }}
        </p>
        <p v-else-if="item.reason" class="ml-5 mt-1 text-xs text-error">
          <span class="font-medium">error:</span> {{ item.reason }}
        </p>
        <details v-if="item.summary || item.input" class="ml-5 mt-1 text-xs text-muted">
          <summary class="cursor-pointer select-none">Details</summary>
          <p v-if="item.summary" class="mt-1">{{ item.summary }}</p>
          <pre v-if="item.input" class="mt-1 overflow-auto">{{ JSON.stringify(item.input, null, 2) }}</pre>
        </details>
      </div>

      <div v-else class="py-1" :data-activity-kind="item.unknown ? 'unknown' : 'event'">
        <div class="flex flex-wrap items-baseline gap-x-2">
          <strong class="font-medium">{{ item.title }}</strong>
          <time class="text-xs text-muted">{{ item.occurredAt }}</time>
        </div>
        <p v-if="item.description" class="mt-1 text-muted">{{ item.description }}</p>
        <template v-if="item.unknown">
          <p class="mt-1 font-mono text-xs text-muted">{{ item.event.type }}</p>
          <pre class="mt-1 overflow-auto">{{ JSON.stringify(item.event.payload, null, 2) }}</pre>
        </template>
      </div>
    </li>
  </ol>
</template>
