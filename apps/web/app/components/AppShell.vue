<script setup lang="ts">
import { computed } from 'vue'
import type { AuthUser } from '../types/auth'
import { useProjectPermissions } from '../composables/useProjectPermissions'
import { navigation } from '../utils/navigation'

const route = useRoute()
const currentUser = useState<AuthUser | null>('auth-user')
const projectId = computed(() => typeof route.params.projectID === 'string' ? route.params.projectID : undefined)
const permissions = useProjectPermissions(projectId)
const deploymentAdmin = computed(() => currentUser.value?.deploymentRole === 'admin')
const projectAdmin = computed(() => deploymentAdmin.value || permissions.canAdmin.value)
const links = computed(() => navigation(projectId.value, {
  deploymentAdmin: deploymentAdmin.value,
  projectAdmin: projectAdmin.value
}))
</script>
<template>
  <UDashboardGroup unit="px">
    <UDashboardSidebar collapsible resizable :default-size="224" :min-size="180" :max-size="320" :collapsed-size="64">
      <template #header="{ collapsed }"><UButton to="/projects" icon="i-lucide-panels-top-left" :label="collapsed ? undefined : 'Agent Board'" aria-label="Agent Board projects" color="neutral" variant="ghost" /></template>
      <template #default="{ collapsed }">
        <UNavigationMenu v-if="!projectId" aria-label="Primary navigation" orientation="vertical" :collapsed="collapsed" :items="links.global" />
        <UButton v-else to="/projects" icon="i-lucide-arrow-left" :label="collapsed ? undefined : 'Back to projects'" aria-label="Back to projects" color="neutral" variant="ghost" block class="justify-start" />
        <template v-if="projectId"><USeparator label="Project" /><UNavigationMenu aria-label="Project navigation" orientation="vertical" :collapsed="collapsed" :items="links.project" /></template>
        <UNavigationMenu v-if="links.settings.length" aria-label="Settings navigation" orientation="vertical" :collapsed="collapsed" :items="links.settings" class="mt-auto" data-testid="settings-main-nav" />
      </template>
      <template #footer>
        <ThemeSelector />
        <span class="text-xs text-muted">Self-hosted · v0.1</span>
      </template>
    </UDashboardSidebar>
    <slot />
  </UDashboardGroup>
</template>
