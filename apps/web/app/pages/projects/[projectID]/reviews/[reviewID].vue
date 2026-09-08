<script setup lang="ts">
const route = useRoute()
</script>
<template>
  <ReviewDetail :key="`${route.params.projectID}/${route.params.reviewID}`" :project-id="String(route.params.projectID)" :review-id="String(route.params.reviewID)" />
</template>
