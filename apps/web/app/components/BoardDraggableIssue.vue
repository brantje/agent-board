<script setup lang="ts">
import { useDraggable } from '@dnd-kit/vue'
import { ref } from 'vue'
import type { Issue } from '../types/api'
import { boardDragId } from '../utils/board-order'

const props = defineProps<{ issue: Issue; runStatus?: string; disabled?: boolean }>()
const element = ref<HTMLElement | null>(null)
const handle = ref<HTMLElement | null>(null)
const { isDragging } = useDraggable({
  id: () => boardDragId(props.issue.id),
  element,
  handle,
  disabled: () => props.disabled ?? false,
  data: () => ({ type: 'issue', issueId: props.issue.id })
})
</script>

<template>
  <div
    ref="element"
    :data-board-draggable="issue.id"
    :class="[
      'group relative rounded transition-opacity',
      isDragging ? 'opacity-45' : ''
    ]"
  >
    <button
      v-if="!disabled"
      ref="handle"
      type="button"
      data-board-drag-handle
      :aria-label="`Drag ${issue.id}`"
      title="Drag to reorder"
      class="absolute -left-1 top-1/2 z-10 flex size-6 -translate-y-1/2 cursor-grab touch-none items-center justify-center rounded-md border border-default bg-default/95 text-muted opacity-70 shadow-sm transition hover:text-highlighted hover:opacity-100 active:cursor-grabbing focus-visible:outline-2 focus-visible:outline-primary"
      @click.prevent.stop
    >
      <UIcon name="i-lucide-grip-vertical" class="size-3.5" aria-hidden="true" />
    </button>
    <IssueCard :issue="issue" :run-status="runStatus" />
  </div>
</template>
