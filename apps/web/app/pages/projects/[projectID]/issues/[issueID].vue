<script setup lang="ts">
import { computed } from 'vue'
import { useProjectPermissions } from '../../../../composables/useProjectPermissions'

const route = useRoute()
const projectId = computed(() => String(route.params.projectID))
const issueId = computed(() => String(route.params.issueID))
const { canMutate } = useProjectPermissions(projectId)
</script>
<template>
  <IssueDetail :key="`${projectId}/${issueId}`" :project-id="projectId" :issue-id="issueId" :can-mutate="canMutate" />
</template>
