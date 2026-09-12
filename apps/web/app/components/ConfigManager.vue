<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ApiError, apiPath, apiRequest } from '../utils/api'
import { useResource } from '../composables/useResource'
import { useProviderModels } from '../composables/useProviderModels'
import { definitions, draftFor, payloadFor, validateDraft, resourceOptions, canEdit, CUSTOM_PROVIDER_KIND, isBuiltInProviderKind, providerKindSelectValue, type ConfigKind, type ConfigRecord } from '../utils/configuration'
import type { RepositorySettings } from '../types/api'

const props = defineProps<{kind: ConfigKind; projectId?: string; resourceId?: string}>()
const definition = definitions[props.kind]
const path = computed(() => apiPath(props.kind, props.kind === 'projects' ? undefined : props.projectId))
const { data, pending, error, refresh } = useResource<ConfigRecord[]>(path)
const references = Object.fromEntries(definition.fields
  .filter(field => field.resource)
  .map(field => [field.key, useResource<ConfigRecord[]>(() => apiPath(field.resource!, props.projectId))]))
const referencesPending = computed(() => Object.values(references).some(resource => resource.pending.value))
const referenceError = computed(() => Object.values(references).find(resource => resource.error.value)?.error.value)
const open = ref(false)
const selected = ref<ConfigRecord>()
const draft = ref(draftFor(props.kind))
const saving = ref(false)
const saveError = ref<Error>()
const saved = ref(false)
const credential = ref('')
const providerKindChoice = ref('')
const suppressProviderModelReset = ref(false)
const providerIdForModels = computed(() => props.kind === 'model-profiles' && open.value ? String(draft.value.providerId ?? '') : '')
const { models: providerModels, error: providerModelsError, pending: providerModelsPending, refresh: refreshProviderModels } = useProviderModels(providerIdForModels, () => props.projectId)
const modelOptions = computed(() => {
  const options = providerModels.value.map(model => ({
    label: model.name?.trim() ? `${model.name} (${model.id})` : model.id,
    value: model.id
  }))
  const current = String(draft.value.model ?? '').trim()
  if (current && !options.some(option => option.value === current)) {
    options.unshift({ label: current, value: current })
  }
  return options
})
const showModelSelect = computed(() => {
  if (props.kind !== 'model-profiles' || !String(draft.value.providerId ?? '').trim()) return false
  if (providerModelsPending.value || providerModelsError.value) return false
  return modelOptions.value.length > 0
})
const visible = computed(() => props.resourceId ? data.value?.filter(item => item.id === props.resourceId) : data.value)
const pageDescription = computed(() => {
  if (props.kind === 'projects') return props.resourceId ? 'Project settings · backend-managed repository context' : 'Projects · backend-managed repository contexts'
  return props.projectId ? 'Project configuration · shared resources are read-only' : 'Shared configuration'
})
const editingShared = computed(() => !!selected.value && !canEdit(selected.value, props.kind === 'projects' ? undefined : props.projectId))
const controlsDisabled = computed(() => saving.value || editingShared.value)
const saveErrorDescription = computed(() => {
  if (!saveError.value) return undefined
  if (saveError.value instanceof ApiError && !['network', 'request_failed', 'invalid_response'].includes(saveError.value.code)) {
    return `${saveError.value.message} Server code: ${saveError.value.code}.`
  }
  return saveError.value.message
})

function syncProviderKindChoice() {
  if (props.kind !== 'providers') return
  providerKindChoice.value = providerKindSelectValue(String(draft.value.kind ?? ''))
}

function setProviderKindChoice(value: string) {
  providerKindChoice.value = value
  if (value === CUSTOM_PROVIDER_KIND) {
    if (isBuiltInProviderKind(String(draft.value.kind ?? ''))) {
      draft.value.kind = ''
    }
    return
  }
  draft.value.kind = value
}

function edit(item?: ConfigRecord) {
  suppressProviderModelReset.value = true
  selected.value = item
  draft.value = draftFor(props.kind, item)
  syncProviderKindChoice()
  saveError.value = undefined
  saved.value = false
  if (props.kind === 'projects' && !item) {
    void prefillProjectRepositoryPath()
  }
  open.value = true
  queueMicrotask(() => { suppressProviderModelReset.value = false })
}

async function prefillProjectRepositoryPath() {
  try {
    const settings = await apiRequest<RepositorySettings>('/api/repository-settings')
    if (settings.defaultRepositoryPath && !String(draft.value.repositoryPath ?? '').trim()) {
      draft.value.repositoryPath = settings.defaultRepositoryPath
    }
  } catch {
    // Server remains authoritative when deployment settings are unavailable.
  }
}

function clearSecrets() {
  credential.value = ''
}

function readable(value: unknown, fallback = 'Unavailable') {
  if (typeof value !== 'string' || !value) return fallback
  return value.toLowerCase().replaceAll('_', ' ').replace(/^./, character => character.toUpperCase())
}

function formErrors() {
  const errors = validateDraft(props.kind, draft.value)
  if (props.kind === 'providers' && !providerKindChoice.value) {
    errors.push({ name: 'kind', message: 'Select a provider kind.' })
  }
  for (const field of definition.fields) {
    if (!field.resource) continue
    const selectedId = String(draft.value[field.key] ?? '')
    if (!selectedId) continue
    const item = references[field.key]?.data.value?.find(candidate => candidate.id === selectedId)
    if (!item || resourceOptions([item])[0]?.disabled) {
      errors.push({ name: field.key, message: `Select an enabled ${definitions[field.resource].singular.toLowerCase()} available in this scope.` })
    }
  }
  return errors
}

watch(open, value => {
  if (!value) clearSecrets()
})

watch(() => draft.value.providerId, (next, previous) => {
  if (suppressProviderModelReset.value || props.kind !== 'model-profiles' || !open.value) return
  if (previous !== undefined && previous !== next) draft.value.model = ''
})

async function retry() {
  await Promise.all([refresh(), ...Object.values(references).map(resource => resource.refresh())])
}

async function save() {
  if (saving.value) return
  if (formErrors().length) {
    saveError.value = new Error('Check the highlighted form values.')
    return
  }

  saving.value = true
  saveError.value = undefined
  try {
    const body = payloadFor(props.kind, draft.value, { editing: Boolean(selected.value) })
    if (props.kind === 'providers' && credential.value) {
      body.credential = credential.value
    }
    clearSecrets()
    await apiRequest(`${path.value}${selected.value ? `/${selected.value.id}` : ''}`, {
      method: selected.value ? (props.kind === 'projects' ? 'PATCH' : 'PUT') : 'POST',
      body
    })
    draft.value = draftFor(props.kind)
    providerKindChoice.value = ''
    open.value = false
    saved.value = true
    await refresh()
  } catch (failure) {
    clearSecrets()
    saveError.value = failure as Error
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <PageFrame :title="definition.title" :description="pageDescription">
    <template #actions>
      <UButton :label="`New ${definition.singular.toLowerCase()}`" icon="i-lucide-plus" @click="edit()" />
    </template>

    <UAlert v-if="saved" title="Saved" color="success" class="mb-4" />

    <AsyncState :pending="pending" :error="error" :empty="!visible?.length" :empty-title="`No ${definition.title.toLowerCase()} yet`" :empty-description="definition.emptyDescription" @retry="retry">
      <div class="grid w-full gap-3">
        <UCard v-for="item in visible" :key="item.id">
          <div class="flex flex-wrap items-center gap-3">
            <div class="min-w-0 flex-1">
              <h2 class="font-medium text-highlighted break-words">{{ item.name }}</h2>
              <p class="text-xs text-muted">
                <template v-if="kind === 'projects'">Backend-managed repository context</template>
                <template v-else>{{ item.projectId ? 'Project-scoped' : 'Shared' }}</template>
                <template v-if="item.state"> · State: {{ readable(item.state) }}</template>
                <template v-if="item.enabled !== undefined"> · Configuration: {{ item.enabled ? 'Enabled' : 'Disabled' }}</template>
              </p>
            </div>
            <UBadge v-if="item.healthStatus" color="neutral" variant="subtle" :label="`Health: ${readable(item.healthStatus)}`" />
            <UButton v-if="kind === 'projects'" label="Open board" :to="`/projects/${item.id}/board`" variant="outline" />
            <UButton :label="canEdit(item, kind === 'projects' ? undefined : projectId) ? 'Edit' : 'View shared'" color="neutral" variant="outline" @click="edit(item)" />
          </div>

          <p v-if="kind === 'projects'" class="mt-2 text-sm font-mono break-all">
            {{ item.issuePrefix }} · {{ item.repositoryPath }} · {{ item.defaultBranch }}
          </p>
        </UCard>
      </div>
    </AsyncState>

    <UModal v-model:open="open" :title="`${selected ? 'Edit' : 'New'} ${definition.singular}`" description="Changes are saved to Agent Board." :dismissible="!saving" :close="!saving">
      <template #body>
        <AsyncState :pending="referencesPending" :error="referenceError" @retry="retry">
          <UForm :state="draft" :validate="formErrors" class="space-y-4" @submit="save">
            <UAlert v-if="saveError" color="error" title="Unable to save" :description="saveErrorDescription" />
            <UAlert v-if="editingShared" title="Shared resource" description="Manage this resource from global Settings." color="neutral" />

            <div class="form-grid">
              <template v-for="field in definition.fields" :key="field.key">
                <UFormField v-if="field.key === 'kind' && field.allowCustom" :label="field.label" :name="field.key" :description="field.help" :required="field.required">
                  <USelectMenu
                    :model-value="providerKindChoice"
                    :items="field.selectItems"
                    value-key="value"
                    class="w-full"
                    :disabled="controlsDisabled"
                    @update:model-value="setProviderKindChoice($event as string)"
                  />
                </UFormField>
                <UFormField
                  v-if="field.key === 'kind' && providerKindChoice === CUSTOM_PROVIDER_KIND"
                  label="Provider ID"
                  name="providerKindId"
                  description="Unique OpenCode provider ID for a custom OpenAI-compatible endpoint."
                  required
                >
                  <UInput v-model="draft.kind as string" class="w-full font-mono" :disabled="controlsDisabled" />
                </UFormField>
                <UFormField v-else-if="field.key !== 'kind'" :label="field.label" :name="field.key" :description="field.help" :required="field.required">
                  <UCheckbox v-if="field.type === 'checkbox'" :model-value="Boolean(draft[field.key])" :disabled="controlsDisabled" @update:model-value="draft[field.key] = $event === true" />
                  <template v-else-if="field.type === 'provider-model'">
                    <div class="space-y-2">
                      <UAlert
                        v-if="providerModelsError"
                        color="neutral"
                        title="Unable to load models"
                        :description="providerModelsError.message"
                      />
                      <USelectMenu
                        v-if="showModelSelect"
                        v-model="draft[field.key] as string"
                        :items="modelOptions"
                        value-key="value"
                        class="w-full"
                        :disabled="controlsDisabled || providerModelsPending"
                      />
                      <UInput
                        v-else
                        v-model="draft[field.key] as string"
                        class="w-full font-mono"
                        :disabled="controlsDisabled || providerModelsPending"
                      />
                      <div v-if="providerModelsError" class="flex justify-end">
                        <UButton label="Retry model discovery" color="neutral" variant="outline" size="xs" :loading="providerModelsPending" @click="refreshProviderModels()" />
                      </div>
                    </div>
                  </template>
                  <USelectMenu v-else-if="field.type === 'select'" v-model="draft[field.key] as string" :items="field.resource ? resourceOptions(references[field.key]?.data.value || []) : field.options" :value-key="field.resource ? 'value' : undefined" class="w-full" :disabled="controlsDisabled || (field.immutable && !!selected)" />
                  <UTextarea v-else-if="['textarea', 'json'].includes(field.type || '')" v-model="draft[field.key] as string" class="w-full" :disabled="controlsDisabled || (field.immutable && !!selected)" />
                  <UInput v-else v-model="draft[field.key] as string" :type="field.type === 'number' ? 'number' : 'text'" :min="field.min" :max="field.max" :step="field.key === 'temperature' ? 'any' : 1" class="w-full" :disabled="controlsDisabled || (field.immutable && !!selected)" />
                </UFormField>
              </template>
            </div>

            <UAccordion v-if="kind === 'providers'" :items="[{ label: 'Set or replace API key', slot: 'credential' }]">
              <template #credential>
                <div class="py-2">
                  <UFormField label="API key" description="Saved credentials are encrypted and never shown again after save.">
                    <UInput v-model="credential" type="password" autocomplete="new-password" class="w-full" :disabled="controlsDisabled" />
                  </UFormField>
                </div>
              </template>
            </UAccordion>

            <div class="flex justify-end gap-2">
              <UButton label="Cancel" color="neutral" variant="outline" :disabled="saving" @click="open = false" />
              <UButton v-if="!editingShared" label="Save" type="submit" :loading="saving" />
            </div>
          </UForm>
        </AsyncState>
      </template>
    </UModal>
  </PageFrame>
</template>
