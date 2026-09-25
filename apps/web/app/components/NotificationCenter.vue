<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import type { UserNotification, UserNotificationsResponse } from '../types/api'
import { apiPath, apiRequest } from '../utils/api'
import { useProjectEvents } from '../composables/useProjectEvents'

const auth = useAuth()
const open = ref(false)
const pending = ref(false)
const error = ref<Error>()
const mutationError = ref<Error>()
const page = ref<UserNotificationsResponse>({ notifications: [], unreadCount: 0 })
const projectIds = ref<string[]>([])
let generation = 0

const unreadLabel = computed(() => page.value.unreadCount > 99 ? '99+' : String(page.value.unreadCount))

async function load(showPending = false) {
  if (!auth.user.value) return
  const current = ++generation
  if (showPending) pending.value = true
  error.value = undefined
  try {
    const value = await apiRequest<UserNotificationsResponse>(apiPath('notifications'))
    if (current !== generation) return
    page.value = value
    projectIds.value = [...new Set(value.notifications.map(notification => notification.projectId))]
  } catch (failure) {
    if (current === generation) error.value = failure as Error
  } finally {
    if (current === generation) pending.value = false
  }
}

function notificationTo(notification: UserNotification) {
  return `/projects/${notification.projectId}/issues/${notification.issueKey}#comment-${notification.sourceCommentId}`
}

function notificationTitle(notification: UserNotification) {
  return notification.kind === 'COMMENT_REPLY' ? `${notification.commentAuthorName} replied to your comment` : `${notification.commentAuthorName} commented on ${notification.issueKey}`
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
}

async function setRead(notification: UserNotification, read: boolean) {
  mutationError.value = undefined
  try {
    await apiRequest<void>(apiPath('notifications', undefined, notification.id), { method: 'PATCH', body: { read } })
    notification.readAt = read ? new Date().toISOString() : null
    page.value.unreadCount = page.value.notifications.filter(item => !item.readAt).length
  } catch (failure) {
    mutationError.value = failure as Error
  }
}

async function openNotification(notification: UserNotification) {
  if (!notification.readAt) {
    try {
      await setRead(notification, true)
    } catch {
      // Navigation remains useful even if the read-state request is unavailable.
    }
  }
  open.value = false
}

async function markAllRead() {
  if (!page.value.unreadCount) return
  mutationError.value = undefined
  try {
    await apiRequest<void>(`${apiPath('notifications')}/read-all`, { method: 'POST' })
    const now = new Date().toISOString()
    for (const notification of page.value.notifications) notification.readAt ||= now
    page.value.unreadCount = 0
  } catch (failure) {
    mutationError.value = failure as Error
  }
}

useProjectEvents(projectIds, event => {
  if (event.type === 'issue.comment_created') void load(false)
})

watch(auth.user, user => {
  if (user) void load(true)
  else {
    generation++
    page.value = { notifications: [], unreadCount: 0 }
    projectIds.value = []
  }
})

onMounted(() => {
  if (auth.user.value) void load(true)
})
</script>

<template>
  <UPopover v-if="auth.user.value" v-model:open="open" :ui="{ content: 'w-[min(28rem,calc(100vw-2rem))] p-0' }">
    <UButton
      icon="i-lucide-bell"
      color="neutral"
      variant="ghost"
      square
      aria-label="Notifications"
      data-testid="notification-center-trigger"
      class="relative"
      @click="open = true"
    >
      <UBadge
        v-if="page.unreadCount"
        :label="unreadLabel"
        color="primary"
        size="xs"
        class="absolute -right-1 -top-1 min-w-4 justify-center px-1"
        aria-label="Unread notifications"
      />
    </UButton>
    <template #content>
      <UCard :ui="{ body: 'p-0' }">
        <div class="flex items-center justify-between gap-3 border-b border-default px-4 py-3">
          <div>
            <h2 class="font-semibold">Notifications</h2>
            <p class="text-xs text-muted">All accessible Issue discussion activity.</p>
          </div>
          <UButton label="Mark all read" size="xs" variant="ghost" :disabled="!page.unreadCount || pending" @click="markAllRead" />
        </div>
        <div v-if="pending" class="px-4 py-6 text-sm text-muted" role="status">Loading notifications…</div>
        <UAlert v-else-if="error" class="m-3" title="Unable to load notifications" :description="error.message" color="error" :actions="[{ label: 'Retry', onClick: () => load(true) }]" />
        <template v-else>
          <UAlert v-if="mutationError" class="m-3" title="Unable to update notification state" :description="mutationError.message" color="error" />
          <div v-else-if="!page.notifications.length" class="px-4 py-6 text-sm text-muted">No notifications yet.</div>
          <ul v-else class="max-h-[min(32rem,70vh)] divide-y divide-default overflow-y-auto" aria-label="Notifications">
            <li v-for="notification in page.notifications" :key="notification.id" :class="!notification.readAt ? 'bg-primary/5' : ''">
              <div class="flex items-start gap-3 px-4 py-3">
                <NuxtLink :to="notificationTo(notification)" class="min-w-0 flex-1 focus-visible:outline-2 focus-visible:outline-primary" @click="openNotification(notification)">
                  <p class="text-sm font-medium">{{ notificationTitle(notification) }}</p>
                  <p class="mt-1 text-xs text-muted">{{ notification.projectName }} · {{ notification.issueKey }} · {{ formatTime(notification.createdAt) }}</p>
                  <p class="mt-2 line-clamp-2 text-sm text-muted">{{ notification.preview }}</p>
                </NuxtLink>
                <UButton
                  :label="notification.readAt ? 'Mark unread' : 'Mark read'"
                  size="xs"
                  variant="ghost"
                  :aria-label="notification.readAt ? 'Mark notification unread' : 'Mark notification read'"
                  @click="setRead(notification, !notification.readAt)"
                />
              </div>
            </li>
          </ul>
        </template>
      </UCard>
    </template>
  </UPopover>
</template>
