<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type {
  Agent,
  IssueCommentTarget,
  IssueComment,
  IssueCommentImplicitReasonCode,
  IssueCommentImplicitRoutingReason,
  IssueCommentImplicitTrigger,
  IssueCommentMentionReasonCode,
  IssueCommentReactionKey,
  IssueCommentTriggerPreview,
  IssueTimelineEntry,
  Squad
} from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { eventActivityIcon, eventDescription, eventTitle, formatActivityTime } from '../utils/events'
import { useResource } from '../composables/useResource'
import MarkdownContent from './MarkdownContent.vue'

const props = withDefaults(defineProps<{ projectId: string; issueId: string; canMutate?: boolean }>(), { canMutate: true })
const auth = useAuth()
const timeline = useResource<IssueTimelineEntry[]>(() => `${apiPath('issues', props.projectId, props.issueId)}/timeline`)
const body = ref('')
const replyTo = ref<IssueComment>()
const submitting = ref(false)
const submitError = ref<Error>()
const actionError = ref<Error>()
const actionBusy = ref('')
const editingCommentId = ref('')
const editBody = ref('')
const mentionPickerOpen = ref(false)
const mentionLoading = ref(false)
const mentionLoadError = ref<Error>()
const mentionQuery = ref('')
const mentionAgents = ref<Agent[]>([])
const mentionSquads = ref<Squad[]>([])
const selectedMentionAgentIDs = ref<string[]>([])
const selectedMentionSquadIDs = ref<string[]>([])
const triggerPreview = ref<IssueCommentTriggerPreview>({ mentions: [], implicit: null })
const triggerPreviewError = ref<Error>()
const suppressImplicitAgentTrigger = ref(false)
const draftRequestId = ref('')
let triggerPreviewGeneration = 0

const reactionOptions: Array<{ key: IssueCommentReactionKey; emoji: string; label: string }> = [
  { key: 'THUMBS_UP', emoji: '👍', label: 'Thumbs up' },
  { key: 'THUMBS_DOWN', emoji: '👎', label: 'Thumbs down' },
  { key: 'LAUGH', emoji: '😄', label: 'Laugh' },
  { key: 'HOORAY', emoji: '🎉', label: 'Hooray' },
  { key: 'CONFUSED', emoji: '😕', label: 'Confused' },
  { key: 'HEART', emoji: '❤️', label: 'Heart' },
  { key: 'ROCKET', emoji: '🚀', label: 'Rocket' },
  { key: 'EYES', emoji: '👀', label: 'Eyes' }
]

const commentsById = computed(() => new Map(
  (timeline.data.value || [])
    .flatMap(entry => entry.comment ? [entry.comment] : [])
    .map(comment => [comment.id, comment])
))

const selectedMentionAgents = computed(() => {
  const byID = new Map(mentionAgents.value.map(agent => [agent.id, agent]))
  return selectedMentionAgentIDs.value.map(id => byID.get(id)).filter((agent): agent is Agent => Boolean(agent))
})

const selectedMentionSquads = computed(() => {
  const byID = new Map(mentionSquads.value.map(squad => [squad.id, squad]))
  return selectedMentionSquadIDs.value.map(id => byID.get(id)).filter((squad): squad is Squad => Boolean(squad))
})

const selectedMentionTargets = computed<IssueCommentTarget[]>(() => [
  ...selectedMentionAgents.value.map(agent => ({ type: 'AGENT' as const, id: agent.id, name: agent.name })),
  ...selectedMentionSquads.value.map(squad => ({ type: 'SQUAD' as const, id: squad.id, name: squad.name }))
])

const filteredMentionAgents = computed(() => {
  const query = mentionQuery.value.trim().toLocaleLowerCase()
  const selected = new Set(selectedMentionAgentIDs.value)
  return mentionAgents.value
    .filter(agent => !selected.has(agent.id))
    .filter(agent => !query || agent.name.toLocaleLowerCase().includes(query))
    .slice(0, 8)
})

const filteredMentionSquads = computed(() => {
  const query = mentionQuery.value.trim().toLocaleLowerCase()
  const selected = new Set(selectedMentionSquadIDs.value)
  return mentionSquads.value
    .filter(squad => !selected.has(squad.id))
    .filter(squad => !query || squad.name.toLocaleLowerCase().includes(query))
    .slice(0, 8)
})

function replyLabel(comment: IssueComment) {
  if (!comment.parentCommentId) return undefined
  return commentsById.value.get(comment.parentCommentId)?.author.name || 'an earlier comment'
}

function isOwnComment(comment: IssueComment) {
  return comment.author.type === 'HUMAN' && comment.author.id === auth.user.value?.id
}

function canEditComment(comment: IssueComment) {
  return props.canMutate && !comment.deletedAt && isOwnComment(comment)
}

function wasEdited(comment: IssueComment) {
  return comment.updatedAt !== comment.createdAt
}

function resetDraftRequestIdentity() {
  if (!submitting.value) draftRequestId.value = ''
}

function routingInputChanged() {
  resetDraftRequestIdentity()
  triggerPreviewGeneration++
  triggerPreview.value = { mentions: [], implicit: null }
  triggerPreviewError.value = undefined
}

watch(body, (_value, _oldValue, onCleanup) => {
  routingInputChanged()
  const timer = globalThis.setTimeout(() => {
    void refreshTriggerPreview()
  }, 250)
  onCleanup(() => globalThis.clearTimeout(timer))
})
watch(suppressImplicitAgentTrigger, () => {
  routingInputChanged()
  void refreshTriggerPreview()
})

function beginReply(comment: IssueComment) {
  if (comment.deletedAt) return
  replyTo.value = comment
  routingInputChanged()
  void refreshTriggerPreview()
}

function cancelReply() {
  replyTo.value = undefined
  routingInputChanged()
  void refreshTriggerPreview()
}

function mentionReasonLabel(reason: IssueCommentMentionReasonCode | IssueCommentImplicitReasonCode | null | undefined) {
  switch (reason) {
    case 'TARGET_UNAVAILABLE': return 'Agent unavailable'
    case 'TARGET_BUSY': return 'Agent already has active work on this Issue'
    case 'DELEGATION_BLOCKED': return 'Delegation policy blocked this request'
    case 'WORKFLOW_BLOCKED': return 'Issue workflow does not allow an implicit Agent wakeup'
    default: return 'Unable to queue work'
  }
}

function routingReasonLabel(reason: IssueCommentImplicitRoutingReason) {
  switch (reason) {
    case 'DIRECT_AGENT_REPLY': return 'replying directly to this Agent'
    case 'UNIQUE_THREAD_AGENT': return 'only Agent participating in this discussion'
    case 'ISSUE_ASSIGNEE': return 'current Issue Agent assignee'
    case 'ISSUE_SQUAD_ASSIGNEE': return 'current Issue Squad assignee'
  }
}

function implicitPreviewStatus() {
  const preview = triggerPreview.value.implicit
  if (!preview) return ''
  if (preview.suppressed) return 'Automatic Agent trigger suppressed'
  if (preview.eligible) return 'Will request Agent work'
  return mentionReasonLabel(preview.reasonCode)
}

function triggerOutcomeLabel(outcome: string, reason: IssueCommentMentionReasonCode | IssueCommentImplicitReasonCode | null | undefined) {
  switch (outcome) {
    case 'QUEUED': return 'Work queued'
    case 'COALESCED': return 'Folded into pending Agent work'
    case 'DEFERRED': return 'Follow-up saved until current Agent work finishes'
    case 'SUPPRESSED': return 'Automatic Agent trigger suppressed'
    default: return mentionReasonLabel(reason)
  }
}

function implicitTriggerStatus(trigger: IssueCommentImplicitTrigger) {
  return triggerOutcomeLabel(trigger.outcome, trigger.reasonCode)
}

function mentionPreviewFor(agentID: string) {
  return triggerPreview.value.mentions.find(item => item.targetAgentId === agentID)
}

function mentionPreviewLabel(agentID: string) {
  const preview = mentionPreviewFor(agentID)
  if (!preview) return 'Checking eligibility…'
  return preview.eligible ? 'Eligible to request work' : mentionReasonLabel(preview.reasonCode)
}

async function loadMentionAgents() {
  mentionPickerOpen.value = true
  if ((mentionAgents.value.length && mentionSquads.value.length) || mentionLoading.value) return
  mentionLoading.value = true
  mentionLoadError.value = undefined
  try {
    mentionAgents.value = await apiRequest<Agent[]>(apiPath('agents', props.projectId))
    try {
      mentionSquads.value = await apiRequest<Squad[]>(apiPath('squads', props.projectId))
    } catch {
      // Agent selection remains usable when an older deployment does not yet expose Squads.
      mentionSquads.value = []
    }
  } catch (failure) {
    mentionLoadError.value = failure as Error
  } finally {
    mentionLoading.value = false
  }
}

async function refreshTriggerPreview() {
  const targetAgentIDs = [...selectedMentionAgentIDs.value]
  const generation = ++triggerPreviewGeneration
  triggerPreviewError.value = undefined
  try {
    const value = await apiRequest<IssueCommentTriggerPreview>(
      `${apiPath('issues', props.projectId, props.issueId)}/comments/trigger-preview`,
      {
        method: 'POST',
        body: {
          parentCommentId: replyTo.value?.id ?? null,
          body: body.value.trim(),
          ...(selectedMentionSquads.value.length ? { mentionTargets: selectedMentionTargets.value.map(({ type, id }) => ({ type, id })) } : {}),
          mentionAgentIds: targetAgentIDs,
          suppressImplicitAgentTrigger: suppressImplicitAgentTrigger.value
        }
      }
    )
    if (generation === triggerPreviewGeneration) triggerPreview.value = value
  } catch (failure) {
    if (generation === triggerPreviewGeneration) {
      triggerPreview.value = { mentions: [], implicit: null }
      triggerPreviewError.value = failure as Error
    }
  }
}

function addSquad(squad: Squad) {
  if (selectedMentionSquadIDs.value.includes(squad.id)) return
  selectedMentionSquadIDs.value = [...selectedMentionSquadIDs.value, squad.id]
  mentionQuery.value = ''
  routingInputChanged()
  void refreshTriggerPreview()
}

async function addMention(agent: Agent) {
  if (selectedMentionAgentIDs.value.includes(agent.id)) return
  selectedMentionAgentIDs.value = [...selectedMentionAgentIDs.value, agent.id]
  mentionQuery.value = ''
  routingInputChanged()
  await refreshTriggerPreview()
}

async function removeMention(agentID: string) {
  selectedMentionAgentIDs.value = selectedMentionAgentIDs.value.filter(id => id !== agentID)
  routingInputChanged()
  await refreshTriggerPreview()
}

async function removeSquad(squadID: string) {
  selectedMentionSquadIDs.value = selectedMentionSquadIDs.value.filter(id => id !== squadID)
  routingInputChanged()
  await refreshTriggerPreview()
}

function beginEdit(comment: IssueComment) {
  if (!canEditComment(comment) || comment.body === null) return
  editingCommentId.value = comment.id
  editBody.value = comment.body
  actionError.value = undefined
}

function cancelEdit() {
  editingCommentId.value = ''
  editBody.value = ''
}

function commentPath(comment: IssueComment) {
  return `${apiPath('issues', props.projectId, props.issueId)}/comments/${encodeURIComponent(comment.id)}`
}

function reactionSummary(comment: IssueComment, reaction: IssueCommentReactionKey) {
  return comment.reactions.find(item => item.reaction === reaction)
}

function reactionOption(reaction: IssueCommentReactionKey) {
  return reactionOptions.find(option => option.key === reaction)
}

function reactionButtonLabel(comment: IssueComment, reaction: IssueCommentReactionKey) {
  const option = reactionOption(reaction)
  const count = reactionSummary(comment, reaction)?.count ?? 0
  return `${option?.emoji ?? reaction}${count ? ` ${count}` : ''}`
}

async function refreshAfterAction() {
  await timeline.refresh()
}

async function saveEdit(comment: IssueComment) {
  const content = editBody.value.trim()
  if (!canEditComment(comment) || !content || actionBusy.value) return
  actionBusy.value = `edit:${comment.id}`
  actionError.value = undefined
  try {
    await apiRequest<IssueComment>(commentPath(comment), { method: 'PATCH', body: { body: content } })
    cancelEdit()
    await refreshAfterAction()
  } catch (failure) {
    actionError.value = failure as Error
  } finally {
    actionBusy.value = ''
  }
}

async function deleteComment(comment: IssueComment) {
  if (!canEditComment(comment) || actionBusy.value) return
  actionBusy.value = `delete:${comment.id}`
  actionError.value = undefined
  try {
    await apiRequest<void>(commentPath(comment), { method: 'DELETE' })
    if (replyTo.value?.id === comment.id) cancelReply()
    if (editingCommentId.value === comment.id) cancelEdit()
    await refreshAfterAction()
  } catch (failure) {
    actionError.value = failure as Error
  } finally {
    actionBusy.value = ''
  }
}

async function setResolved(comment: IssueComment, resolved: boolean) {
  if (!props.canMutate || comment.deletedAt || comment.parentCommentId || actionBusy.value) return
  actionBusy.value = `resolution:${comment.id}`
  actionError.value = undefined
  try {
    await apiRequest<IssueComment>(`${commentPath(comment)}/resolution`, {
      method: resolved ? 'PUT' : 'DELETE'
    })
    await refreshAfterAction()
  } catch (failure) {
    actionError.value = failure as Error
  } finally {
    actionBusy.value = ''
  }
}

async function toggleReaction(comment: IssueComment, reaction: IssueCommentReactionKey) {
  if (!props.canMutate || comment.deletedAt || actionBusy.value) return
  const existing = reactionSummary(comment, reaction)
  actionBusy.value = `reaction:${comment.id}:${reaction}`
  actionError.value = undefined
  try {
    await apiRequest<void>(`${commentPath(comment)}/reactions/${encodeURIComponent(reaction)}`, {
      method: existing?.reactedByCurrentUser ? 'DELETE' : 'PUT'
    })
    await refreshAfterAction()
  } catch (failure) {
    actionError.value = failure as Error
  } finally {
    actionBusy.value = ''
  }
}

async function submit() {
  const content = body.value.trim()
  if (!props.canMutate || !content || submitting.value) return

  const mentionAgentIds = [...selectedMentionAgentIDs.value]
  if (!draftRequestId.value) {
    draftRequestId.value = globalThis.crypto.randomUUID()
  }
  const requestBody: {
    body: string
    parentCommentId: string | null
    requestId: string
    mentionTargets?: Array<{ type: 'AGENT' | 'SQUAD'; id: string }>
    mentionAgentIds?: string[]
    suppressImplicitAgentTrigger: boolean
  } = {
    body: content,
    parentCommentId: replyTo.value?.id ?? null,
    requestId: draftRequestId.value,
    suppressImplicitAgentTrigger: suppressImplicitAgentTrigger.value
  }
  if (mentionAgentIds.length) {
    requestBody.mentionAgentIds = mentionAgentIds
  }
  if (selectedMentionSquads.value.length) {
    requestBody.mentionTargets = selectedMentionTargets.value.map(({ type, id }) => ({ type, id }))
  }

  submitting.value = true
  submitError.value = undefined
  try {
    await apiRequest<IssueComment>(`${apiPath('issues', props.projectId, props.issueId)}/comments`, {
      method: 'POST',
      body: requestBody
    })
    body.value = ''
    replyTo.value = undefined
    mentionPickerOpen.value = false
    mentionQuery.value = ''
    selectedMentionAgentIDs.value = []
    selectedMentionSquadIDs.value = []
    triggerPreview.value = { mentions: [], implicit: null }
    triggerPreviewError.value = undefined
    suppressImplicitAgentTrigger.value = false
    draftRequestId.value = ''
    await timeline.refresh()
  } catch (failure) {
    submitError.value = failure as Error
  } finally {
    submitting.value = false
  }
}

defineExpose({ refresh: timeline.refresh })
</script>

<template>
  <UCard>
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="section-label">Discussion & activity</h2>
        <p class="mt-1 text-sm text-muted">Durable Issue collaboration and workflow activity.</p>
      </div>
    </div>

    <UAlert v-if="actionError" class="mb-3" title="Unable to update comment" :description="actionError.message" color="error" />

    <AsyncState
      :pending="timeline.pending.value"
      :error="timeline.error.value"
      :empty="!timeline.data.value?.length"
      empty-title="No discussion yet"
      empty-description="Comments and Issue activity will appear here."
      @retry="timeline.refresh"
    >
      <ol v-if="timeline.data.value?.length" class="space-y-4" aria-label="Issue timeline">
        <li v-for="entry in timeline.data.value" :key="`${entry.kind}:${entry.id}`">
          <article
            v-if="entry.kind === 'comment' && entry.comment"
            class="rounded-md border border-default p-3"
            :class="entry.comment.parentCommentId ? 'ms-6' : ''"
          >
            <div class="flex flex-wrap items-center gap-2 text-sm">
              <IdentityAvatar :kind="entry.comment.author.type === 'AGENT' ? 'agent' : 'user'" :name="entry.comment.author.name" size="xs" />
              <strong>{{ entry.comment.author.name }}</strong>
              <UBadge v-if="entry.comment.author.type === 'AGENT'" label="Agent" size="xs" variant="subtle" />
              <NuxtLink
                v-if="entry.comment.sourceRunId"
                :to="`/projects/${projectId}/runs/${entry.comment.sourceRunId}`"
                class="text-xs text-muted hover:text-primary focus-visible:outline-2 focus-visible:outline-primary"
              >
                · via Run
              </NuxtLink>
              <span class="text-muted">· {{ formatActivityTime(entry.occurredAt) }}</span>
              <span v-if="wasEdited(entry.comment)" class="text-muted">· edited</span>
            </div>

            <p v-if="replyLabel(entry.comment)" class="mt-2 text-xs text-muted">
              Replying to {{ replyLabel(entry.comment) }}
            </p>

            <div
              v-if="entry.comment.resolvedAt"
              class="mt-2 rounded-md bg-elevated px-2 py-1 text-xs text-muted"
            >
              Resolved by {{ entry.comment.resolvedBy?.name || 'a user' }} · {{ formatActivityTime(entry.comment.resolvedAt) }}
            </div>

            <p v-if="entry.comment.deletedAt" class="mt-2 italic text-muted">Comment deleted</p>
            <div v-else-if="editingCommentId === entry.comment.id" class="mt-2 space-y-2">
              <UTextarea v-model="editBody" :disabled="Boolean(actionBusy)" class="w-full" />
              <div class="flex gap-2">
                <UButton
                  label="Save edit"
                  size="sm"
                  :loading="actionBusy === `edit:${entry.comment.id}`"
                  :disabled="!editBody.trim()"
                  @click="saveEdit(entry.comment)"
                />
                <UButton label="Cancel edit" size="sm" variant="ghost" :disabled="Boolean(actionBusy)" @click="cancelEdit" />
              </div>
            </div>
            <MarkdownContent v-else-if="entry.comment.body !== null" class="mt-2" :content="entry.comment.body" />

            <div v-if="entry.comment.mentions?.length" class="mt-3 space-y-2" aria-label="Structured collaboration targets">
              <div
                v-for="mention in entry.comment.mentions"
                :key="mention.id"
                class="flex flex-wrap items-center gap-2 rounded-md bg-elevated px-2 py-1 text-xs"
              >
                <UBadge :label="`@${mention.targetName || mention.targetAgentName || (mention.targetType === 'SQUAD' ? 'Unavailable Squad' : 'Unavailable Agent')}`" size="xs" variant="subtle" />
                <span>{{ triggerOutcomeLabel(mention.outcome, mention.reasonCode) }}</span>
                <span v-if="mention.targetType === 'SQUAD'" class="text-muted">· leader: {{ mention.resolvedAgentName || 'unavailable' }}</span>
                <NuxtLink
                  v-if="mention.delegatedRunId"
                  :to="`/projects/${projectId}/runs/${mention.delegatedRunId}`"
                  class="text-primary hover:underline focus-visible:outline-2 focus-visible:outline-primary"
                >
                  Open delegated Run
                </NuxtLink>
              </div>
            </div>

            <div
              v-if="entry.comment.implicitTrigger"
              class="mt-3 flex flex-wrap items-center gap-2 rounded-md bg-elevated px-2 py-1 text-xs"
              aria-label="Implicit collaboration trigger"
            >
              <UBadge :label="`@${entry.comment.implicitTrigger.targetName || entry.comment.implicitTrigger.targetAgentName || (entry.comment.implicitTrigger.targetType === 'SQUAD' ? 'Unavailable Squad' : 'Unavailable Agent')}`" size="xs" variant="subtle" />
              <span>{{ implicitTriggerStatus(entry.comment.implicitTrigger) }}</span>
              <span v-if="entry.comment.implicitTrigger.targetType === 'SQUAD'" class="text-muted">· leader: {{ entry.comment.implicitTrigger.resolvedAgentName || 'unavailable' }}</span>
              <span class="text-muted">· {{ routingReasonLabel(entry.comment.implicitTrigger.routingReason) }}</span>
              <NuxtLink
                v-if="entry.comment.implicitTrigger.delegatedRunId"
                :to="`/projects/${projectId}/runs/${entry.comment.implicitTrigger.delegatedRunId}`"
                class="text-primary hover:underline focus-visible:outline-2 focus-visible:outline-primary"
              >
                Open delegated Run
              </NuxtLink>
            </div>

            <div v-if="!entry.comment.deletedAt" class="mt-2 flex flex-wrap gap-2">
              <UButton
                v-if="canMutate"
                label="Reply"
                variant="ghost"
                size="sm"
                :disabled="Boolean(actionBusy)"
                @click="beginReply(entry.comment)"
              />
              <UButton
                v-if="canEditComment(entry.comment)"
                label="Edit"
                variant="ghost"
                size="sm"
                :disabled="Boolean(actionBusy)"
                @click="beginEdit(entry.comment)"
              />
              <UButton
                v-if="canEditComment(entry.comment)"
                label="Delete"
                variant="ghost"
                size="sm"
                :loading="actionBusy === `delete:${entry.comment.id}`"
                @click="deleteComment(entry.comment)"
              />
              <UButton
                v-if="canMutate && !entry.comment.parentCommentId && !entry.comment.resolvedAt"
                label="Resolve"
                variant="ghost"
                size="sm"
                :loading="actionBusy === `resolution:${entry.comment.id}`"
                @click="setResolved(entry.comment, true)"
              />
              <UButton
                v-if="canMutate && !entry.comment.parentCommentId && entry.comment.resolvedAt"
                label="Reopen"
                variant="ghost"
                size="sm"
                :loading="actionBusy === `resolution:${entry.comment.id}`"
                @click="setResolved(entry.comment, false)"
              />
            </div>

            <div v-if="!entry.comment.deletedAt" class="mt-2 flex flex-wrap gap-1" aria-label="Comment reactions">
              <template v-if="canMutate">
                <UButton
                  v-for="option in reactionOptions"
                  :key="option.key"
                  :label="reactionButtonLabel(entry.comment, option.key)"
                  size="sm"
                  :variant="reactionSummary(entry.comment, option.key)?.reactedByCurrentUser ? 'soft' : 'ghost'"
                  :disabled="Boolean(actionBusy)"
                  :title="option.label"
                  @click="toggleReaction(entry.comment, option.key)"
                />
              </template>
              <template v-else>
                <span
                  v-for="reaction in entry.comment.reactions"
                  :key="reaction.reaction"
                  class="rounded-md bg-elevated px-2 py-1 text-xs"
                >
                  {{ reactionButtonLabel(entry.comment, reaction.reaction) }}
                </span>
              </template>
            </div>
          </article>

          <div v-else-if="entry.kind === 'activity' && entry.activity" class="flex gap-3 px-1 text-sm">
            <UIcon :name="eventActivityIcon(entry.activity.type)" class="mt-0.5 shrink-0 text-muted" />
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <strong>{{ eventTitle(entry.activity) }}</strong>
                <span class="text-muted">· {{ formatActivityTime(entry.occurredAt) }}</span>
              </div>
              <p v-if="eventDescription(entry.activity)" class="mt-1 break-words text-muted">
                {{ eventDescription(entry.activity) }}
              </p>
            </div>
          </div>
        </li>
      </ol>
    </AsyncState>

    <form v-if="canMutate" class="mt-5 space-y-3 border-t border-default pt-4" @submit.prevent="submit">
      <div v-if="replyTo" class="flex items-center justify-between gap-3 rounded-md bg-elevated p-2 text-sm">
        <span>Replying to {{ replyTo.author.name }}</span>
        <UButton label="Cancel reply" variant="ghost" size="sm" @click="cancelReply" />
      </div>
      <UAlert v-if="submitError" title="Unable to post comment" :description="submitError.message" color="error" />
      <UFormField label="Comment" name="issue-comment">
        <UTextarea v-model="body" :disabled="submitting" placeholder="Write a comment…" class="w-full" @focus="refreshTriggerPreview" />
      </UFormField>

      <div class="space-y-2">
        <div class="flex flex-wrap items-center gap-2">
          <UButton label="Mention Agent" variant="outline" size="sm" :disabled="submitting" @click="loadMentionAgents" />
          <div v-for="agent in selectedMentionAgents" :key="agent.id" class="flex items-center gap-1 rounded-md bg-elevated px-2 py-1 text-xs">
            <span>@{{ agent.name }}</span>
            <UButton :label="`Remove @${agent.name}`" variant="ghost" size="sm" :disabled="submitting" @click="removeMention(agent.id)" />
          </div>
          <div v-for="squad in selectedMentionSquads" :key="squad.id" class="flex items-center gap-1 rounded-md bg-elevated px-2 py-1 text-xs">
            <span>@{{ squad.name }} · Squad</span>
            <UButton :label="`Remove @${squad.name}`" variant="ghost" size="sm" :disabled="submitting" @click="removeSquad(squad.id)" />
          </div>
        </div>

        <div v-if="mentionPickerOpen" class="space-y-2 rounded-md border border-default p-2">
          <UInput v-model="mentionQuery" :disabled="submitting || mentionLoading" placeholder="Filter Agents…" />
          <p v-if="mentionLoading" class="text-xs text-muted">Loading Agents…</p>
          <UAlert v-else-if="mentionLoadError" title="Unable to load Agents" :description="mentionLoadError.message" color="error" />
          <div v-else class="flex flex-wrap gap-2">
            <UButton
              v-for="agent in filteredMentionAgents"
              :key="agent.id"
              :label="`@${agent.name}`"
              variant="ghost"
              size="sm"
              :disabled="submitting"
              @click="addMention(agent)"
            />
            <UButton
              v-for="squad in filteredMentionSquads"
              :key="squad.id"
              :label="`@${squad.name} · Squad`"
              variant="ghost"
              size="sm"
              :disabled="submitting"
              @click="addSquad(squad)"
            />
            <span v-if="!filteredMentionAgents.length && !filteredMentionSquads.length" class="text-xs text-muted">No matching Agents or Squads.</span>
          </div>
        </div>

        <UAlert v-if="triggerPreviewError" title="Trigger preview unavailable" :description="triggerPreviewError.message" color="warning" />
        <div v-if="selectedMentionTargets.length" class="space-y-1 text-xs text-muted" aria-label="Mention target preview">
          <p v-for="agent in selectedMentionAgents" :key="agent.id">
            @{{ agent.name }} · {{ mentionPreviewLabel(agent.id) }}
          </p>
          <p v-for="squad in selectedMentionSquads" :key="squad.id">
            @{{ squad.name }} · Squad target
          </p>
        </div>
        <div
          v-else-if="triggerPreview.implicit"
          class="space-y-2 rounded-md bg-elevated p-2 text-xs"
          aria-label="Implicit Agent routing preview"
        >
          <p>
            @{{ triggerPreview.implicit.targetName || triggerPreview.implicit.targetAgentName || (triggerPreview.implicit.targetType === 'SQUAD' ? 'Unavailable Squad' : 'Unavailable Agent') }}
            · {{ routingReasonLabel(triggerPreview.implicit.routingReason) }}
            · {{ implicitPreviewStatus() }}
          </p>
          <label class="flex items-center gap-2">
            <UCheckbox v-model="suppressImplicitAgentTrigger" :disabled="submitting" />
            <span>Don’t notify Agent for this comment</span>
          </label>
        </div>
      </div>

      <UButton :label="replyTo ? 'Post reply' : 'Post comment'" type="submit" :loading="submitting" :disabled="!body.trim()" />
    </form>
  </UCard>
</template>
