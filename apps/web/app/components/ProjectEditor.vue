<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import type { Project, RepositorySettings } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'

const props = defineProps<{ project?: Project }>()
const emit = defineEmits<{ saved: [project: Project]; cancel: [] }>()

const sourceOptions = [
  { label: 'Local', value: 'local' },
  { label: 'Git repository', value: 'git' }
]

const state = reactive({
  name: props.project?.name ?? '',
  issuePrefix: props.project?.issuePrefix ?? '',
  sourceType: props.project?.sourceType ?? 'local',
  cloneUrl: props.project?.cloneUrl ?? '',
  sourceRef: props.project?.sourceRef ?? '',
  repositoryPath: props.project?.repositoryPath ?? '',
  defaultBranch: props.project?.defaultBranch || 'main',
  workflowSettings: props.project ? JSON.stringify(props.project.workflowSettings ?? {}, null, 2) : '{}',
  allowInternalRunner: props.project?.allowInternalRunner ?? true
})
const saving = ref(false)
const error = ref<Error>()

function validate() {
  const errors: { name: string; message: string }[] = []
  if (!state.name.trim()) errors.push({ name: 'name', message: 'Name is required.' })
  if (!props.project) {
    const prefix = state.issuePrefix.trim()
    if (!prefix) errors.push({ name: 'issuePrefix', message: 'Issue prefix is required.' })
    else if (!/^[A-Za-z][A-Za-z0-9]{1,9}$/.test(prefix)) {
      errors.push({ name: 'issuePrefix', message: 'Use 2-10 uppercase letters and digits; must start with a letter.' })
    }
  }
  if (state.sourceType === 'git') {
    if (!state.cloneUrl.trim()) errors.push({ name: 'cloneUrl', message: 'Clone URL is required.' })
  } else {
    if (!state.repositoryPath.trim()) errors.push({ name: 'repositoryPath', message: 'Local repository path is required.' })
    if (!state.defaultBranch.trim()) errors.push({ name: 'defaultBranch', message: 'Default branch is required.' })
  }
  try {
    const parsed = JSON.parse(state.workflowSettings)
    if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
      errors.push({ name: 'workflowSettings', message: 'Workflow settings must be a JSON object.' })
    }
  } catch {
    errors.push({ name: 'workflowSettings', message: 'Workflow settings must be valid JSON.' })
  }
  return errors
}

function payload() {
  const workflowSettings = JSON.parse(state.workflowSettings) as Record<string, unknown>
  const body: Record<string, unknown> = {
    name: state.name.trim(),
    sourceType: state.sourceType,
    workflowSettings
  }
  if (state.sourceType === 'git') {
    body.cloneUrl = state.cloneUrl.trim()
    body.sourceRef = state.sourceRef.trim()
  } else {
    body.repositoryPath = state.repositoryPath.trim()
    body.defaultBranch = state.defaultBranch.trim()
  }
  if (props.project) body.allowInternalRunner = state.allowInternalRunner
  if (!props.project) body.issuePrefix = state.issuePrefix.trim().toUpperCase()
  return body
}

async function save() {
  if (saving.value || validate().length) return
  saving.value = true
  error.value = undefined
  try {
    const saved = await apiRequest<Project>(apiPath('projects', undefined, props.project?.id), {
      method: props.project ? 'PATCH' : 'POST',
      body: payload()
    })
    emit('saved', saved)
  } catch (failure) {
    error.value = failure as Error
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  if (props.project || state.sourceType !== 'local' || state.repositoryPath.trim()) return
  try {
    const settings = await apiRequest<RepositorySettings>('/api/repository-settings')
    if (settings.defaultRepositoryPath) state.repositoryPath = settings.defaultRepositoryPath
  } catch {
    // Server remains authoritative when deployment settings are unavailable.
  }
})
</script>

<template>
  <UForm :state="state" :validate="validate" class="space-y-4" @submit="save">
    <UAlert v-if="error" color="error" title="Unable to save Project" :description="error.message" />
    <UFormField label="Name" name="name" required>
      <UInput v-model="state.name" class="w-full" :disabled="saving" autofocus />
    </UFormField>
    <UFormField
      label="Issue prefix"
      name="issuePrefix"
      description="Immutable public prefix for Issue keys such as AB-12. Use 2-10 uppercase letters and digits; must start with a letter."
      :required="!project"
    >
      <UInput v-model="state.issuePrefix" class="w-full font-mono" :disabled="saving || !!project" />
    </UFormField>
    <UFormField label="Source" name="sourceType" required>
      <URadioGroup v-model="state.sourceType" :items="sourceOptions" :disabled="saving" />
    </UFormField>
    <template v-if="state.sourceType === 'git'">
      <UFormField
        label="Clone URL"
        name="cloneUrl"
        description="Credential-free Git clone URL. The remote is not contacted when this Project is saved."
        required
      >
        <UInput v-model="state.cloneUrl" class="w-full font-mono" :disabled="saving" />
      </UFormField>
      <UFormField
        label="Ref (optional)"
        name="sourceRef"
        description="Branch, tag, or commit. Leave empty to use the remote default branch at execution time."
      >
        <UInput v-model="state.sourceRef" class="w-full font-mono" :disabled="saving" />
      </UFormField>
    </template>
    <template v-else>
      <UFormField
        label="Local repository path"
        name="repositoryPath"
        description="Backend-visible path within deployment-authorized repository roots. If the directory does not exist, Agent Board creates it and initializes a new Git repository on save."
        required
      >
        <UInput v-model="state.repositoryPath" class="w-full font-mono" :disabled="saving" />
      </UFormField>
      <UFormField label="Default branch" name="defaultBranch" required>
        <UInput v-model="state.defaultBranch" class="w-full font-mono" :disabled="saving" />
      </UFormField>
    </template>
    <UFormField
      label="Workflow settings"
      name="workflowSettings"
      description="Optional workflow policy overrides supported by your Go server."
    >
      <UTextarea v-model="state.workflowSettings" :rows="6" class="w-full font-mono" :disabled="saving" />
    </UFormField>
    <UFormField
      v-if="project"
      label="Allow internal runner"
      name="allowInternalRunner"
      description="Permit the server-managed internal runner as scheduler fallback when no eligible external runner is available."
    >
      <USwitch v-model="state.allowInternalRunner" :disabled="saving" />
    </UFormField>
    <div class="flex justify-end gap-2">
      <UButton label="Cancel" color="neutral" variant="outline" :disabled="saving" @click="emit('cancel')" />
      <UButton :label="project ? 'Save project' : 'Create project'" type="submit" :loading="saving" />
    </div>
  </UForm>
</template>
