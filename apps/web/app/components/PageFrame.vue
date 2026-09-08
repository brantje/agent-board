<script setup lang="ts">
import SettingsSidebar from './SettingsSidebar.vue'
import { useSettingsShell } from '../composables/useSettingsShell'

defineProps<{ title: string; description?: string }>()

const settingsShell = useSettingsShell()
</script>
<template>
  <UDashboardPanel id="main-panel" class="min-w-0 w-full" :ui="{ body: 'gap-4 p-4 min-w-0' }">
    <template #header>
      <UDashboardNavbar :title="title"><template #leading><UDashboardSidebarCollapse /></template></UDashboardNavbar>
      <UDashboardToolbar v-if="$slots.actions || description"><p v-if="description" class="text-sm text-muted">{{ description }}</p><div class="ml-auto flex flex-wrap items-center gap-2"><slot name="actions" /></div></UDashboardToolbar>
    </template>
    <template #body>
      <div class="flex min-w-0 w-full flex-col gap-4 lg:flex-row">
        <SettingsSidebar v-if="settingsShell" :project-id="settingsShell.projectId.value" />
        <main id="main-content" class="min-w-0 w-full flex-1"><slot /></main>
      </div>
    </template>
  </UDashboardPanel>
</template>
