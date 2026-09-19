<script setup lang="ts">
import { computed, ref } from 'vue'
import type { IssueComment, IssueTimelineEntry } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { eventActivityIcon, eventDescription, eventTitle, formatActivityTime } from '../utils/events'
import { useResource } from '../composables/useResource'
import MarkdownContent from './MarkdownContent.vue'

const props = withDefaults(defineProps<{ projectId: string; issueId: string; canMutate?: boolean }>(), { canMutate: true })
const timeline = useResource<IssueTimelineEntry[]>(() => `${apiPath('issues', props.projectId, props.issueId)}/timeline`)
const body = ref('')
const replyTo = ref<IssueComment>()
const submitting = ref(false)
const submitError = ref<Error>()

const commentsById = computed(() => new Map(
  (timeline.data.value || [])
    .flatMap(entry => entry.comment ? [entry.comment] : [])
    .map(comment => [comment.id, comment])
))

function replyLabel(comment: IssueComment) {
  if (!comment.parentCommentId) return undefined
  return commentsById.value.get(comment.parentCommentId)?.author.name || 'an earlier comment'
}

function beginReply(comment: IssueComment) {
  replyTo.value = comment
}

function cancelReply() {
  replyTo.value = undefined
}

async function submit() {
  const content = body.value.trim()
  if (!props.canMutate || !content || submitting.value) return

  submitting.value = true
  submitError.value = undefined
  try {
    await apiRequest<IssueComment>(`${apiPath('issues', props.projectId, props.issueId)}/comments`, {
      method: 'POST',
      body: {
        body: content,
        parentCommentId: replyTo.value?.id ?? null
      }
    })
    body.value = ''
    replyTo.value = undefined
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
              <IdentityAvatar kind="user" :name="entry.comment.author.name" size="xs" />
              <strong>{{ entry.comment.author.name }}</strong>
              <span class="text-muted">· {{ formatActivityTime(entry.occurredAt) }}</span>
            </div>
            <p v-if="replyLabel(entry.comment)" class="mt-2 text-xs text-muted">
              Replying to {{ replyLabel(entry.comment) }}
            </p>
            <MarkdownContent class="mt-2" :content="entry.comment.body" />
            <UButton
              v-if="canMutate"
              class="mt-2"
              label="Reply"
              variant="ghost"
              size="sm"
              @click="beginReply(entry.comment)"
            />
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
        <UTextarea v-model="body" :disabled="submitting" placeholder="Write a comment…" class="w-full" />
      </UFormField>
      <UButton :label="replyTo ? 'Post reply' : 'Post comment'" type="submit" :loading="submitting" :disabled="!body.trim()" />
    </form>
  </UCard>
</template>
