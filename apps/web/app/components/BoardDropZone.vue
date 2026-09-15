<script setup lang="ts">
import { useDroppable } from '@dnd-kit/vue'
import { ref } from 'vue'
import { boardDropZoneId } from '../utils/board-order'

const props = withDefaults(defineProps<{ status: string; index: number; disabled?: boolean; empty?: boolean }>(), {
  disabled: false,
  empty: false
})
const element = ref<HTMLElement | null>(null)
const { isDropTarget } = useDroppable({
  id: () => boardDropZoneId(props.status, props.index),
  element,
  disabled: () => props.disabled,
  data: () => ({ type: 'board-gap', status: props.status, index: props.index })
})
</script>

<template>
  <div
    ref="element"
    :data-board-drop-zone="`${status}:${index}`"
    aria-hidden="true"
    :class="[
      'rounded transition-all',
      empty ? 'min-h-16' : 'h-2',
      isDropTarget ? 'min-h-8 bg-primary/10 ring-1 ring-primary/40' : ''
    ]"
  />
</template>
