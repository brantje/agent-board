<script setup lang="ts">
const route = useRoute()
</script>
<template>
  <RunList :key="String(route.params.projectID)" :project-id="String(route.params.projectID)" />
</template>
