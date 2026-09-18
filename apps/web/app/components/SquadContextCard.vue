<script setup lang="ts">
import { computed } from 'vue'
import type { Assignee, Squad, SquadMember } from '../types/api'
import { apiPath } from '../utils/api'
import { useResource } from '../composables/useResource'

const props = defineProps<{ projectId: string; squadId: string }>()
const squad = useResource<Squad>(() => apiPath('squads', props.projectId, props.squadId))
const assignees = useResource<Assignee[]>(() => apiPath('assignees', props.projectId))

const identityName = (type: SquadMember['type'], id: string) =>
  assignees.data.value?.find(value => value.type === type && value.id === id)?.name || id
const leaderName = computed(() => squad.data.value
  ? identityName('AGENT', squad.data.value.leaderAgentId)
  : '')

async function refresh() {
  await Promise.all([squad.refresh(), assignees.refresh()])
}
</script>

<template>
  <UCard data-squad-context>
    <h2 class="section-label mb-3">Squad context</h2>
    <AsyncState :pending="squad.pending.value || assignees.pending.value" :error="squad.error.value || assignees.error.value" @retry="refresh">
      <div v-if="squad.data.value" class="space-y-3 text-sm">
        <div>
          <p class="text-muted">Owner</p>
          <p>{{ squad.data.value.name }} · Squad</p>
        </div>
        <div>
          <p class="text-muted">Authoritative leader</p>
          <p>{{ leaderName }}</p>
        </div>
        <div>
          <p class="text-muted">Members</p>
          <ul v-if="squad.data.value.members.length" class="space-y-1">
            <li v-for="member in squad.data.value.members" :key="`${member.type}:${member.id}`">
              {{ identityName(member.type, member.id) }} · {{ member.type === 'AGENT' ? 'Agent' : 'User' }}
              <span v-if="member.role"> · {{ member.role }}</span>
            </li>
          </ul>
          <p v-else>None</p>
        </div>
        <p class="text-muted">Members are collaboration context only; delegation remains explicit.</p>
      </div>
    </AsyncState>
  </UCard>
</template>
