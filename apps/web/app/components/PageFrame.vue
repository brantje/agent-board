<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>
<template>
  <UDashboardPanel id="main-panel" :ui="{ body: 'gap-4 p-4' }">
    <template #header>
      <UDashboardNavbar :title="title"><template #leading><UDashboardSidebarCollapse /></template></UDashboardNavbar>
      <UDashboardToolbar v-if="$slots.actions || description"><p v-if="description" class="text-sm text-muted">{{ description }}</p><div class="ml-auto flex flex-wrap items-center gap-2"><slot name="actions" /></div></UDashboardToolbar>
    </template>
    <template #body><main id="main-content" class="min-w-0 flex-1"><slot /></main></template>
  </UDashboardPanel>
</template>
