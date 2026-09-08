<script setup lang="ts">const route = useRoute()</script>
<template>
  <SettingsShell>
    <ConfigManager :key="String(route.params.projectID)" kind="projects" :resource-id="String(route.params.projectID)" />
  </SettingsShell>
</template>
