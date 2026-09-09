<script setup lang="ts">const route = useRoute()</script>
<template>
  <SettingsShell>
    <ProjectSettings :key="String(route.params.projectID)" :project-id="String(route.params.projectID)" />
  </SettingsShell>
</template>
