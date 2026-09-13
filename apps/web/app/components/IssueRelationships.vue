<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { Issue, IssueRelationship } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { useResource } from '../composables/useResource'

const props = withDefaults(defineProps<{ projectId: string; issueId: string; canMutate?: boolean }>(), { canMutate: true })
const issues = useResource<Issue[]>(() => apiPath('issues', props.projectId))
const relationships = useResource<IssueRelationship[]>(() => `${apiPath('issues', props.projectId, props.issueId)}/relationships`)
const state = reactive({ targetIssueId: '', type: 'blocks' })
const saving = ref(false)
const deleting = ref('')
const mutationError = ref<Error>()

const typeItems = [
  { label: 'blocks', value: 'blocks' },
  { label: 'depends on', value: 'depends_on' },
  { label: 'related to', value: 'related_to' },
  { label: 'duplicates', value: 'duplicates' }
]
const targetItems = computed(() => (issues.data.value || [])
  .filter(issue => issue.id !== props.issueId)
  .map(issue => ({ label: issue.title, value: issue.id })))

function targetName(id: string) {
  return issues.data.value?.find(issue => issue.id === id)?.title || id
}

async function refreshAll() {
  await Promise.all([issues.refresh(), relationships.refresh()])
}

async function createRelationship() {
  if (!props.canMutate || !state.targetIssueId || saving.value) return
  saving.value = true
  mutationError.value = undefined
  try {
    await apiRequest<IssueRelationship>(`${apiPath('issues', props.projectId, props.issueId)}/relationships`, {
      method: 'POST',
      body: { targetIssueId: state.targetIssueId, type: state.type }
    })
    state.targetIssueId = ''
    await relationships.refresh()
  } catch (failure) {
    mutationError.value = failure as Error
  } finally {
    saving.value = false
  }
}

async function removeRelationship(id: string) {
  if (!props.canMutate || deleting.value) return
  deleting.value = id
  mutationError.value = undefined
  try {
    await apiRequest<void>(`${apiPath('issues', props.projectId, props.issueId)}/relationships/${id}`, { method: 'DELETE' })
    await relationships.refresh()
  } catch (failure) {
    mutationError.value = failure as Error
  } finally {
    deleting.value = ''
  }
}
</script>

<template>
  <UCard>
    <h2 class="section-label mb-3">Relationships</h2>
    <p class="mb-4 text-sm text-muted">Relationships authored from this Issue. Direction and type are stored by the server.</p>
    <UAlert v-if="mutationError" title="Unable to update relationships" :description="mutationError.message" color="error" class="mb-4" />

    <AsyncState :pending="issues.pending.value || relationships.pending.value" :error="issues.error.value || relationships.error.value" @retry="refreshAll">
      <div class="space-y-4">
        <div v-if="relationships.data.value?.length" class="divide-y divide-default border border-default">
          <div v-for="relationship in relationships.data.value" :key="relationship.id" class="flex items-center justify-between gap-3 p-3 text-sm">
            <div class="min-w-0">
              <span class="font-medium">{{ typeItems.find(item => item.value === relationship.type)?.label || relationship.type }}</span>
              <span class="mx-2 text-muted">→</span>
              <NuxtLink :to="`/projects/${projectId}/issues/${relationship.targetIssueId}`" class="break-words hover:text-primary focus-visible:outline-2 focus-visible:outline-primary">
                {{ targetName(relationship.targetIssueId) }}
              </NuxtLink>
            </div>
            <UButton v-if="canMutate" label="Remove" color="neutral" variant="outline" size="xs" :loading="deleting === relationship.id" :disabled="Boolean(deleting)" @click="removeRelationship(relationship.id)" />
          </div>
        </div>
        <p v-else class="text-sm text-muted">No relationships authored from this Issue.</p>

        <template v-if="canMutate">
          <UForm :state="state" class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end" @submit="createRelationship">
            <UFormField label="Relationship" name="relationshipType">
              <USelect v-model="state.type" :items="typeItems" :disabled="saving" class="w-full" />
            </UFormField>
            <UFormField label="Target Issue" name="relationshipTarget">
              <USelect v-model="state.targetIssueId" :items="targetItems" :disabled="saving || !targetItems.length" class="w-full" />
            </UFormField>
            <UButton label="Add relationship" type="submit" :loading="saving" :disabled="!state.targetIssueId" />
          </UForm>
          <p v-if="!targetItems.length" class="text-xs text-muted">Create another Issue in this Project before adding a relationship.</p>
        </template>
      </div>
    </AsyncState>
  </UCard>
</template>
