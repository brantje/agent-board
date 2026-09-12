<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { computed, onMounted, reactive, ref } from 'vue'
import type { AuthUser, Group } from '../../types/auth'

const auth = useAuth()
const groups = ref<Group[]>([])
const users = ref<AuthUser[]>([])
const members = ref<AuthUser[]>([])
const selectedGroupId = ref('')
const createForm = reactive({ name: '' })
const renameForm = reactive({ name: '' })
const memberSearch = ref('')
const selectedUserId = ref('')
const loading = ref(true)
const memberLoading = ref(false)
const busy = ref(false)
const confirmDelete = ref(false)
const errorMessage = ref('')
let memberLoadGeneration = 0

const memberColumns: TableColumn<AuthUser>[] = [
  { accessorKey: 'displayName', header: 'User' },
  { accessorKey: 'status', header: 'Status' },
  { id: 'actions', header: 'Actions' }
]

const selectedGroup = computed(() => groups.value.find(group => group.id === selectedGroupId.value) ?? null)
const groupOptions = computed(() => groups.value.map(group => ({ label: group.name, value: group.id })))
const memberIds = computed(() => new Set(members.value.map(member => member.id)))
const userOptions = computed(() => {
  const query = memberSearch.value.trim().toLowerCase()
  return users.value
    .filter(user => !memberIds.value.has(user.id))
    .filter(user => !query || `${user.displayName} ${user.username} ${user.email}`.toLowerCase().includes(query))
    .map(user => ({
      label: `${user.displayName} (${user.username})${user.status === 'disabled' ? ' — disabled' : ''}`,
      value: user.id
    }))
})

function requestErrorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'The request failed.'
}

async function run(action: () => Promise<void>) {
  busy.value = true
  errorMessage.value = ''
  try {
    await action()
  } catch (error) {
    errorMessage.value = requestErrorMessage(error)
  } finally {
    busy.value = false
  }
}

async function loadMembers(clearExisting = false) {
  const groupId = selectedGroupId.value
  const generation = ++memberLoadGeneration
  if (clearExisting) members.value = []
  if (!groupId) {
    members.value = []
    memberLoading.value = false
    return
  }

  memberLoading.value = true
  try {
    const nextMembers = await auth.groupMembers(groupId)
    if (generation === memberLoadGeneration && selectedGroupId.value === groupId) {
      members.value = nextMembers
    }
  } catch (error) {
    if (generation === memberLoadGeneration && selectedGroupId.value === groupId) {
      errorMessage.value = requestErrorMessage(error)
    }
  } finally {
    if (generation === memberLoadGeneration && selectedGroupId.value === groupId) {
      memberLoading.value = false
    }
  }
}

async function reload() {
  loading.value = true
  errorMessage.value = ''
  try {
    const [nextGroups, nextUsers] = await Promise.all([auth.groups(), auth.users()])
    groups.value = nextGroups
    users.value = nextUsers
    if (!nextGroups.some(group => group.id === selectedGroupId.value)) selectedGroupId.value = nextGroups[0]?.id ?? ''
    renameForm.name = selectedGroup.value?.name ?? ''
    await loadMembers(true)
  } catch (error) {
    errorMessage.value = requestErrorMessage(error)
  } finally {
    loading.value = false
  }
}

async function selectGroup(groupId: string) {
  selectedGroupId.value = groupId
  selectedUserId.value = ''
  confirmDelete.value = false
  errorMessage.value = ''
  renameForm.name = selectedGroup.value?.name ?? ''
  await loadMembers(true)
}

async function createGroup() {
  await run(async () => {
    const created = await auth.createGroup(createForm.name)
    createForm.name = ''
    groups.value = [...groups.value.filter(group => group.id !== created.id), created]
    selectedGroupId.value = created.id
    selectedUserId.value = ''
    memberSearch.value = ''
    renameForm.name = created.name
    await loadMembers(true)
  })
}

async function renameGroup() {
  if (!selectedGroup.value) return
  await run(async () => {
    const updated = await auth.updateGroup(selectedGroup.value!.id, renameForm.name)
    groups.value = groups.value.map(group => group.id === updated.id ? updated : group)
    renameForm.name = updated.name
  })
}

async function deleteSelectedGroup() {
  if (!selectedGroup.value) return
  const groupId = selectedGroup.value.id
  await run(async () => {
    await auth.deleteGroup(groupId)
    groups.value = groups.value.filter(group => group.id !== groupId)
    confirmDelete.value = false
    if (selectedGroupId.value !== groupId) return
    selectedGroupId.value = groups.value[0]?.id ?? ''
    selectedUserId.value = ''
    memberSearch.value = ''
    renameForm.name = selectedGroup.value?.name ?? ''
    await loadMembers(true)
  })
}

async function addMember() {
  if (!selectedGroup.value || !selectedUserId.value) return
  const groupId = selectedGroup.value.id
  const userId = selectedUserId.value
  const user = users.value.find(candidate => candidate.id === userId)
  await run(async () => {
    await auth.addGroupMember(groupId, userId)
    if (selectedGroupId.value !== groupId) return
    if (user && !members.value.some(member => member.id === user.id)) {
      members.value = [...members.value, user]
    }
    selectedUserId.value = ''
    memberSearch.value = ''
    await loadMembers()
  })
}

async function removeMember(user: AuthUser) {
  if (!selectedGroup.value) return
  const groupId = selectedGroup.value.id
  await run(async () => {
    await auth.removeGroupMember(groupId, user.id)
    if (selectedGroupId.value !== groupId) return
    members.value = members.value.filter(member => member.id !== user.id)
    await loadMembers()
  })
}

onMounted(reload)
</script>

<template>
  <SettingsShell>
    <PageFrame title="Groups" description="Manage deployment-global groups of human users. Project access roles are configured separately in a later phase.">
      <div class="space-y-4">
        <UAlert v-if="loading" title="Loading groups" description="Loading Groups and Users…" />
        <UAlert v-if="errorMessage" color="error" variant="soft" title="Group request failed" :description="errorMessage" />

        <UCard>
          <template #header><h2 class="font-semibold">Create group</h2></template>
          <form data-testid="create-group" class="flex flex-col gap-3 sm:flex-row sm:items-end" @submit.prevent="createGroup">
            <UFormField label="Name" required class="flex-1"><UInput v-model="createForm.name" class="w-full" /></UFormField>
            <UButton type="submit" :loading="busy" :disabled="!createForm.name.trim()">Create group</UButton>
          </form>
        </UCard>

        <UCard>
          <template #header><h2 class="font-semibold">Groups</h2></template>
          <UEmpty v-if="!loading && groups.length === 0" title="No groups" description="Create a group to organize deployment users." />
          <div v-else-if="groups.length" class="space-y-4">
            <UFormField label="Group">
              <USelect :model-value="selectedGroupId" :items="groupOptions" class="w-full" @update:model-value="selectGroup" />
            </UFormField>

            <div v-if="selectedGroup" class="grid gap-4 lg:grid-cols-2">
              <form class="space-y-3" @submit.prevent="renameGroup">
                <UFormField label="Group name"><UInput v-model="renameForm.name" class="w-full" /></UFormField>
                <UButton type="submit" :loading="busy" :disabled="!renameForm.name.trim()">Save name</UButton>
              </form>
              <div class="space-y-3">
                <p class="text-sm text-muted">Deleting a Group removes its memberships. It does not delete Users.</p>
                <UButton v-if="!confirmDelete" color="error" variant="soft" @click="confirmDelete = true">Delete group</UButton>
                <div v-else class="flex flex-wrap items-center gap-2">
                  <UAlert title="Confirm deletion" description="This removes the Group and its memberships." />
                  <UButton color="error" :loading="busy" @click="deleteSelectedGroup">Confirm delete</UButton>
                  <UButton color="neutral" variant="ghost" @click="confirmDelete = false">Cancel</UButton>
                </div>
              </div>
            </div>
          </div>
        </UCard>

        <UCard v-if="selectedGroup">
          <template #header><h2 class="font-semibold">Members · {{ selectedGroup.name }}</h2></template>
          <div class="mb-4 grid gap-3 md:grid-cols-[1fr_1fr_auto] md:items-end">
            <UFormField label="Search users"><UInput v-model="memberSearch" placeholder="Name, username or email" class="w-full" /></UFormField>
            <UFormField label="User"><USelect v-model="selectedUserId" :items="userOptions" class="w-full" /></UFormField>
            <UButton :loading="busy" :disabled="!selectedUserId || memberLoading" @click="addMember">Add member</UButton>
          </div>
          <UAlert v-if="memberLoading && members.length === 0" title="Loading members" description="Loading Group members…" />
          <UEmpty v-else-if="members.length === 0" title="No group members" />
          <UTable v-else :data="members" :columns="memberColumns" class="w-full">
            <template #displayName-cell="{ row }">
              <div>
                <p class="font-medium">{{ row.original.displayName }}</p>
                <p class="text-muted">{{ row.original.username }} · {{ row.original.email }}</p>
              </div>
            </template>
            <template #status-cell="{ row }">
              <UBadge :color="row.original.status === 'disabled' ? 'warning' : 'neutral'" variant="soft">{{ row.original.status }}</UBadge>
            </template>
            <template #actions-cell="{ row }">
              <UButton size="xs" color="error" variant="soft" :loading="busy" @click="removeMember(row.original)">Remove</UButton>
            </template>
          </UTable>
        </UCard>
      </div>
    </PageFrame>
  </SettingsShell>
</template>
