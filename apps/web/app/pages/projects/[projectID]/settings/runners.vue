<script setup lang="ts">
import { computed } from 'vue'

const route = useRoute()
const projectId = computed(() => String(route.params.projectID))
</script>

<template>
  <SettingsShell>
    <ProjectRunnerSettings :key="projectId" :project-id="projectId" />
  </SettingsShell>
</template>
