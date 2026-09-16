<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Agent, Squad, SquadMember } from '../types/api'
import { apiEmpty, apiPath, apiRequest } from '../utils/api'
import { useResource } from '../composables/useResource'

const props = withDefaults(defineProps<{ projectId: string; canMutate?: boolean }>(), { canMutate: true })
const squads = useResource<Squad[]>(() => apiPath('squads', props.projectId))
const agents = useResource<Agent[]>(() => apiPath('agents', props.projectId))
const open = ref(false)
const selected = ref<Squad>()
const name = ref('')
const leaderAgentId = ref('')
const members = ref<SquadMember[]>([])
const saving = ref(false)
const deleting = ref<string>()
const saveError = ref<Error>()
const deleteError = ref<Error>()
const saved = ref(false)

const usableAgents = computed(() => (agents.data.value || []).filter(agent => agent.state === 'ENABLED'))
const agentOptions = computed(() => usableAgents.value.map(agent => ({ label: agent.name, value: agent.id })))
const memberAgentOptions = computed(() => agentOptions.value.filter(option => option.value !== leaderAgentId.value))
const readOnly = computed(() => !props.canMutate)

function agentName(agentId: string) {
  return agents.data.value?.find(agent => agent.id === agentId)?.name || 'Agent unavailable'
}

function edit(squad?: Squad) {
  selected.value = squad
  name.value = squad?.name || ''
  leaderAgentId.value = squad?.leaderAgentId || ''
  members.value = (squad?.members || []).map(member => ({ ...member }))
  saveError.value = undefined
  deleteError.value = undefined
  saved.value = false
  open.value = true
}

function addMember() {
  if (readOnly.value) return
  members.value.push({ agentId: '', role: null })
}

function removeMember(index: number) {
  if (readOnly.value) return
  members.value.splice(index, 1)
}

function validationError() {
  if (!name.value.trim()) return 'Squad name is required.'
  if (!leaderAgentId.value) return 'Choose one leader Agent.'
  const ids = members.value.map(member => member.agentId).filter(Boolean)
  if (ids.length !== members.value.length) return 'Choose an Agent for every member row.'
  if (ids.includes(leaderAgentId.value)) return 'The leader is already represented by the leader selection.'
  if (new Set(ids).size !== ids.length) return 'Each member Agent may only appear once.'
  return undefined
}

async function retry() {
  await Promise.all([squads.refresh(), agents.refresh()])
}

async function save() {
  if (readOnly.value || saving.value) return
  const invalid = validationError()
  if (invalid) {
    saveError.value = new Error(invalid)
    return
  }
  saving.value = true
  saveError.value = undefined
  try {
    await apiRequest<Squad>(`${apiPath('squads', props.projectId)}${selected.value ? `/${selected.value.id}` : ''}`, {
      method: selected.value ? 'PUT' : 'POST',
      body: {
        name: name.value.trim(),
        leaderAgentId: leaderAgentId.value,
        members: members.value.map(member => ({
          agentId: member.agentId,
          role: member.role?.trim() || null
        }))
      }
    })
    open.value = false
    saved.value = true
    await squads.refresh()
  } catch (failure) {
    saveError.value = failure as Error
  } finally {
    saving.value = false
  }
}

async function remove(squad: Squad) {
  if (readOnly.value || deleting.value) return
  deleting.value = squad.id
  deleteError.value = undefined
  try {
    await apiEmpty(apiPath('squads', props.projectId, squad.id), { method: 'DELETE' })
    await squads.refresh()
  } catch (failure) {
    deleteError.value = failure as Error
  } finally {
    deleting.value = undefined
  }
}
</script>

<template>
  <PageFrame title="Squads" description="Agent Squads · reusable Project-scoped Agent teams, distinct from human Groups.">
    <template #actions>
      <UButton v-if="canMutate" label="New squad" icon="i-lucide-plus" @click="edit()" />
    </template>

    <UAlert v-if="saved" title="Saved" color="success" class="mb-4" />
    <UAlert v-if="deleteError" title="Unable to delete Squad" :description="deleteError.message" color="error" class="mb-4" />

    <AsyncState
      :pending="squads.pending.value || agents.pending.value"
      :error="squads.error.value || agents.error.value"
      :empty="!squads.data.value?.length"
      empty-title="No Squads yet"
      empty-description="Create a reusable Agent Squad with one leader and optional members."
      @retry="retry"
    >
      <div class="grid gap-3">
        <UCard v-for="squad in squads.data.value || []" :key="squad.id" :data-squad-id="squad.id">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0 flex-1 space-y-3">
              <div>
                <h2 class="font-medium text-highlighted break-words">{{ squad.name }}</h2>
                <p class="text-xs text-muted">Agent Squad · {{ squad.members.length }} {{ squad.members.length === 1 ? 'member' : 'members' }}</p>
              </div>
              <dl class="space-y-2 text-sm">
                <div class="flex flex-wrap items-center gap-2">
                  <dt><UBadge label="Leader" color="primary" variant="subtle" /></dt>
                  <dd class="flex items-center gap-2">
                    <IdentityAvatar kind="agent" :name="agentName(squad.leaderAgentId)" size="xs" />
                    <span>{{ agentName(squad.leaderAgentId) }}</span>
                  </dd>
                </div>
                <div v-for="member in squad.members" :key="member.agentId" class="flex flex-wrap items-center gap-2">
                  <dt><UBadge label="Member" color="neutral" variant="subtle" /></dt>
                  <dd class="flex items-center gap-2">
                    <IdentityAvatar kind="agent" :name="agentName(member.agentId)" size="xs" />
                    <span>{{ agentName(member.agentId) }}</span>
                    <span v-if="member.role" class="text-muted">· {{ member.role }}</span>
                  </dd>
                </div>
              </dl>
            </div>
            <div class="flex gap-2">
              <UButton :label="canMutate ? 'Edit' : 'View'" color="neutral" variant="outline" @click="edit(squad)" />
              <UButton
                v-if="canMutate"
                label="Delete"
                color="error"
                variant="outline"
                :loading="deleting === squad.id"
                :disabled="Boolean(deleting)"
                @click="remove(squad)"
              />
            </div>
          </div>
        </UCard>
      </div>
    </AsyncState>

    <UModal v-model:open="open" :title="`${selected ? (readOnly ? 'View' : 'Edit') : 'New'} Squad`" description="Squad membership is durable Project configuration.">
      <template #body>
        <UForm :state="{ name, leaderAgentId, members }" class="space-y-4" @submit="save">
          <UAlert v-if="saveError" title="Unable to save Squad" :description="saveError.message" color="error" />
          <UAlert v-if="readOnly" title="Read-only Squad" description="Project administrators can change Squad configuration." color="neutral" />

          <UFormField label="Name" name="name" required>
            <UInput v-model="name" class="w-full" :disabled="readOnly || saving" />
          </UFormField>

          <UFormField label="Leader Agent" name="leaderAgentId" description="The leader is the authoritative execution Agent for Squad-owned Issues." required>
            <USelectMenu v-model="leaderAgentId" :items="agentOptions" value-key="value" class="w-full" :disabled="readOnly || saving" />
          </UFormField>

          <div class="space-y-3">
            <div class="flex items-center justify-between gap-3">
              <div>
                <h3 class="font-medium">Members</h3>
                <p class="text-sm text-muted">Optional reusable members and descriptive roles. Roles do not grant permissions.</p>
              </div>
              <UButton v-if="!readOnly" label="Add member" icon="i-lucide-plus" color="neutral" variant="outline" @click="addMember" />
            </div>

            <div v-for="(member, index) in members" :key="index" class="grid gap-2 rounded-md border border-default p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
              <UFormField label="Agent" :name="`member-${index}-agent`">
                <USelectMenu v-model="member.agentId" :items="memberAgentOptions" value-key="value" class="w-full" :disabled="readOnly || saving" />
              </UFormField>
              <UFormField label="Role" :name="`member-${index}-role`" description="Descriptive only">
                <UInput v-model="member.role" class="w-full" :disabled="readOnly || saving" />
              </UFormField>
              <div class="flex items-end">
                <UButton v-if="!readOnly" label="Remove" color="neutral" variant="outline" :disabled="saving" @click="removeMember(index)" />
              </div>
            </div>
            <p v-if="!members.length" class="text-sm text-muted">No additional members.</p>
          </div>

          <div v-if="!readOnly" class="flex justify-end">
            <UButton label="Save Squad" type="submit" :loading="saving" />
          </div>
        </UForm>
      </template>
    </UModal>
  </PageFrame>
</template>
