<script setup lang="ts">
import { computed, provide } from 'vue'
import { settingsShellKey } from '../composables/useSettingsShell'

const route = useRoute()
const projectId = computed(() =>
  typeof route.params.projectID === 'string' ? route.params.projectID : undefined
)

provide(settingsShellKey, { projectId })
</script>

<template>
  <div class="min-w-0 w-full flex-1" data-testid="settings-shell">
    <slot />
  </div>
</template>
