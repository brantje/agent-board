<script setup lang="ts">const route = useRoute()</script>
<template><ConfigManager :key="String(route.params.projectID)" kind="projects" :resource-id="String(route.params.projectID)" /></template>
