<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

const route = useRoute()
const auth = useAuth()
const mode = computed(() => String(route.params.mode ?? 'login'))
const allowed = new Set(['login', 'register', 'setup', 'reset'])
const form = reactive({ login: '', username: '', email: '', displayName: '', password: '', stayLoggedIn: false })
const busy = ref(false)
const errorMessage = ref('')

const title = computed(() => ({
  login: 'Sign in', register: 'Create first administrator', setup: 'Complete account setup', reset: 'Reset password'
}[mode.value] ?? 'Authentication'))

const description = computed(() => ({
  login: 'Sign in with your username or email.', register: 'Registration is available only while deployment bootstrap is open.', setup: 'Choose a password to activate your pending account.', reset: 'Choose a new password using your one-time reset token.'
}[mode.value] ?? ''))

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

async function submit() {
  busy.value = true
  errorMessage.value = ''
  try {
    if (mode.value === 'login') {
      await auth.login(form.login, form.password, form.stayLoggedIn)
      await navigateTo('/account')
    } else if (mode.value === 'register') {
      await auth.register({ username: form.username, email: form.email, displayName: form.displayName, password: form.password })
      form.password = ''
      await navigateTo('/auth/login')
    } else if (mode.value === 'setup' || mode.value === 'reset') {
      const token = typeof route.query.token === 'string' ? route.query.token : ''
      if (!token) throw new Error('A one-time token is required.')
      await auth.completePasswordToken(mode.value, token, form.password)
      form.password = ''
      await navigateTo('/auth/login')
    }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : 'Authentication failed.'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <PageFrame :title="title" :description="description">
    <div class="mx-auto max-w-lg">
      <UCard>
        <form class="space-y-4" @submit.prevent="submit">
          <UAlert v-if="errorMessage" color="error" variant="soft" :description="errorMessage" />
          <template v-if="mode === 'login'">
            <UFormField label="Username or email" required><UInput v-model="form.login" autocomplete="username" class="w-full" /></UFormField>
          </template>
          <template v-else-if="mode === 'register'">
            <UFormField label="Display name" required><UInput v-model="form.displayName" autocomplete="name" class="w-full" /></UFormField>
            <UFormField label="Username" required><UInput v-model="form.username" autocomplete="username" class="w-full" /></UFormField>
            <UFormField label="Email" required><UInput v-model="form.email" type="email" autocomplete="email" class="w-full" /></UFormField>
          </template>
          <UFormField label="Password" required><UInput v-model="form.password" type="password" :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" class="w-full" /></UFormField>
          <UCheckbox v-if="mode === 'login'" v-model="form.stayLoggedIn" label="Stay logged in" description="Persist this session in this browser." />
          <UButton type="submit" block :loading="busy">{{ mode === 'login' ? 'Sign in' : mode === 'register' ? 'Create administrator' : 'Set password' }}</UButton>
        </form>
      </UCard>
    </div>
  </PageFrame>
</template>
