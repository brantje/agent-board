<script setup lang="ts">
import { reactive, ref } from 'vue'
import type { Runner } from '../types/api'
import { apiRequest } from '../utils/api'
import { useResource } from '../composables/useResource'
import { runnerEngines, runnerSessionSummary } from '../utils/runners'

const { data: runners, pending, error, refresh } = useResource<Runner[]>('/api/runners')
const registrationToken = ref('')
const creating = ref(false)
const createError = ref<Error>()
const editing = ref<Runner>()
const editState = reactive({ name: '' })
const saving = ref(false)
const saveError = ref<Error>()

async function createRunner() {
  if (creating.value) return
  creating.value = true
  createError.value = undefined
  registrationToken.value = ''
  try {
    const response = await apiRequest<{ runner: { id: string }, registrationToken: string }>('/api/runners', { method: 'POST' })
    registrationToken.value = response.registrationToken
    await refresh()
  } catch (failure) {
    createError.value = failure as Error
  } finally {
    creating.value = false
  }
}

async function copyRegistrationToken() {
  if (!registrationToken.value || !navigator.clipboard) return
  await navigator.clipboard.writeText(registrationToken.value)
}

function edit(runner: Runner) {
  if (runner.internal || !runner.registeredAt) return
  editing.value = runner
  editState.name = runner.name ?? ''
  saveError.value = undefined
}

function resetEdit() {
  editing.value = undefined
  editState.name = ''
  saveError.value = undefined
}

function closeEdit() {
  if (!saving.value) resetEdit()
}

async function saveName() {
  if (!editing.value || saving.value || !editState.name.trim()) return
  saving.value = true
  saveError.value = undefined
  try {
    await apiRequest<Runner>(`/api/runners/${editing.value.id}`, {
      method: 'PATCH',
      body: { name: editState.name.trim() }
    })
    resetEdit()
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
      <UButton label="Create runner" icon="i-lucide-plus" :loading="creating" @click="createRunner" />
    </template>

    <UAlert v-if="createError" color="error" title="Unable to create runner" :description="createError.message" class="mb-4" />
    <UAlert
      v-if="registrationToken"
      color="warning"
      title="One-time registration token"
      description="Copy this token now. It is shown once and enrolls the pending runner. The runner will use its system hostname as its initial name."
      class="mb-4"
    >
      <template #actions>
        <code class="select-all break-all font-mono text-sm" data-testid="runner-registration-token">{{ registrationToken }}</code>
        <UButton label="Copy" color="neutral" variant="outline" @click="copyRegistrationToken" />
      </template>
    </UAlert>

    <AsyncState
      :pending="pending"
      :error="error"
      :empty="!runners?.length"
      empty-title="No runners yet"
      empty-description="Create a pending runner, then enroll an external agent-runner with its one-time registration token."
      @retry="refresh"
    >
      <div class="grid w-full gap-3">
        <UCard v-for="runner in runners" :key="runner.id">
          <div class="flex flex-wrap items-center gap-3">
            <div class="min-w-0 flex-1">
              <h2 class="font-medium text-highlighted break-words">{{ runner.name ?? 'Pending registration' }}</h2>
              <p class="text-xs text-muted font-mono break-all">{{ runner.id }}</p>
            </div>
            <UBadge v-if="runner.internal" color="neutral" variant="subtle" label="Internal" />
            <UBadge v-if="!runner.registeredAt" color="warning" variant="subtle" :label="runner.revokedAt ? 'Revoked' : 'Pending registration'" />
            <UBadge v-else :color="runner.connected ? 'success' : 'neutral'" variant="subtle" :label="runner.connected ? 'Connected' : 'Offline'" />
            <UButton v-if="!runner.internal && runner.registeredAt" label="Edit" color="neutral" variant="outline" @click="edit(runner)" />
          </div>
          <div v-if="runner.registeredAt" class="mt-3 flex flex-wrap items-center gap-2">
            <UBadge
              v-for="engine in runnerEngines(runner)"
              :key="engine"
              color="primary"
              variant="subtle"
              :label="engine"
            />
            <span v-if="!runnerEngines(runner).length" class="text-xs text-muted">No engines reported yet</span>
          </div>
          <p v-if="runner.registeredAt" class="mt-2 text-sm text-muted" data-testid="runner-session-summary">
            {{ runnerSessionSummary(runner) }}
          </p>
        </UCard>
      </div>
    </AsyncState>

    <UModal :open="!!editing" title="Edit runner" description="Change the display name for this registered runner." :dismissible="!saving" :close="!saving" @update:open="value => { if (!value) closeEdit() }">
      <template #body>
        <UForm :state="editState" class="space-y-4" @submit="saveName">
          <UAlert v-if="saveError" color="error" title="Unable to rename runner" :description="saveError.message" />
          <UFormField label="Name" name="name" required>
            <UInput v-model="editState.name" class="w-full" :disabled="saving" autofocus />
          </UFormField>
          <div class="flex justify-end gap-2">
            <UButton label="Cancel" color="neutral" variant="outline" :disabled="saving" @click="closeEdit" />
            <UButton label="Save runner" type="submit" :loading="saving" :disabled="!editState.name.trim()" />
          </div>
        </UForm>
      </template>
    </UModal>
  </PageFrame>
</template>
