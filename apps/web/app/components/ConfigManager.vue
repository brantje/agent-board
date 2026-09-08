<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { apiPath, apiRequest } from '../utils/api'
import { useResource } from '../composables/useResource'
import { definitions, draftFor, payloadFor, validateDraft, resourceOptions, canEdit, type ConfigKind, type ConfigRecord } from '../utils/configuration'
const props = defineProps<{kind:ConfigKind;projectId?:string;resourceId?:string}>()
const definition = definitions[props.kind]
const path = computed(() => apiPath(props.kind,props.kind === 'projects' || props.kind === 'providers' ? undefined : props.projectId))
const {data,pending,error,refresh} = useResource<ConfigRecord[]>(path)
const references = Object.fromEntries(definition.fields.filter(f=>f.resource).map(f=>[f.key,useResource<ConfigRecord[]>(() => apiPath(f.resource!,f.resource === 'providers' ? undefined : props.projectId))]))
const referencesPending = computed(() => Object.values(references).some(r=>r.pending.value))
const referenceError = computed(() => Object.values(references).find(r=>r.error.value)?.error.value)
const open = ref(false)
const selected = ref<ConfigRecord>()
const draft = ref(draftFor(props.kind))
const saving = ref(false)
const saveError = ref<Error>()
const saved = ref(false)
const credential = ref('')
const capability = ref('')
const visible = computed(() => props.resourceId ? data.value?.filter(item=>item.id === props.resourceId) : data.value)
function edit(item?:ConfigRecord) { selected.value = item; draft.value = draftFor(props.kind,item); saveError.value = undefined; saved.value = false; open.value = true }
function clearSecrets() { credential.value = ''; capability.value = '' }
watch(open, value => { if (!value) clearSecrets() })
async function retry() { await Promise.all([refresh(),...Object.values(references).map(r=>r.refresh())]) }
async function save() {
  if (saving.value) return
  if (validateDraft(props.kind,draft.value).length) { saveError.value = new Error('Check the highlighted form values.'); return }
  if (credential.value && (!draft.value.credentialRef || !capability.value)) { saveError.value = new Error('A credential reference and deployment secret-write capability are required.'); return }
  saving.value = true; saveError.value = undefined
  try {
    if (credential.value) await apiRequest('/api/secrets',{method:'PUT',body:{ref:draft.value.credentialRef,value:credential.value},headers:{'X-Agent-Board-Secret-Write-Token':capability.value}})
    clearSecrets()
    await apiRequest(`${path.value}${selected.value ? `/${selected.value.id}` : ''}`,{method:selected.value ? props.kind === 'projects' ? 'PATCH' : 'PUT' : 'POST',body:payloadFor(props.kind,draft.value)})
    draft.value = draftFor(props.kind); open.value = false; saved.value = true; await refresh()
  } catch (failure) { clearSecrets(); saveError.value = failure as Error } finally { saving.value = false }
}
</script>
<template>
  <PageFrame :title="definition.title" :description="projectId ? 'Project configuration · shared resources are read-only' : 'Shared configuration'">
    <template #actions><template v-if="resourceId"><UButton v-for="section in ['model-profiles','runtimes','executor-profiles']" :key="section" :label="definitions[section as ConfigKind].title" :to="`/projects/${resourceId}/settings/${section}`" variant="outline" /></template><UButton :label="`New ${definition.singular.toLowerCase()}`" icon="i-lucide-plus" @click="edit()" /></template>
    <UAlert v-if="saved" title="Saved" color="success" class="mb-4" />
    <AsyncState :pending="pending" :error="error" :empty="!visible?.length" :empty-title="`No ${definition.title.toLowerCase()} yet`" @retry="retry">
      <div class="grid gap-3">
        <UCard v-for="item in visible" :key="item.id">
          <div class="flex flex-wrap items-center gap-3">
            <div class="min-w-0 flex-1"><h2 class="font-medium text-highlighted break-words">{{ item.name }}</h2><p class="text-xs text-muted">{{ item.projectId ? 'Project' : 'Shared' }}<template v-if="item.state"> · {{ item.state }}</template><template v-if="item.enabled !== undefined"> · {{ item.enabled ? 'Enabled' : 'Disabled' }}</template></p></div>
            <UBadge v-if="item.healthStatus" color="neutral" variant="subtle" :label="`Configured · health ${item.healthStatus.toLowerCase()}`" />
            <UButton v-if="kind === 'projects'" label="Open board" :to="`/projects/${item.id}/board`" variant="outline" />
            <UButton :label="canEdit(item, kind === 'projects' ? undefined : projectId) ? 'Edit' : 'View shared'" color="neutral" variant="outline" @click="edit(item)" />
          </div>
          <p v-if="kind === 'projects'" class="mt-2 text-sm font-mono break-all">{{ item.repositoryPath }} · {{ item.defaultBranch }}</p>
          <p v-if="kind === 'runtimes'" class="mt-2 text-sm font-mono break-all">{{ item.image }} · {{ item.networkPolicy }} network · Issue workspace</p>
        </UCard>
      </div>
    </AsyncState>
    <UModal v-model:open="open" :title="`${selected ? 'Edit' : 'New'} ${definition.singular}`" description="Changes are saved to Agent Board." :dismissible="!saving" :close="!saving">
      <template #body>
        <AsyncState :pending="referencesPending" :error="referenceError" @retry="retry">
          <UForm :state="draft" :validate="() => validateDraft(kind,draft)" class="space-y-4" @submit="save">
            <UAlert v-if="saveError" color="error" title="Unable to save" :description="saveError.message" />
            <UAlert v-if="selected && !canEdit(selected,kind === 'projects' ? undefined : projectId)" title="Shared resource" description="Manage this resource from global Settings." color="neutral" />
            <div class="form-grid">
              <UFormField v-for="field in definition.fields" :key="field.key" :label="field.label" :name="field.key" :description="field.help" :required="field.required">
                <UCheckbox v-if="field.type === 'checkbox'" :model-value="Boolean(draft[field.key])" :disabled="saving || !!(selected && !canEdit(selected,kind === 'projects' ? undefined : projectId))" @update:model-value="draft[field.key] = $event === true" />
                <USelect v-else-if="field.type === 'select'" v-model="draft[field.key] as string" :items="field.resource ? resourceOptions(references[field.key]?.data.value || []) : field.options" class="w-full" :disabled="saving || !!(selected && !canEdit(selected,kind === 'projects' ? undefined : projectId))" />
                <UTextarea v-else-if="['textarea','json','lines'].includes(field.type || '')" v-model="draft[field.key] as string" class="w-full" :disabled="saving || !!(selected && !canEdit(selected,kind === 'projects' ? undefined : projectId))" />
                <UInput v-else v-model="draft[field.key] as string" :type="field.type === 'number' ? 'number' : 'text'" :min="field.min" :max="field.max" :step="field.key === 'temperature' ? 'any' : 1" class="w-full" :disabled="saving || !!(selected && !canEdit(selected,kind === 'projects' ? undefined : projectId))" />
              </UFormField>
            </div>
            <UAccordion v-if="kind === 'providers'" :items="[{label:'Set or replace credential',slot:'credential'}]">
              <template #credential><div class="space-y-3 py-2"><UFormField label="Credential value"><UInput v-model="credential" type="password" autocomplete="new-password" class="w-full" /></UFormField><UFormField label="Deployment secret-write capability" description="Required by the deployment to authorize secret storage. Used only for this save."><UInput v-model="capability" type="password" autocomplete="off" class="w-full" /></UFormField></div></template>
            </UAccordion>
            <div class="flex justify-end gap-2"><UButton label="Cancel" color="neutral" variant="outline" :disabled="saving" @click="open = false" /><UButton v-if="!selected || canEdit(selected,kind === 'projects' ? undefined : projectId)" label="Save" type="submit" :loading="saving" /></div>
          </UForm>
        </AsyncState>
      </template>
    </UModal>
  </PageFrame>
</template>
