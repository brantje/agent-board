<script setup lang="ts">
import { computed, onMounted, reactive, ref, watchEffect } from 'vue'
import type { AuthSession } from '../types/auth'

const auth = useAuth()
const profile = reactive({ username: '', email: '', displayName: '' })
const currentPassword = ref('')
const password = ref('')
const sessionRows = ref<AuthSession[]>([])
const busy = ref(false)
const errorMessage = ref('')
const successMessage = ref('')
const forced = computed(() => Boolean(auth.user.value?.forcePasswordChange))

watchEffect(() => {
  const user = auth.user.value
  if (!user) return
  profile.username = user.username
  profile.email = user.email
  profile.displayName = user.displayName
})

async function run(action: () => Promise<unknown>, success = '') {
  busy.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    await action()
    successMessage.value = success
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : 'The request failed.'
  } finally {
    busy.value = false
  }
}

async function loadSessions() {
  sessionRows.value = await auth.sessions()
}

async function saveProfile() {
  await run(() => auth.updateProfile(profile), 'Profile updated.')
}

async function savePassword() {
  await run(async () => {
    await auth.changePassword(forced.value ? undefined : currentPassword.value, password.value)
    currentPassword.value = ''
    password.value = ''
    await navigateTo('/auth/login')
  })
}

async function revoke(id: string) {
  await run(async () => {
    await auth.revokeSession(id)
    await loadSessions()
  }, 'Session revoked.')
}

async function logoutOthers() {
  await run(async () => {
    await auth.logoutOthers()
    await loadSessions()
  }, 'Other sessions logged out.')
}

async function logout() {
  await auth.logout()
  await navigateTo('/auth/login')
}

onMounted(async () => {
  if (!forced.value) await run(loadSessions)
})
</script>

<template>
  <PageFrame title="Account" description="Manage your profile, password and active sessions.">
    <div class="space-y-4">
      <UAlert v-if="forced" color="warning" variant="soft" title="Password change required" description="Change your password before continuing to the rest of Agent Board." />
      <UAlert v-if="errorMessage" color="error" variant="soft" :description="errorMessage" />
      <UAlert v-if="successMessage" color="success" variant="soft" :description="successMessage" />

      <UCard v-if="!forced">
        <template #header><h2 class="font-semibold">Profile</h2></template>
        <form class="grid gap-4 md:grid-cols-2" @submit.prevent="saveProfile">
          <UFormField label="Display name" required><UInput v-model="profile.displayName" class="w-full" /></UFormField>
          <UFormField label="Username" required><UInput v-model="profile.username" autocomplete="username" class="w-full" /></UFormField>
          <UFormField label="Email" required class="md:col-span-2"><UInput v-model="profile.email" type="email" autocomplete="email" class="w-full" /></UFormField>
          <div class="md:col-span-2"><UButton type="submit" :loading="busy">Save profile</UButton></div>
        </form>
      </UCard>

      <UCard>
        <template #header><h2 class="font-semibold">{{ forced ? 'Required password change' : 'Password' }}</h2></template>
        <form class="space-y-4" @submit.prevent="savePassword">
          <UFormField v-if="!forced" label="Current password" required><UInput v-model="currentPassword" type="password" autocomplete="current-password" class="w-full" /></UFormField>
          <UFormField label="New password" required><UInput v-model="password" type="password" autocomplete="new-password" class="w-full" /></UFormField>
          <UButton type="submit" :loading="busy">Change password</UButton>
        </form>
      </UCard>

      <UCard v-if="!forced">
        <template #header><div class="flex items-center justify-between gap-3"><h2 class="font-semibold">Active sessions</h2><UButton color="neutral" variant="outline" :loading="busy" @click="logoutOthers">Log out all other sessions</UButton></div></template>
        <UEmpty v-if="sessionRows.length === 0" title="No active sessions found" />
        <ul v-else class="divide-y divide-default">
          <li v-for="session in sessionRows" :key="session.id" class="flex items-center justify-between gap-4 py-3">
            <div class="min-w-0 text-sm"><p class="font-medium">Session {{ session.id.slice(0, 8) }}</p><p class="text-muted">Created {{ new Date(session.createdAt).toLocaleString() }} · Expires {{ new Date(session.expiresAt).toLocaleString() }}</p></div>
            <UButton color="error" variant="soft" size="sm" @click="revoke(session.id)">Revoke</UButton>
          </li>
        </ul>
      </UCard>

      <UButton color="neutral" variant="outline" @click="logout">Log out</UButton>
    </div>
  </PageFrame>
</template>
