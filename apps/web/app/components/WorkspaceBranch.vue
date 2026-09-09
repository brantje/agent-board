<script setup lang="ts">
defineProps<{ branch: string }>()
</script>

<template>
  <UBadge
    :label="branch"
    icon="i-lucide-git-branch"
    color="neutral"
    variant="subtle"
    size="sm"
    class="font-mono issue-branch"
  />
</template>
