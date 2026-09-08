<script setup lang="ts">
const route = useRoute()
</script>
<template>
  <RunDetail :key="`${route.params.projectID}/${route.params.runID}`" :project-id="String(route.params.projectID)" :run-id="String(route.params.runID)" />
</template>
