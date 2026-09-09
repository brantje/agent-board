<script setup lang="ts">const route = useRoute()</script>
<template>
  <SettingsShell>
    <ConfigManager :key="String(route.params.projectID)" kind="providers" :project-id="String(route.params.projectID)" />
  </SettingsShell>
</template>
