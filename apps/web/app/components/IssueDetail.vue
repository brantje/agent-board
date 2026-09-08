<script setup lang="ts">
import {computed,ref} from 'vue'
import type {Issue,Agent,Run} from '../types/api'
import {apiPath,apiRequest} from '../utils/api'
import {latestRun} from '../utils/issues'
import {useResource} from '../composables/useResource'
import {useRefresh} from '../composables/useRefresh'
const props=defineProps<{projectId:string;issueId:string}>()
const {data:issue,pending,error,refresh}=useResource<Issue>(()=>apiPath('issues',props.projectId,props.issueId))
const agents=useResource<Agent[]>(()=>apiPath('agents',props.projectId))
const runs=useResource<Run[]>(()=>apiPath('runs',props.projectId))
const selected=ref('')
const assigning=ref(false)
const assignmentError=ref<Error>()
const editing=ref(false)
const latest=computed(()=>latestRun(runs.data.value || [],props.issueId))
const choices=computed(()=>agents.data.value?.filter(agent=>agent.state==='ENABLED').map(agent=>({label:agent.name,value:agent.id})) || [])
async function reload(){await Promise.all([refresh(),runs.refresh()])}
useRefresh(reload)
async function assign(){
 if(!selected.value || assigning.value || issue.value?.status==='DONE')return
 assigning.value=true;assignmentError.value=undefined
 try {await apiRequest(`${apiPath('issues',props.projectId,props.issueId)}/assignment`,{method:'POST',body:{agentId:selected.value}});await reload()}
 catch(failure){assignmentError.value=failure as Error}finally{assigning.value=false}
}
async function saved(){editing.value=false;await reload()}
</script>
<template><PageFrame :title="issue?.title || 'Issue'"><template #actions><UButton label="Board" :to="`/projects/${projectId}/board`" variant="outline"/><UButton v-if="issue" label="Edit issue" @click="editing=true"/></template><AsyncState :pending="pending" :error="error" @retry="reload"><div v-if="issue" class="detail-grid"><section class="min-w-0 space-y-4"><UCard><h2 class="section-label mb-3">Description</h2><p class="whitespace-pre-wrap break-words">{{issue.description || 'No description provided.'}}</p></UCard><UCard><h2 class="section-label mb-3">Execution</h2><AsyncState :pending="runs.pending.value" :error="runs.error.value" :empty="!latest" empty-title="No Runs yet" @retry="runs.refresh"><template v-if="latest"><div class="flex flex-wrap gap-3 items-center"><UBadge :label="latest.status" color="neutral"/><span>Attempt {{latest.attempt}}</span><UButton label="Open Run" :to="`/projects/${projectId}/runs/${latest.id}`" variant="outline"/></div><p v-if="latest.queueReason" class="mt-3 text-sm">Queued: {{latest.queueReason}}</p><p v-if="latest.failureReason" class="mt-3 text-sm text-error">{{latest.failureReason}}</p></template></AsyncState></UCard><slot name="questions"/></section><aside class="space-y-4"><UCard><h2 class="section-label mb-3">Properties</h2><dl class="space-y-3 text-sm"><div><dt class="text-muted">Board status</dt><dd>{{issue.status}}</dd></div><div><dt class="text-muted">Assigned Agent</dt><dd>{{agents.data.value?.find(agent=>agent.id===issue?.assignedAgentId)?.name || (issue.assignedAgentId ? 'Assigned Agent unavailable' : 'Unassigned')}}</dd></div><div><dt class="text-muted">Issue</dt><dd class="font-mono break-all">{{issue.id}}</dd></div></dl></UCard><UCard><h2 class="section-label mb-3">Assignment</h2><AsyncState :pending="agents.pending.value" :error="agents.error.value" @retry="agents.refresh"><UForm :state="{selected}" class="space-y-3" @submit="assign"><UAlert v-if="assignmentError" title="Unable to assign" :description="assignmentError.message" color="error"/><UFormField label="Runnable Agent" name="agent"><USelect v-model="selected" :items="choices" :disabled="assigning || issue.status==='DONE'" class="w-full"/></UFormField><p v-if="issue.status==='DONE'" class="text-sm text-muted">Reopen this Issue before assigning work.</p><p v-else-if="issue.assignedAgentId" class="text-sm text-muted">Changing Agent may cancel and replace the current attempt on the same Workspace.</p><p v-else class="text-sm text-muted">Assignment durably schedules a Run. Execution continues when you close this page.</p><UButton label="Assign Agent" type="submit" :loading="assigning" :disabled="!selected || issue.status==='DONE'"/></UForm></AsyncState></UCard></aside></div></AsyncState><UModal v-model:open="editing" title="Edit issue" description="Update the durable Issue."><template #body><IssueEditor v-if="issue" :project-id="projectId" :issue="issue" @saved="saved" @cancel="editing=false"/></template></UModal></PageFrame></template>
