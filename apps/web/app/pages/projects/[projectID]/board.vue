<script setup lang="ts">
import { computed } from 'vue'
import { useProjectPermissions } from '../../../composables/useProjectPermissions'

const route = useRoute()
const projectId = computed(() => String(route.params.projectID))
const { canMutate } = useProjectPermissions(projectId)
</script>
<template>
  <ProjectBoard :key="projectId" :project-id="projectId" :can-mutate="canMutate" />
</template>
