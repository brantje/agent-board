<script setup lang="ts">
import { computed } from 'vue'
import { useProjectPermissions } from '../../../../composables/useProjectPermissions'

const route = useRoute()
const projectId = computed(() => String(route.params.projectID))
const runId = computed(() => String(route.params.runID))
const { canMutate } = useProjectPermissions(projectId)
</script>
<template>
  <RunDetail :key="`${projectId}/${runId}`" :project-id="projectId" :run-id="runId" :can-mutate="canMutate" />
</template>
