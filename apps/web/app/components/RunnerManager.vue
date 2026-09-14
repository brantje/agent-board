<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import type { Project, ProjectRunnerSettings, Runner } from '../types/api'
import { apiRequest } from '../utils/api'
import { useResource } from '../composables/useResource'
import { runnerEngines, runnerFeatures, runnerSessionSummary } from '../utils/runners'

const props = defineProps<{
  projectId?: string
  canAdmin?: boolean
  allowInternalRunner?: boolean
}>()
const emit = defineEmits<{ projectUpdated: [project: Project] }>()

const projectMode = computed(() => Boolean(props.projectId))
const canManage = computed(() => !projectMode.value || props.canAdmin === true)
const collectionURL = computed(() => props.projectId
  ? `/api/projects/${encodeURIComponent(props.projectId)}/runners`
  : '/api/runners')
const { data, pending, error, refresh } = useResource<Runner[] | ProjectRunnerSettings>(() => collectionURL.value)
const projectSettings = computed<ProjectRunnerSettings | undefined>(() => {
  if (!data.value || Array.isArray(data.value)) return undefined
  if (!Array.isArray(data.value.runnerIds) || !Array.isArray(data.value.projectRunners) || !Array.isArray(data.value.sharedRunners)) return undefined
  return data.value
})
const managedRunners = computed(() => Array.isArray(data.value) ? data.value : projectSettings.value?.projectRunners ?? [])
const sharedRunners = computed(() => projectSettings.value?.sharedRunners ?? [])
const internalRunner = computed(() => projectSettings.value?.internalRunner ?? undefined)
const registrationToken = ref('')
const runnerToken = ref('')
const creating = ref(false)
const createError = ref<Error>()
const editing = ref<Runner>()
const editState = reactive({ name: '', maxActiveSessions: 5 })
const saving = ref(false)
const saveError = ref<Error>()
const deleting = ref('')
const revoking = ref('')
const rotating = ref('')
const lifecycleError = ref<Error>()
const selectedSharedRunnerIds = ref<string[]>([])
const policySaving = ref(false)
const policyError = ref<Error>()
const internalFallback = ref(props.allowInternalRunner ?? true)
const fallbackSaving = ref(false)
const fallbackError = ref<Error>()

watch(projectSettings, (value) => {
  if (value) selectedSharedRunnerIds.value = [...value.runnerIds]
}, { immediate: true })
watch(() => props.allowInternalRunner, value => {
  if (value !== undefined) internalFallback.value = value
})

function runnerURL(runner: Runner, suffix = '') {
  const base = props.projectId
    ? `/api/projects/${encodeURIComponent(props.projectId)}/runners/${encodeURIComponent(runner.id)}`
    : `/api/runners/${encodeURIComponent(runner.id)}`
  return `${base}${suffix}`
}

async function createRunner() {
  if (creating.value || !canManage.value) return
  creating.value = true
  createError.value = undefined
  registrationToken.value = ''
  try {
    const response = await apiRequest<{ runner: { id: string }, registrationToken: string }>(collectionURL.value, { method: 'POST' })
    registrationToken.value = response.registrationToken
    await refresh()
  } catch (failure) {
    createError.value = failure as Error
  } finally {
    creating.value = false
  }
}

async function copyValue(value: string) {
  if (!value || !navigator.clipboard) return
  await navigator.clipboard.writeText(value)
}

function edit(runner: Runner) {
  if (!canManage.value || runner.internal || !runner.registeredAt) return
  editing.value = runner
  editState.name = runner.name ?? ''
  editState.maxActiveSessions = runner.maxActiveSessions
  saveError.value = undefined
}

function resetEdit() {
  editing.value = undefined
  editState.name = ''
  editState.maxActiveSessions = 5
  saveError.value = undefined
}

function closeEdit() {
  if (!saving.value) resetEdit()
}

async function saveRunner() {
  if (!editing.value || saving.value || !editState.name.trim() || editState.maxActiveSessions < 1) return
  saving.value = true
  saveError.value = undefined
  try {
    await apiRequest<Runner>(runnerURL(editing.value), {
      method: 'PATCH',
      body: { name: editState.name.trim(), maxActiveSessions: editState.maxActiveSessions }
    })
    resetEdit()
    await refresh()
  } catch (failure) {
    saveError.value = failure as Error
  } finally {
    saving.value = false
  }
}

async function rotateRunner(runner: Runner) {
  if (!canManage.value || rotating.value) return
  rotating.value = runner.id
  lifecycleError.value = undefined
  runnerToken.value = ''
  try {
    const response = await apiRequest<{ runner: Runner, runnerToken: string }>(runnerURL(runner, '/rotate-token'), { method: 'POST' })
    runnerToken.value = response.runnerToken
    await refresh()
  } catch (failure) {
    lifecycleError.value = failure as Error
  } finally {
    rotating.value = ''
  }
}

async function revokeRunner(runner: Runner) {
  if (!canManage.value || revoking.value) return
  revoking.value = runner.id
  lifecycleError.value = undefined
  try {
    await apiRequest<Runner>(runnerURL(runner, '/revoke'), { method: 'POST' })
    await refresh()
  } catch (failure) {
    lifecycleError.value = failure as Error
  } finally {
    revoking.value = ''
  }
}

async function deleteRunner(runner: Runner) {
  if (!canManage.value || deleting.value) return
  deleting.value = runner.id
  lifecycleError.value = undefined
  try {
    await apiRequest<void>(runnerURL(runner), { method: 'DELETE' })
    await refresh()
  } catch (failure) {
    lifecycleError.value = failure as Error
  } finally {
    deleting.value = ''
  }
}

function toggleSharedRunner(id: string, selected: boolean) {
  const ids = selectedSharedRunnerIds.value.filter(value => value !== id)
  if (selected) ids.push(id)
  selectedSharedRunnerIds.value = ids
}

async function saveSharedPolicy() {
  if (!props.projectId || !canManage.value || policySaving.value) return
  policySaving.value = true
  policyError.value = undefined
  try {
    await apiRequest<ProjectRunnerSettings>(collectionURL.value, {
      method: 'PUT',
      body: { runnerIds: selectedSharedRunnerIds.value }
    })
    await refresh()
  } catch (failure) {
    policyError.value = failure as Error
  } finally {
    policySaving.value = false
  }
}

async function saveInternalFallback(value = internalFallback.value) {
  if (!props.projectId || !canManage.value || fallbackSaving.value) return
  internalFallback.value = value
  fallbackSaving.value = true
  fallbackError.value = undefined
  try {
    const project = await apiRequest<Project>(`/api/projects/${encodeURIComponent(props.projectId)}`, {
      method: 'PATCH',
      body: { allowInternalRunner: internalFallback.value }
    })
    emit('projectUpdated', project)
  } catch (failure) {
    internalFallback.value = props.allowInternalRunner ?? true
    fallbackError.value = failure as Error
  } finally {
    fallbackSaving.value = false
  }
}

function scopeLabel(runner: Runner) {
  if (runner.internal) return 'Internal'
  if (runner.projectId) return 'Project runner'
  return 'Shared'
}
</script>

<template>
  <div data-runner-manager class="space-y-6">
    <section>
      <div class="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 class="text-base font-semibold">{{ projectMode ? 'Project runners' : 'Runners' }}</h2>
          <p class="text-sm text-muted">
            {{ projectMode ? 'Dedicated execution hosts that can run only this Project.' : 'Manage execution hosts and their enrollment.' }}
          </p>
        </div>
        <UButton v-if="canManage" label="Create runner" icon="i-lucide-plus" :loading="creating" @click="createRunner" />
      </div>

      <UAlert v-if="createError" color="error" title="Unable to create runner" :description="createError.message" class="mb-4" />
      <UAlert v-if="lifecycleError" color="error" title="Unable to manage runner" :description="lifecycleError.message" class="mb-4" />
      <UAlert
        v-if="registrationToken"
        color="warning"
        title="One-time registration token"
        description="Copy this token now. It is shown once and enrolls the pending runner. The runner will use its system hostname as its initial name."
        class="mb-4"
      >
        <template #actions>
          <code class="select-all break-all font-mono text-sm" data-testid="runner-registration-token">{{ registrationToken }}</code>
          <UButton label="Copy" color="neutral" variant="outline" @click="copyValue(registrationToken)" />
        </template>
      </UAlert>
      <UAlert
        v-if="runnerToken"
        color="warning"
        title="New runner credential"
        description="Copy this credential now and replace the runner service token before reconnecting. It is shown once."
        class="mb-4"
      >
        <template #actions>
          <code class="select-all break-all font-mono text-sm" data-testid="runner-token">{{ runnerToken }}</code>
          <UButton label="Copy" color="neutral" variant="outline" @click="copyValue(runnerToken)" />
        </template>
      </UAlert>

      <AsyncState
        :pending="pending"
        :error="error"
        :empty="!managedRunners.length"
        empty-title="No runners yet"
        :empty-description="projectMode ? 'Create a dedicated runner, then enroll agent-runner with its one-time registration token.' : 'Create a pending runner, then enroll an external agent-runner with its one-time registration token.'"
        @retry="refresh"
      >
        <div class="grid w-full gap-3">
          <UCard v-for="runner in managedRunners" :key="runner.id">
            <div class="flex flex-wrap items-center gap-3">
              <div class="min-w-0 flex-1">
                <h3 class="font-medium text-highlighted break-words">{{ runner.name ?? 'Pending registration' }}</h3>
                <p class="text-xs text-muted font-mono break-all">{{ runner.id }}</p>
                <p v-if="runner.projectId && !projectMode" class="mt-1 text-xs text-muted font-mono break-all">Owner: {{ runner.projectId }}</p>
              </div>
              <UBadge color="neutral" variant="subtle" :label="scopeLabel(runner)" />
              <UBadge v-if="!runner.registeredAt" color="warning" variant="subtle" :label="runner.revokedAt ? 'Revoked' : 'Pending registration'" />
              <UBadge v-else-if="runner.revokedAt" color="warning" variant="subtle" label="Revoked" />
              <UBadge v-else :color="runner.connected ? 'success' : 'neutral'" variant="subtle" :label="runner.connected ? 'Connected' : 'Offline'" />
              <UButton v-if="canManage && !runner.internal && runner.registeredAt" label="Edit" color="neutral" variant="outline" @click="edit(runner)" />
              <UButton v-if="canManage && !runner.internal && runner.registeredAt && !runner.revokedAt" label="Rotate token" color="neutral" variant="outline" :loading="rotating === runner.id" @click="rotateRunner(runner)" />
              <UButton v-if="canManage && !runner.internal && !runner.revokedAt" label="Revoke" color="neutral" variant="outline" :loading="revoking === runner.id" @click="revokeRunner(runner)" />
              <UButton v-if="canManage && runner.deletable" label="Delete" color="neutral" variant="outline" :loading="deleting === runner.id" :disabled="Boolean(deleting)" @click="deleteRunner(runner)" />
            </div>
            <div v-if="runner.registeredAt" class="mt-3 flex flex-wrap items-center gap-2">
              <UBadge v-for="engine in runnerEngines(runner)" :key="engine" color="primary" variant="subtle" :label="engine" />
              <span v-if="!runnerEngines(runner).length" class="text-xs text-muted">No engines reported yet</span>
            </div>
            <div v-if="runner.registeredAt && runnerFeatures(runner).length" class="mt-2 flex flex-wrap items-center gap-2" data-testid="runner-features">
              <span class="text-xs text-muted">Features:</span>
              <UBadge v-for="feature in runnerFeatures(runner)" :key="feature" color="neutral" variant="subtle" :label="feature" />
            </div>
            <p v-if="runner.registeredAt" class="mt-2 text-sm text-muted" data-testid="runner-session-summary">{{ runnerSessionSummary(runner) }}</p>
            <p class="mt-1 text-xs text-muted">Last seen: {{ runner.lastSeenAt ?? 'Never' }}</p>
          </UCard>
        </div>
      </AsyncState>
    </section>

    <template v-if="projectMode">
      <UCard>
        <template #header>
          <div>
            <h2 class="text-base font-semibold">Deployment runners</h2>
            <p class="text-sm text-muted">Deployment-global capacity available to this Project. The internal runner is controlled by the fallback setting below; shared external runners can be restricted to selected hosts.</p>
          </div>
        </template>
        <UAlert v-if="policyError" color="error" title="Unable to save shared runner policy" :description="policyError.message" class="mb-4" />
        <div v-if="internalRunner" data-testid="internal-runner-capacity" class="mb-5 rounded-md border border-default p-3">
          <div class="flex flex-wrap items-center gap-3">
            <div class="min-w-0 flex-1">
              <h3 class="font-medium text-highlighted">{{ internalRunner.name ?? 'Internal runner' }}</h3>
              <p class="text-xs text-muted font-mono break-all">{{ internalRunner.id }}</p>
            </div>
            <UBadge color="neutral" variant="subtle" label="Internal" />
            <UBadge :color="internalFallback ? 'success' : 'neutral'" variant="subtle" :label="internalFallback ? 'Fallback enabled' : 'Fallback disabled'" />
          </div>
          <div class="mt-3 flex flex-wrap items-center gap-2">
            <UBadge v-for="engine in runnerEngines(internalRunner)" :key="engine" color="primary" variant="subtle" :label="engine" />
            <span v-if="!runnerEngines(internalRunner).length" class="text-xs text-muted">No engines reported yet</span>
          </div>
          <div v-if="runnerFeatures(internalRunner).length" class="mt-2 flex flex-wrap items-center gap-2" data-testid="internal-runner-features">
            <span class="text-xs text-muted">Features:</span>
            <UBadge v-for="feature in runnerFeatures(internalRunner)" :key="feature" color="neutral" variant="subtle" :label="feature" />
          </div>
          <p class="mt-2 text-sm text-muted" data-testid="internal-runner-session-summary">{{ runnerSessionSummary(internalRunner) }}</p>
        </div>
        <p v-else class="mb-5 text-sm text-muted">Internal runner capacity is unavailable.</p>
        <div class="border-t border-default pt-4">
          <h3 class="mb-1 font-medium text-highlighted">Shared external runners</h3>
          <p class="mb-3 text-sm text-muted">No selection allows any eligible shared external runner; selecting runners restricts shared capacity to those hosts.</p>
          <div v-if="sharedRunners.length" class="space-y-3">
            <UCheckbox
              v-for="runner in sharedRunners"
              :key="runner.id"
              :model-value="selectedSharedRunnerIds.includes(runner.id)"
              :label="runner.name ?? 'Pending registration'"
              :description="runner.id"
              :disabled="!canManage || policySaving"
              @update:model-value="value => toggleSharedRunner(runner.id, Boolean(value))"
            />
          </div>
          <p v-else class="text-sm text-muted">No shared external runners are available.</p>
        </div>
        <div v-if="canManage" class="mt-4 flex justify-end">
          <UButton label="Save shared runner policy" :loading="policySaving" @click="saveSharedPolicy" />
        </div>
      </UCard>

      <UCard>
        <template #header>
          <div>
            <h2 class="text-base font-semibold">Internal runner fallback</h2>
            <p class="text-sm text-muted">Permit the server-managed internal runner when no eligible external runner is available.</p>
          </div>
        </template>
        <UAlert v-if="fallbackError" color="error" title="Unable to save internal fallback" :description="fallbackError.message" class="mb-4" />
        <UFormField label="Allow internal runner" name="allowInternalRunner">
          <USwitch
            :model-value="internalFallback"
            :disabled="!canManage || fallbackSaving"
            @update:model-value="value => saveInternalFallback(Boolean(value))"
          />
        </UFormField>
      </UCard>
    </template>

    <UModal :open="!!editing" title="Edit runner" description="Change the display name and scheduler session capacity for this registered runner." :dismissible="!saving" :close="!saving" @update:open="value => { if (!value) closeEdit() }">
      <template #body>
        <UForm :state="editState" class="space-y-4" @submit="saveRunner">
          <UAlert v-if="saveError" color="error" title="Unable to update runner" :description="saveError.message" />
          <UFormField label="Name" name="name" required>
            <UInput v-model="editState.name" class="w-full" :disabled="saving" autofocus />
          </UFormField>
          <UFormField label="Maximum sessions" name="maxActiveSessions" description="Scheduler admission limit for concurrent execution sessions on this runner." required>
            <UInput v-model.number="editState.maxActiveSessions" type="number" min="1" step="1" class="w-full" :disabled="saving" />
          </UFormField>
          <div class="flex justify-end gap-2">
            <UButton label="Cancel" color="neutral" variant="outline" :disabled="saving" @click="closeEdit" />
            <UButton label="Save runner" type="submit" :loading="saving" :disabled="!editState.name.trim() || editState.maxActiveSessions < 1" />
          </div>
        </UForm>
      </template>
    </UModal>
  </div>
</template>
