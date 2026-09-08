<script setup lang="ts">
defineProps<{ pending?: boolean; error?: Error; empty?: boolean; emptyTitle?: string }>()
defineEmits<{ retry: [] }>()
</script>
<template>
  <div v-if="pending" role="status" aria-label="Loading" class="space-y-3 py-4"><USkeleton class="h-6 w-1/3" /><USkeleton class="h-24 w-full" /><span class="sr-only">Loading…</span></div>
  <UAlert v-else-if="error" color="error" title="Unable to load" :description="error.message" :actions="[{ label: 'Retry', onClick: () => $emit('retry') }]" />
  <UEmpty v-else-if="empty" :title="emptyTitle || 'Nothing here yet'" description="New work will appear here when it is created." />
  <slot v-else />
</template>
