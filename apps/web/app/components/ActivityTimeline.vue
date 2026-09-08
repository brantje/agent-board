<script setup lang="ts">
import { computed } from 'vue'
import type { EventEvidence } from '../types/api'
import { compareEvents, eventDescription, eventTitle, isUnknownEvent } from '../utils/events'

type ActivityItem = {
  value: string
  date: string
  title: string
  description: string
  icon: string
  slot?: string
  payload: Record<string, unknown>
  type: string
}

const props = defineProps<{ events: EventEvidence[] }>()

const items = computed((): ActivityItem[] => [...props.events].sort(compareEvents).map(event => ({
  value: event.id,
  date: event.occurredAt,
  title: eventTitle(event),
  description: eventDescription(event),
  icon: isUnknownEvent(event) ? 'i-lucide-circle-alert' : 'i-lucide-activity',
  slot: isUnknownEvent(event) ? 'fallback' : undefined,
  payload: event.payload,
  type: event.type
})))
</script>

<template>
  <UEmpty v-if="!items.length" title="No activity yet" description="Persisted Events appear here, then live updates continue from the last sequence." />
  <UTimeline v-else :items="items" size="xs" color="neutral">
    <template #fallback-description="{ item }">
      <p class="font-mono text-xs text-muted">{{ (item as ActivityItem).type }}</p>
      <pre>{{ JSON.stringify((item as ActivityItem).payload, null, 2) }}</pre>
    </template>
  </UTimeline>
</template>
