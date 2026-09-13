<script setup lang="ts">
import { computed } from 'vue'
import { useProjectPermissions } from '../../../composables/useProjectPermissions'

const route = useRoute()
const projectId = computed(() => String(route.params.projectID))
const { canAdmin } = useProjectPermissions(projectId)
</script>
<template>
  <ConfigManager :key="projectId" kind="agents" :project-id="projectId" :can-mutate="canAdmin" />
</template>
