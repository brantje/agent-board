<script setup lang="ts">
import { useDraggable } from '@dnd-kit/vue'
import { ref } from 'vue'
import type { Issue } from '../types/api'
import { boardDragId } from '../utils/board-order'

const props = defineProps<{ issue: Issue; runStatus?: string; disabled?: boolean }>()
const element = ref<HTMLElement | null>(null)
const { isDragging } = useDraggable({
  id: () => boardDragId(props.issue.id),
  element,
  disabled: () => props.disabled ?? false,
  data: () => ({ type: 'issue', issueId: props.issue.id })
})
</script>

<template>
  <div
    ref="element"
    :data-board-draggable="issue.id"
    :class="[
      'rounded transition-opacity',
      disabled ? '' : 'cursor-grab active:cursor-grabbing',
      isDragging ? 'opacity-45' : ''
    ]"
  >
    <IssueCard :issue="issue" :run-status="runStatus" />
  </div>
</template>
