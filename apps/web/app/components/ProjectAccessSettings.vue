<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type {
  ProjectDirectoryGroup,
  ProjectDirectoryUser,
  ProjectGroupAccess,
  ProjectRole,
  ProjectRoleResponse,
  ProjectUserAccess
} from '../types/project-access'
import { ApiError, apiQuery, apiRequest } from '../utils/api'

const props = defineProps<{ projectId: string; effectiveRole: ProjectRole }>()
const roles: { label: string; value: ProjectRole }[] = [
  { label: 'Viewer', value: 'viewer' },
  { label: 'Member', value: 'member' },
  { label: 'Admin', value: 'admin' }
]
const users = ref<ProjectUserAccess[]>([])
const groups = ref<ProjectGroupAccess[]>([])
const userQuery = ref('')
const groupQuery = ref('')
const userResults = ref<ProjectDirectoryUser[]>([])
const groupResults = ref<ProjectDirectoryGroup[]>([])
const pending = ref(true)
const actionError = ref('')
const loadingKey = ref('')

const base = computed(() => `/api/projects/${props.projectId}/access`)
const isAdmin = computed(() => props.effectiveRole === 'admin')

function accessError(error: unknown) {
  if (error instanceof ApiError && error.code === 'last_project_admin') {
    return 'Every Project must retain at least one active direct User administrator.'
  }
  return error instanceof Error ? error.message : 'Project access could not be updated.'
}

async function load() {
  pending.value = true
  actionError.value = ''
  try {
    if (props.effectiveRole === 'admin') {
      const [userAccess, groupAccess] = await Promise.all([
        apiRequest<ProjectUserAccess[]>(`${base.value}/users`),
        apiRequest<ProjectGroupAccess[]>(`${base.value}/groups`)
      ])
      users.value = userAccess
      groups.value = groupAccess
    } else {
      users.value = []
      groups.value = []
    }
  } catch (error) {
    actionError.value = accessError(error)
  } finally {
    pending.value = false
  }
}

async function setUserRole(user: ProjectUserAccess, role: ProjectRole) {
  loadingKey.value = `user:${user.id}`
  actionError.value = ''
  try {
    await apiRequest<ProjectRoleResponse>(`${base.value}/users/${user.id}`, { method: 'PUT', body: { role } })
    user.role = role
  } catch (error) {
    actionError.value = accessError(error)
  } finally {
    loadingKey.value = ''
  }
}

async function removeUser(user: ProjectUserAccess) {
  loadingKey.value = `user:${user.id}`
  actionError.value = ''
  try {
    await apiRequest<void>(`${base.value}/users/${user.id}`, { method: 'DELETE' })
    users.value = users.value.filter(item => item.id !== user.id)
  } catch (error) {
    actionError.value = accessError(error)
  } finally {
    loadingKey.value = ''
  }
}

async function searchUsers() {
  actionError.value = ''
  try {
    userResults.value = await apiRequest<ProjectDirectoryUser[]>(apiQuery(`${base.value}/directory/users`, { q: userQuery.value }))
  } catch (error) {
    actionError.value = accessError(error)
  }
}

async function addUser(user: ProjectDirectoryUser) {
  loadingKey.value = `candidate-user:${user.id}`
  actionError.value = ''
  try {
    await apiRequest<ProjectRoleResponse>(`${base.value}/users/${user.id}`, { method: 'PUT', body: { role: 'viewer' } })
    if (!users.value.some(item => item.id === user.id)) {
      users.value.push({ ...user, status: 'active', role: 'viewer' })
    }
    userResults.value = userResults.value.filter(item => item.id !== user.id)
  } catch (error) {
    actionError.value = accessError(error)
  } finally {
    loadingKey.value = ''
  }
}

async function setGroupRole(group: ProjectGroupAccess, role: ProjectRole) {
  loadingKey.value = `group:${group.id}`
  actionError.value = ''
  try {
    await apiRequest<ProjectRoleResponse>(`${base.value}/groups/${group.id}`, { method: 'PUT', body: { role } })
    group.role = role
  } catch (error) {
    actionError.value = accessError(error)
  } finally {
    loadingKey.value = ''
  }
}

async function removeGroup(group: ProjectGroupAccess) {
  loadingKey.value = `group:${group.id}`
  actionError.value = ''
  try {
    await apiRequest<void>(`${base.value}/groups/${group.id}`, { method: 'DELETE' })
    groups.value = groups.value.filter(item => item.id !== group.id)
  } catch (error) {
    actionError.value = accessError(error)
  } finally {
    loadingKey.value = ''
  }
}

async function searchGroups() {
  actionError.value = ''
  try {
    groupResults.value = await apiRequest<ProjectDirectoryGroup[]>(apiQuery(`${base.value}/directory/groups`, { q: groupQuery.value }))
  } catch (error) {
    actionError.value = accessError(error)
  }
}

async function addGroup(group: ProjectDirectoryGroup) {
  loadingKey.value = `candidate-group:${group.id}`
  actionError.value = ''
  try {
    await apiRequest<ProjectRoleResponse>(`${base.value}/groups/${group.id}`, { method: 'PUT', body: { role: 'viewer' } })
    if (!groups.value.some(item => item.id === group.id)) groups.value.push({ ...group, role: 'viewer' })
    groupResults.value = groupResults.value.filter(item => item.id !== group.id)
  } catch (error) {
    actionError.value = accessError(error)
  } finally {
    loadingKey.value = ''
  }
}

onMounted(load)
</script>

<template>
  <UCard class="mt-6">
    <template #header>
      <div>
        <h2 class="text-base font-semibold">Access</h2>
        <p class="text-sm text-muted">Project roles combine direct User grants and Group grants; the highest role wins.</p>
      </div>
    </template>

    <p v-if="pending">Loading Project access…</p>
    <template v-else>
      <UAlert v-if="actionError" title="Project access update failed" :description="actionError" color="error" class="mb-4" />
      <UAlert
        :title="`Your effective role is ${effectiveRole}`"
        description="Viewer can read, Member can perform normal workflow mutations, and Admin can manage Project settings and access."
        color="neutral"
        class="mb-4"
      />
      <p v-if="!isAdmin" data-testid="access-readonly" class="text-sm text-muted">Only Project admins can manage User and Group grants.</p>

      <div v-else data-testid="access-management" class="space-y-6">
        <section>
          <h3 class="font-medium">Users</h3>
          <div class="mt-2 flex gap-2">
            <div data-testid="user-search" class="grow"><UInput v-model="userQuery" placeholder="Search active users" /></div>
            <UButton data-testid="search-users" label="Search users" @click="searchUsers" />
          </div>
          <div v-if="userResults.length" class="mt-2 space-y-2">
            <div v-for="candidate in userResults" :key="candidate.id" class="flex items-center justify-between gap-3">
              <span>{{ candidate.displayName }} · {{ candidate.email }}</span>
              <UButton :data-testid="`add-user-${candidate.id}`" label="Add" :loading="loadingKey === `candidate-user:${candidate.id}`" @click="addUser(candidate)" />
            </div>
          </div>
          <div v-if="users.length" class="mt-4 space-y-3">
            <div v-for="user in users" :key="user.id" class="flex items-center gap-3">
              <div class="grow">
                <div class="flex items-center gap-2"><span class="font-medium">{{ user.displayName }}</span><UBadge v-if="user.status === 'disabled'" label="Disabled" color="neutral" /></div>
                <p class="text-sm text-muted">{{ user.email }}</p>
              </div>
              <div :data-testid="`user-role-${user.id}`"><USelect :model-value="user.role" :items="roles" :disabled="loadingKey === `user:${user.id}`" @update:model-value="setUserRole(user, $event as ProjectRole)" /></div>
              <UButton :data-testid="`remove-user-${user.id}`" label="Remove" color="error" variant="soft" :loading="loadingKey === `user:${user.id}`" @click="removeUser(user)" />
            </div>
          </div>
          <UEmpty v-else-if="!userResults.length" title="No direct User grants" description="Search for an active User to add direct Project access." />
        </section>

        <section>
          <h3 class="font-medium">Groups</h3>
          <div class="mt-2 flex gap-2">
            <div data-testid="group-search" class="grow"><UInput v-model="groupQuery" placeholder="Search groups" /></div>
            <UButton data-testid="search-groups" label="Search groups" @click="searchGroups" />
          </div>
          <div v-if="groupResults.length" class="mt-2 space-y-2">
            <div v-for="candidate in groupResults" :key="candidate.id" class="flex items-center justify-between gap-3">
              <span>{{ candidate.name }}</span>
              <UButton :data-testid="`add-group-${candidate.id}`" label="Add" :loading="loadingKey === `candidate-group:${candidate.id}`" @click="addGroup(candidate)" />
            </div>
          </div>
          <div v-if="groups.length" class="mt-4 space-y-3">
            <div v-for="group in groups" :key="group.id" class="flex items-center gap-3">
              <span class="grow font-medium">{{ group.name }}</span>
              <div :data-testid="`group-role-${group.id}`"><USelect :model-value="group.role" :items="roles" :disabled="loadingKey === `group:${group.id}`" @update:model-value="setGroupRole(group, $event as ProjectRole)" /></div>
              <UButton :data-testid="`remove-group-${group.id}`" label="Remove" color="error" variant="soft" :loading="loadingKey === `group:${group.id}`" @click="removeGroup(group)" />
            </div>
          </div>
          <UEmpty v-else-if="!groupResults.length" title="No Group grants" description="Search for a Group to add Project access." />
        </section>
      </div>
    </template>
  </UCard>
</template>
