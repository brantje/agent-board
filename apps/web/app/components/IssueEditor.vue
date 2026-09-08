<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { Issue } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { editableStatuses, statusLabel } from '../utils/issues'

const props = defineProps<{ projectId: string; issue?: Issue; initialStatus?: string }>()
const emit = defineEmits<{ saved: [issue: Issue]; cancel: [] }>()
const allowedStatuses = computed(() => editableStatuses(props.issue?.status))
const statusItems = computed(() => allowedStatuses.value.map(status => ({ label: statusLabel(status), value: status })))
const priorityItems = [0, 1, 2, 3, 4].map(value => ({ label: value === 0 ? 'Priority 0 (default)' : `Priority ${value}`, value }))
const initialStatus = props.issue?.status || props.initialStatus || 'BACKLOG'
const state = reactive({
  title: props.issue?.title || '',
  description: props.issue?.description || '',
  status: initialStatus,
  priority: props.issue?.priority ?? 0
})
const saving = ref(false)
const error = ref<Error>()

function validate() {
  const errors: { name: string; message: string }[] = []
  if (!state.title.trim()) errors.push({ name: 'title', message: 'Title is required.' })
  if (!allowedStatuses.value.includes(state.status)) errors.push({ name: 'status', message: 'Choose an available Board status.' })
  const priority = Number(state.priority)
  if (!Number.isInteger(priority) || priority < 0 || priority > 4) errors.push({ name: 'priority', message: 'Choose a priority from 0 through 4.' })
  return errors
}

async function save() {
  if (saving.value || validate().length) return
  saving.value = true
  error.value = undefined
  try {
    const saved = await apiRequest<Issue>(apiPath('issues', props.projectId, props.issue?.id), {
      method: props.issue ? 'PATCH' : 'POST',
      body: {
        title: state.title.trim(),
        description: state.description,
        status: state.status,
        priority: Number(state.priority)
      }
    })
    emit('saved', saved)
  } catch (failure) {
    error.value = failure as Error
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <UForm :state="state" :validate="validate" class="space-y-4" @submit="save">
    <UAlert v-if="error" color="error" title="Unable to save Issue" :description="error.message" />
    <UFormField label="Title" name="title" required>
      <UInput v-model="state.title" class="w-full" :disabled="saving" autofocus />
    </UFormField>
    <UFormField label="Description" name="description">
      <UTextarea v-model="state.description" :rows="8" class="w-full" :disabled="saving" />
    </UFormField>
    <div class="grid gap-4 sm:grid-cols-2">
      <UFormField label="Board status" name="status" description="Run status is tracked separately from this durable workflow state.">
        <USelect v-model="state.status" :items="statusItems" :disabled="saving" class="w-full" />
      </UFormField>
      <UFormField label="Priority" name="priority" description="Canonical Issue priority is an integer from 0 through 4.">
        <USelect v-model="state.priority" :items="priorityItems" :disabled="saving" class="w-full" />
      </UFormField>
    </div>
    <p v-if="issue?.status === 'DONE'" class="text-sm text-muted">Reopen into Todo to allow another Run. Reopening does not start execution.</p>
    <p v-if="issue?.status === 'REVIEW'" class="text-sm text-muted">Use the Review decision to approve or request changes.</p>
    <div class="flex justify-end gap-2">
      <UButton label="Cancel" color="neutral" variant="outline" :disabled="saving" @click="emit('cancel')" />
      <UButton label="Save issue" type="submit" :loading="saving" />
    </div>
  </UForm>
</template>
