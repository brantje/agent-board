<script setup lang="ts">const route = useRoute()</script>
<template><ConfigManager :key="String(route.params.projectID)" kind="executor-profiles" :project-id="String(route.params.projectID)" /></template>
