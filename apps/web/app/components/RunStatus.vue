<script setup lang="ts">
import { computed } from 'vue'
import { runActivityStatusLabel, runStatusPresentation } from '../utils/runs'

const props = withDefaults(defineProps<{
  status: string
  live?: boolean
  label?: string
  size?: 'sm' | 'md' | 'lg'
}>(), {
  size: 'md'
})

const presentation = computed(() => runStatusPresentation(props.status))
const text = computed(() => props.label ?? runActivityStatusLabel(props.status))
const showLive = computed(() => props.live === true && presentation.value.live)
</script>

<template>
  <UBadge
    :label="text"
    :icon="presentation.icon"
    :color="presentation.color"
    :size="size"
    variant="subtle"
    class="run-status"
    :data-run-status="status"
  >
    <template v-if="showLive" #trailing>
      <UIcon name="i-lucide-loader-circle" class="animate-spin" aria-hidden="true" />
      <span class="sr-only">live</span>
    </template>
  </UBadge>
</template>
