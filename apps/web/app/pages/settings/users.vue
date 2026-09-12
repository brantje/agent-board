<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import { onMounted, reactive, ref } from 'vue'
import type { AuthUser } from '../../types/auth'

const auth = useAuth()
const rows = ref<AuthUser[]>([])
const columns: TableColumn<AuthUser>[] = [
  { accessorKey: 'displayName', header: 'User' },
  { accessorKey: 'deploymentRole', header: 'Role' },
  { accessorKey: 'status', header: 'Status' },
  { id: 'actions', header: 'Actions' }
]
const createForm = reactive({ username: '', email: '', displayName: '' })
const passwordForm = reactive({ userId: '', password: '' })
const oneTimeSecret = ref<{ label: string; token: string; expiresAt: string } | null>(null)
const busy = ref(false)
const errorMessage = ref('')

async function run(action: () => Promise<void>) {
  busy.value = true
  errorMessage.value = ''
  try { await action() } catch (error) { errorMessage.value = error instanceof Error ? error.message : 'The request failed.' } finally { busy.value = false }
}

async function reload() { rows.value = await auth.users() }

async function createUser() {
  await run(async () => {
    const result = await auth.createUser(createForm)
    oneTimeSecret.value = { label: `Setup token for ${result.user.username}`, token: result.setupToken, expiresAt: result.setupTokenExpiresAt }
    createForm.username = ''; createForm.email = ''; createForm.displayName = ''
    await reload()
  })
}

async function issueToken(user: AuthUser, purpose: 'setup' | 'reset') {
  await run(async () => {
    const result = await auth.passwordToken(user.id, purpose)
    oneTimeSecret.value = { label: `${purpose === 'setup' ? 'Setup' : 'Reset'} token for ${user.username}`, token: result.token, expiresAt: result.expiresAt }
  })
}

async function toggleDisabled(user: AuthUser) {
  await run(async () => { await auth.setUserDisabled(user.id, user.status !== 'disabled'); await reload() })
}

async function assignPassword() {
  await run(async () => {
    await auth.setUserPassword(passwordForm.userId, passwordForm.password)
    passwordForm.password = ''
    passwordForm.userId = ''
    await reload()
  })
}

onMounted(() => run(reload))
</script>

<template>
  <SettingsShell>
    <PageFrame title="Users" description="Administer local deployment users. Groups and Project access are configured in later phases.">
      <div class="space-y-4">
        <UAlert v-if="errorMessage" color="error" variant="soft" :description="errorMessage" />
        <UCard v-if="oneTimeSecret">
          <template #header><div class="flex items-center justify-between gap-3"><h2 class="font-semibold">{{ oneTimeSecret.label }}</h2><UButton color="neutral" variant="ghost" @click="oneTimeSecret = null">Dismiss</UButton></div></template>
          <p class="mb-2 text-sm text-warning">Copy this token now. It is displayed only for this response.</p>
          <UInput :model-value="oneTimeSecret.token" readonly class="w-full font-mono" />
          <p class="mt-2 text-xs text-muted">Expires {{ new Date(oneTimeSecret.expiresAt).toLocaleString() }}</p>
        </UCard>

        <UCard>
          <template #header><h2 class="font-semibold">Create pending user</h2></template>
          <form class="grid gap-4 md:grid-cols-3" @submit.prevent="createUser">
            <UFormField label="Display name" required><UInput v-model="createForm.displayName" class="w-full" /></UFormField>
            <UFormField label="Username" required><UInput v-model="createForm.username" class="w-full" /></UFormField>
            <UFormField label="Email" required><UInput v-model="createForm.email" type="email" class="w-full" /></UFormField>
            <div class="md:col-span-3"><UButton type="submit" :loading="busy">Create user</UButton></div>
          </form>
        </UCard>

        <UCard>
          <template #header><h2 class="font-semibold">Local users</h2></template>
          <UEmpty v-if="rows.length === 0" title="No users" />
          <UTable v-else :data="rows" :columns="columns" class="w-full">
            <template #displayName-cell="{ row }">
              <div>
                <p class="font-medium">{{ row.original.displayName }}</p>
                <p class="text-muted">{{ row.original.username }} · {{ row.original.email }}</p>
              </div>
            </template>
            <template #status-cell="{ row }">
              <UBadge color="neutral" variant="soft">{{ row.original.status }}</UBadge>
            </template>
            <template #actions-cell="{ row }">
              <div class="flex flex-wrap gap-2">
                <UButton v-if="row.original.status === 'pending'" size="xs" variant="soft" @click="issueToken(row.original, 'setup')">New setup token</UButton>
                <UButton v-if="row.original.status !== 'pending'" size="xs" variant="soft" @click="issueToken(row.original, 'reset')">Reset token</UButton>
                <UButton size="xs" variant="soft" @click="passwordForm.userId = row.original.id">Set password</UButton>
                <UButton size="xs" :color="row.original.status === 'disabled' ? 'success' : 'error'" variant="soft" @click="toggleDisabled(row.original)">{{ row.original.status === 'disabled' ? 'Re-enable' : 'Disable' }}</UButton>
              </div>
            </template>
          </UTable>
        </UCard>

        <UCard v-if="passwordForm.userId">
          <template #header><h2 class="font-semibold">Assign password</h2></template>
          <form class="flex flex-col gap-3 sm:flex-row sm:items-end" @submit.prevent="assignPassword">
            <UFormField label="New password" required class="flex-1"><UInput v-model="passwordForm.password" type="password" autocomplete="new-password" class="w-full" /></UFormField>
            <UButton type="submit" :loading="busy">Assign and require change</UButton><UButton color="neutral" variant="ghost" @click="passwordForm.userId = ''; passwordForm.password = ''">Cancel</UButton>
          </form>
        </UCard>
      </div>
    </PageFrame>
  </SettingsShell>
</template>
