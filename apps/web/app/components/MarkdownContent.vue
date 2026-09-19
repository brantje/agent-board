<script setup lang="ts">
import { computed } from 'vue'
import { renderMarkdown } from '../utils/markdown'

const props = defineProps<{ content: string }>()
const html = computed(() => renderMarkdown(props.content))
</script>

<template>
  <div class="markdown-content break-words text-sm leading-6" v-html="html" />
</template>

<style scoped>
.markdown-content :deep(p + p),
.markdown-content :deep(pre),
.markdown-content :deep(blockquote),
.markdown-content :deep(ul),
.markdown-content :deep(ol) {
  margin-top: 0.75rem;
}

.markdown-content :deep(pre) {
  overflow-x: auto;
  border-radius: 0.375rem;
  background: var(--ui-bg-elevated);
  padding: 0.75rem;
}

.markdown-content :deep(code) {
  font-family: var(--font-mono);
}

.markdown-content :deep(a) {
  color: var(--ui-primary);
  text-decoration: underline;
}

.markdown-content :deep(blockquote) {
  border-inline-start: 2px solid var(--ui-border);
  padding-inline-start: 0.75rem;
  color: var(--ui-text-muted);
}
</style>
