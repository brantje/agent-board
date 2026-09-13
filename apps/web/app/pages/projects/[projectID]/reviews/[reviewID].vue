<script setup lang="ts">
import { computed } from 'vue'
import { useProjectPermissions } from '../../../../composables/useProjectPermissions'

const route = useRoute()
const projectId = computed(() => String(route.params.projectID))
const reviewId = computed(() => String(route.params.reviewID))
const { canMutate } = useProjectPermissions(projectId)
</script>
<template>
  <ReviewDetail :key="`${projectId}/${reviewId}`" :project-id="projectId" :review-id="reviewId" :can-mutate="canMutate" />
</template>
