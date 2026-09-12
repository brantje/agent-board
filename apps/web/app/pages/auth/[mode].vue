<script setup lang="ts">
import type { AuthFormField, FormSubmitEvent } from '@nuxt/ui'
import { computed, onMounted, reactive, ref } from 'vue'

const route = useRoute()
const auth = useAuth()
const mode = computed(() => String(route.params.mode ?? 'login'))
const allowed = new Set(['login', 'register', 'setup', 'reset'])
const passwordForm = reactive({ password: '' })
const busy = ref(false)
const errorMessage = ref('')

const title = computed(() => ({
  login: 'Sign in', register: 'Create first administrator', setup: 'Complete account setup', reset: 'Reset password'
}[mode.value] ?? 'Authentication'))

const description = computed(() => ({
  login: 'Sign in with your username or email.', register: 'Registration is available only while deployment bootstrap is open.', setup: 'Choose a password to activate your pending account.', reset: 'Choose a new password using your one-time reset token.'
}[mode.value] ?? ''))

const authFields = computed<AuthFormField[]>(() => mode.value === 'register' ? [
  { name: 'displayName', type: 'text', label: 'Display name', placeholder: 'Administrator', autocomplete: 'name', required: true },
  { name: 'username', type: 'text', label: 'Username', placeholder: 'admin', autocomplete: 'username', required: true },
  { name: 'email', type: 'email', label: 'Email', placeholder: 'admin@example.com', autocomplete: 'email', required: true },
  { name: 'password', type: 'password', label: 'Password', placeholder: 'Choose a password', autocomplete: 'new-password', required: true }
] : [
  { name: 'login', type: 'text', label: 'Username or email', placeholder: 'admin@example.com', autocomplete: 'username', required: true },
  { name: 'password', type: 'password', label: 'Password', placeholder: 'Enter your password', autocomplete: 'current-password', required: true },
  { name: 'stayLoggedIn', type: 'checkbox', label: 'Stay logged in', description: 'Persist this session in this browser.' }
])

const authSubmit = computed(() => ({
  label: mode.value === 'register' ? 'Create administrator' : 'Sign in',
  block: true
}))

onMounted(async () => {
  if (!allowed.has(mode.value)) return navigateTo('/auth/login')
  if (mode.value === 'login') {
    try {
      if (await auth.bootstrapAvailable()) return navigateTo('/auth/register')
    } catch {
      // Login remains available when bootstrap status cannot be loaded.
    }
  }
})

async function runAuthentication(action: () => Promise<void>) {
  busy.value = true
  errorMessage.value = ''
  try {
    await action()
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : 'Authentication failed.'
  } finally {
    busy.value = false
  }
}

async function submitAuthForm(event: FormSubmitEvent<Record<string, unknown>>) {
  await runAuthentication(async () => {
    const data = event.data
    if (mode.value === 'login') {
      await auth.login(String(data.login ?? ''), String(data.password ?? ''), data.stayLoggedIn === true)
      await navigateTo('/account')
      return
    }
    if (mode.value === 'register') {
      await auth.register({
        username: String(data.username ?? ''),
        email: String(data.email ?? ''),
        displayName: String(data.displayName ?? ''),
        password: String(data.password ?? '')
      })
      await navigateTo('/auth/login')
    }
  })
}

async function submitPassword() {
  await runAuthentication(async () => {
    const token = typeof route.query.token === 'string' ? route.query.token : ''
    if (!token) throw new Error('A one-time token is required.')
    if (mode.value !== 'setup' && mode.value !== 'reset') throw new Error('Invalid password flow.')
    await auth.completePasswordToken(mode.value, token, passwordForm.password)
    passwordForm.password = ''
    await navigateTo('/auth/login')
  })
}
</script>

<template>
  <PageFrame :title="mode === 'setup' || mode === 'reset' ? title : ''" :description="mode === 'setup' || mode === 'reset' ? description : ''">
    <div class="mx-auto max-w-lg">
      <UCard>
        <UAuthForm
          v-if="mode === 'login' || mode === 'register'"
          :title="title"
          :description="description"
          icon="i-lucide-lock-keyhole"
          :fields="authFields"
          :submit="authSubmit"
          :loading="busy"
          @submit="submitAuthForm"
        >
          <template #validation>
            <UAlert v-if="errorMessage" color="error" variant="soft" :description="errorMessage" />
          </template>
        </UAuthForm>

        <form v-else class="space-y-4" @submit.prevent="submitPassword">
          <UAlert v-if="errorMessage" color="error" variant="soft" :description="errorMessage" />
          <UFormField label="Password" required>
            <UInput v-model="passwordForm.password" type="password" autocomplete="new-password" class="w-full" />
          </UFormField>
          <UButton type="submit" block :loading="busy">Set password</UButton>
        </form>
      </UCard>
    </div>
  </PageFrame>
</template>
