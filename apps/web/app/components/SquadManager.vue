<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Agent, Assignee, Squad, SquadMember } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { useResource } from '../composables/useResource'

type SquadMemberDraft = {
  identity: string
  role: string
}

type MemberType = SquadMember['type']

const props = withDefaults(defineProps<{ projectId: string; canMutate?: boolean }>(), { canMutate: true })
const squads = useResource<Squad[]>(() => apiPath('squads', props.projectId))
const agents = useResource<Agent[]>(() => apiPath('agents', props.projectId))
const assignees = useResource<Assignee[]>(() => apiPath('assignees', props.projectId))
const open = ref(false)
const selected = ref<Squad>()
const name = ref('')
const leaderAgentId = ref('')
const members = ref<SquadMemberDraft[]>([])
const saving = ref(false)
const deleting = ref<string>()
const saveError = ref<Error>()
const deleteError = ref<Error>()
const saved = ref(false)

const usableAgents = computed(() => (agents.data.value || []).filter(agent => agent.state === 'ENABLED'))
const agentOptions = computed(() => usableAgents.value.map(agent => ({ label: agent.name, value: agent.id })))
const memberIdentityOptions = computed(() => (assignees.data.value || [])
  .filter((assignee): assignee is Assignee & { type: MemberType } => assignee.type === 'AGENT' || assignee.type === 'USER')
  .map(assignee => ({
    label: `${memberTypeLabel(assignee.type)} · ${assignee.name}`,
    value: memberIdentity(assignee.type, assignee.id)
  })))
const readOnly = computed(() => !props.canMutate)

function memberIdentity(type: MemberType, id: string) {
  return `${type}:${id}`
}

function parseMemberIdentity(value: string): { type: MemberType; id: string } | undefined {
  const separator = value.indexOf(':')
  if (separator < 1) return undefined
  const type = value.slice(0, separator)
  const id = value.slice(separator + 1)
  if ((type !== 'AGENT' && type !== 'USER') || !id) return undefined
  return { type, id }
}

function memberTypeLabel(type: MemberType) {
  return type === 'AGENT' ? 'Agent' : 'User'
}

function memberOptionsFor(index: number) {
  const current = members.value[index]?.identity
  const unavailable = current && !memberIdentityOptions.value.some(option => option.value === current)
    ? [{ label: `${parseMemberIdentity(current)?.type === 'USER' ? 'User' : 'Agent'} · unavailable`, value: current }]
    : []
  const selectedByOtherRows = new Set(members.value
    .filter((_, memberIndex) => memberIndex !== index)
    .map(member => member.identity)
    .filter(Boolean))
  const leader = leaderAgentId.value ? memberIdentity('AGENT', leaderAgentId.value) : ''
  return [
    ...unavailable,
    ...memberIdentityOptions.value.filter(option => option.value === current || (option.value !== leader && !selectedByOtherRows.has(option.value)))
  ]
}

function agentName(agentId: string) {
  return agents.data.value?.find(agent => agent.id === agentId)?.name || 'Agent unavailable'
}

function memberName(member: SquadMember) {
  if (member.type === 'AGENT') return agentName(member.id)
  return assignees.data.value?.find(assignee => assignee.type === 'USER' && assignee.id === member.id)?.name || 'User unavailable'
}

function edit(squad?: Squad) {
  selected.value = squad
  name.value = squad?.name || ''
  leaderAgentId.value = squad?.leaderAgentId || ''
  members.value = (squad?.members || []).map(member => ({ identity: memberIdentity(member.type, member.id), role: member.role || '' }))
  saveError.value = undefined
  deleteError.value = undefined
  saved.value = false
  open.value = true
}

function addMember() {
  if (readOnly.value) return
  members.value.push({ identity: '', role: '' })
}

function removeMember(index: number) {
  if (readOnly.value) return
  members.value.splice(index, 1)
}

function validationError() {
  if (!name.value.trim()) return 'Squad name is required.'
  if (!leaderAgentId.value) return 'Choose one leader Agent.'
  const identities = members.value.map(member => member.identity).filter(Boolean)
  if (identities.length !== members.value.length) return 'Choose an Agent or User for every member row.'
  if (identities.some(identity => !parseMemberIdentity(identity))) return 'Choose a valid Agent or User for every member row.'
  if (identities.includes(memberIdentity('AGENT', leaderAgentId.value))) return 'The leader is already represented by the leader selection.'
  if (new Set(identities).size !== identities.length) return 'Each typed member identity may only appear once.'
  return undefined
}

async function retry() {
  await Promise.all([squads.refresh(), agents.refresh(), assignees.refresh()])
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
        members: members.value.map(member => {
          const identity = parseMemberIdentity(member.identity)!
          return {
            type: identity.type,
            id: identity.id,
            role: member.role.trim() || null
          }
        })
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
    await apiRequest<void>(apiPath('squads', props.projectId, squad.id), { method: 'DELETE' })
    await squads.refresh()
  } catch (failure) {
    deleteError.value = failure as Error
  } finally {
    deleting.value = undefined
  }
}
</script>

<template>
  <PageFrame title="Squads" description="Project collaboration Squads · one Agent leader/executor with optional Agent and User members, distinct from human access Groups.">
    <template #actions>
      <UButton v-if="canMutate" label="New squad" icon="i-lucide-plus" @click="edit()" />
    </template>

    <UAlert v-if="saved" title="Saved" color="success" class="mb-4" />
    <UAlert v-if="deleteError" title="Unable to delete Squad" :description="deleteError.message" color="error" class="mb-4" />

    <AsyncState
      :pending="squads.pending.value || agents.pending.value || assignees.pending.value"
      :error="squads.error.value || agents.error.value || assignees.error.value"
      :empty="!squads.data.value?.length"
      empty-title="No Squads yet"
      empty-description="Create a reusable Squad with one Agent leader and optional Agent or User members."
      @retry="retry"
    >
      <div class="grid gap-3">
        <UCard v-for="squad in squads.data.value || []" :key="squad.id" :data-squad-id="squad.id">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0 flex-1 space-y-3">
              <div>
                <h2 class="font-medium text-highlighted break-words">{{ squad.name }}</h2>
                <p class="text-xs text-muted">Squad · {{ squad.members.length }} {{ squad.members.length === 1 ? 'member' : 'members' }}</p>
              </div>
              <dl class="space-y-2 text-sm">
                <div class="flex flex-wrap items-center gap-2">
                  <dt><UBadge label="Leader" color="primary" variant="subtle" /></dt>
                  <dd class="flex items-center gap-2">
                    <IdentityAvatar kind="agent" :name="agentName(squad.leaderAgentId)" size="xs" />
                    <span>{{ agentName(squad.leaderAgentId) }} · Agent</span>
                  </dd>
                </div>
                <div v-for="member in squad.members" :key="`${member.type}:${member.id}`" class="flex flex-wrap items-center gap-2">
                  <dt><UBadge label="Member" color="neutral" variant="subtle" /></dt>
                  <dd class="flex items-center gap-2">
                    <IdentityAvatar :kind="member.type === 'AGENT' ? 'agent' : 'user'" :name="memberName(member)" size="xs" />
                    <span>{{ memberName(member) }} · {{ memberTypeLabel(member.type) }}</span>
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

    <UModal v-model:open="open" :title="`${selected ? (readOnly ? 'View' : 'Edit') : 'New'} Squad`" description="Squad membership is durable Project collaboration configuration, not an access-control grant.">
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
                <p class="text-sm text-muted">Optional Agent or User collaborators and descriptive roles. Membership and roles do not grant Project permissions or choose an executor.</p>
              </div>
              <UButton v-if="!readOnly" label="Add member" icon="i-lucide-plus" color="neutral" variant="outline" @click="addMember" />
            </div>

            <div v-for="(member, index) in members" :key="index" class="grid gap-2 rounded-md border border-default p-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]">
              <UFormField label="Agent or User" :name="`member-${index}-identity`">
                <USelectMenu v-model="member.identity" :items="memberOptionsFor(index)" value-key="value" class="w-full" :disabled="readOnly || saving" />
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
