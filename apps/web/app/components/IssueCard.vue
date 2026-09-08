<script setup lang="ts">
import type {Issue} from '../types/api'
defineProps<{issue:Issue;agentName?:string}>()
</script>
<template><UCard><h3 class="text-sm font-medium break-words"><NuxtLink :to="`/projects/${issue.projectId}/issues/${issue.id}`" class="hover:text-primary focus-visible:outline-2 focus-visible:outline-primary">{{issue.title}}</NuxtLink></h3><div class="mt-3 flex items-center justify-between gap-2 text-xs"><UBadge color="neutral" variant="subtle" :label="issue.status.replaceAll('_',' ')"/><span class="truncate text-muted">{{agentName || (issue.assignedAgentId ? 'Assigned Agent' : 'Unassigned')}}</span></div><p class="mt-2 text-xs font-mono text-dimmed">{{issue.id}}</p></UCard></template>
