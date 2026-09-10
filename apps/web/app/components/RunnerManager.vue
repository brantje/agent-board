<script setup lang="ts">
import { ref } from 'vue'
import type { Runner } from '../types/api'
import { apiRequest } from '../utils/api'
import { useResource } from '../composables/useResource'

const { data: runners, pending, error, refresh } = useResource<Runner[]>('/api/runners')
const registrationToken = ref('')
const creating = ref(false)
const createError = ref<Error>()
const editing = ref<Runner>()
const name = ref('')
const saving = ref(false)
const saveError = ref<Error>()

async function createRegistration() {
  if (creating.value) return
  creating.value = true
  createError.value = undefined
  registrationToken.value = ''
  try {
    const response = await apiRequest<{ token: string }>('/api/runners', { method: 'POST' })
    registrationToken.value = response.token
  } catch (failure) {
    createError.value = failure as Error
  } finally {
    creating.value = false
  }
}

function edit(runner: Runner) {
  if (runner.internal) return
  editing.value = runner
  name.value = runner.name
  saveError.value = undefined
}

function closeEdit() {
  if (saving.value) return
  editing.value = undefined
  name.value = ''
  saveError.value = undefined
}

async function saveName() {
  if (!editing.value || saving.value || !name.value.trim()) return
  saving.value = true
  saveError.value = undefined
  try {
    await apiRequest<Runner>(`/api/runners/${editing.value.id}`, {
      method: 'PATCH',
      body: { name: name.value.trim() }
    })
    closeEdit()
    await refresh()
  } catch (failure) {
    saveError.value = failure as Error
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <PageFrame title="Runners" description="Manage external execution hosts and their enrollment.">
    <template #actions>
      <UButton label="Create runner" icon="i-lucide-plus" :loading="creating" @click="createRegistration" />
    </template>

    <UAlert v-if="createError" color="error" title="Unable to create runner registration" :description="createError.message" class="mb-4" />
    <UAlert
      v-if="registrationToken"
      color="warning"
      title="One-time registration token"
      description="Copy this token now. It is shown once and can register exactly one runner. The runner will use its system hostname as its initial name."
      class="mb-4"
    >
      <template #actions>
        <code class="select-all break-all font-mono text-sm" data-testid="runner-registration-token">{{ registrationToken }}</code>
      </template>
    </UAlert>

    <AsyncState
      :pending="pending"
      :error="error"
      :empty="!runners?.length"
      empty-title="No runners yet"
      empty-description="Create a one-time registration token, then enroll an external agent-runner."
      @retry="refresh"
    >
      <div class="grid w-full gap-3">
        <UCard v-for="runner in runners" :key="runner.id">
          <div class="flex flex-wrap items-center gap-3">
            <div class="min-w-0 flex-1">
              <h2 class="font-medium text-highlighted break-words">{{ runner.name }}</h2>
              <p class="text-xs text-muted font-mono break-all">{{ runner.id }}</p>
            </div>
            <UBadge v-if="runner.internal" color="neutral" variant="subtle" label="Internal" />
            <UBadge :color="runner.connected ? 'success' : 'neutral'" variant="subtle" :label="runner.connected ? 'Connected' : 'Offline'" />
            <UButton v-if="!runner.internal" label="Edit" color="neutral" variant="outline" @click="edit(runner)" />
          </div>
        </UCard>
      </div>
    </AsyncState>

    <UModal :open="!!editing" title="Edit runner" description="Change the display name for this registered runner." :dismissible="!saving" :close="!saving" @update:open="value => { if (!value) closeEdit() }">
      <template #body>
        <UForm :state="{ name }" class="space-y-4" @submit="saveName">
          <UAlert v-if="saveError" color="error" title="Unable to rename runner" :description="saveError.message" />
          <UFormField label="Name" name="name" required>
            <UInput v-model="name" class="w-full" :disabled="saving" autofocus />
          </UFormField>
          <div class="flex justify-end gap-2">
            <UButton label="Cancel" color="neutral" variant="outline" :disabled="saving" @click="closeEdit" />
            <UButton label="Save runner" type="submit" :loading="saving" :disabled="!name.trim()" />
          </div>
        </UForm>
      </template>
    </UModal>
  </PageFrame>
</template>
