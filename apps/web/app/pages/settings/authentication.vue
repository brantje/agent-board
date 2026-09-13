<script setup lang="ts">
import { onMounted, ref } from 'vue'
import type { AuthSettings } from '../../types/auth'

const auth = useAuth()
const form = ref<AuthSettings | null>(null)
const busy = ref(false)
const errorMessage = ref('')
const saved = ref(false)

async function load() {
  busy.value = true
  errorMessage.value = ''
  saved.value = false
  form.value = null
  try {
    form.value = { ...await auth.settings() }
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : 'Unable to load settings.'
  } finally {
    busy.value = false
  }
}

async function save() {
  if (!form.value) return
  busy.value = true
  errorMessage.value = ''
  saved.value = false
  try {
    form.value = { ...await auth.updateSettings({ ...form.value }) }
    saved.value = true
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : 'Unable to save settings.'
  } finally {
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <SettingsShell>
    <PageFrame title="Authentication / Security" description="Configure authoritative local authentication policy for this deployment.">
      <UCard>
        <UAlert v-if="errorMessage" color="error" variant="soft" :description="errorMessage" class="mb-4" />
        <UAlert v-if="saved" color="success" variant="soft" description="Authentication settings saved." class="mb-4" />

        <form v-if="form" class="space-y-5" @submit.prevent="save">
          <div class="grid gap-4 md:grid-cols-2">
            <UFormField label="Access token lifetime (seconds)" description="300–86400"><UInput v-model.number="form.accessTokenLifetimeSeconds" type="number" min="300" max="86400" class="w-full" /></UFormField>
            <UFormField label="Refresh token lifetime (seconds)" description="3600–31536000"><UInput v-model.number="form.refreshTokenLifetimeSeconds" type="number" min="3600" max="31536000" class="w-full" /></UFormField>
            <UFormField label="Minimum password length" description="8–256"><UInput v-model.number="form.minimumPasswordLength" type="number" min="8" max="256" class="w-full" /></UFormField>
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
            <UCheckbox v-model="form.requireUppercase" label="Require uppercase letter" />
            <UCheckbox v-model="form.requireLowercase" label="Require lowercase letter" />
            <UCheckbox v-model="form.requireNumber" label="Require number" />
            <UCheckbox v-model="form.requireSymbol" label="Require symbol" />
          </div>
          <UButton type="submit" :loading="busy">Save authentication settings</UButton>
        </form>

        <div v-else class="space-y-3">
          <p v-if="!errorMessage" class="text-sm text-muted">Loading authentication settings…</p>
          <UButton v-else color="neutral" variant="outline" :loading="busy" @click="load">Retry</UButton>
        </div>
      </UCard>
    </PageFrame>
  </SettingsShell>
</template>
