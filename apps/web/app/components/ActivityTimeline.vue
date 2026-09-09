<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import type { RunActivityItem, ToolActivityItem } from '../utils/events'
import { eventActivityIcon, formatActivityTime, toolActivityIcon } from '../utils/events'
import { useStickToBottom } from '../composables/useStickToBottom'

const props = defineProps<{ items: RunActivityItem[] }>()
const scroller = ref<HTMLElement>()
const { onScroll, onTouchStart, onTouchMove, onTouchEnd, follow } = useStickToBottom(() => scroller.value)

watch(() => props.items, () => { void nextTick(follow) })
onMounted(follow)

function toolResultText(item: ToolActivityItem) {
  if (item.resultPreview) return item.resultPreview
  return item.status === 'completed' ? '(no output)' : ''
}

function toolExpandable(item: ToolActivityItem) {
  if (item.todos?.length) return false
  return Boolean(toolResultText(item) || item.reason)
}

function todoStatusIcon(status: string) {
  if (status === 'completed') return 'i-lucide-circle-check'
  if (status === 'in_progress') return 'i-lucide-loader-circle'
  if (status === 'cancelled') return 'i-lucide-circle-x'
  return 'i-lucide-circle'
}

function todoStatusClass(status: string) {
  if (status === 'in_progress') return 'text-default'
  if (status === 'completed' || status === 'cancelled') return 'text-muted line-through'
  return 'text-muted'
}
</script>

<template>
  <div class="run-activity-stream min-w-0 p-3">
    <UEmpty v-if="!items.length" title="No activity yet" description="Persisted Events appear here, then live updates continue from the last sequence." />
    <div
      v-else
      ref="scroller"
      class="min-w-0 max-h-[min(36rem,calc(100dvh-18rem))] overflow-x-hidden overflow-y-auto overscroll-none touch-pan-y [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
      data-run-activity-scroll
    @scroll="onScroll"
    @touchstart="onTouchStart"
    @touchmove="onTouchMove"
    @touchend="onTouchEnd"
    @touchcancel="onTouchEnd"
  >
    <ol class="min-w-0 space-y-2" data-run-activity>
      <li v-for="item in items" :key="item.id" class="min-w-0 text-sm">
        <div v-if="item.kind === 'thought' || item.kind === 'message'" class="flex items-start gap-2 py-1 text-muted" :data-activity-kind="item.kind">
          <UIcon :name="item.kind === 'thought' ? 'i-lucide-brain' : 'i-lucide-message-circle'" class="mt-0.5 size-4 shrink-0 text-primary" aria-hidden="true" />
          <p class="min-w-0 italic leading-5">{{ item.message }}</p>
        </div>

        <div
          v-else-if="item.kind === 'tool' && item.todos?.length"
          class="py-1"
          data-tool-kind="todo"
          :data-tool-status="item.status"
        >
          <div class="flex min-w-0 items-center gap-2 overflow-hidden">
            <UIcon name="i-lucide-list-todo" class="size-3.5 shrink-0" aria-hidden="true" />
            <span class="min-w-0 truncate font-medium">Todo</span>
            <span class="min-w-0 truncate text-xs text-muted">{{ item.target }}</span>
            <UIcon v-if="item.status === 'running'" name="i-lucide-loader-circle" class="ml-auto size-3 shrink-0 animate-spin text-primary" aria-hidden="true" />
            <span v-else-if="item.status === 'failed'" class="ml-auto text-xs text-error">failed</span>
            <span v-else-if="item.status === 'stopped'" class="ml-auto text-xs text-muted">stopped</span>
          </div>
          <ul class="run-activity-todo-list ml-5 mt-1 space-y-1 p-2">
            <li
              v-for="(todo, index) in item.todos"
              :key="`${item.id}-${index}`"
              class="flex min-w-0 items-start gap-2"
              :data-todo-status="todo.status"
            >
              <UIcon
                :name="todoStatusIcon(todo.status)"
                class="mt-0.5 size-3.5 shrink-0"
                :class="todo.status === 'in_progress' ? 'animate-spin text-primary' : 'text-muted'"
                aria-hidden="true"
              />
              <span class="min-w-0 leading-5" :class="todoStatusClass(todo.status)">{{ todo.content }}</span>
            </li>
          </ul>
          <p v-if="item.status === 'failed' && item.reason" class="ml-5 mt-1 font-mono text-xs text-error">
            <span class="font-medium">error:</span> {{ item.reason }}
          </p>
        </div>

        <div v-else-if="item.kind === 'tool'" class="overflow-hidden py-1" :data-tool-status="item.status">
          <UCollapsible :disabled="!toolExpandable(item)">
            <template #default="{ open }">
              <UButton
                type="button"
                color="neutral"
                variant="ghost"
                block
                class="h-auto justify-start rounded-none px-0 py-0.5"
                :disabled="!toolExpandable(item)"
              >
                <span class="flex min-w-0 flex-1 items-center gap-2 overflow-hidden text-left">
                  <UIcon
                    :name="open ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                    class="size-3.5 shrink-0 text-muted"
                    aria-hidden="true"
                  />
                  <UIcon :name="toolActivityIcon(item.name)" class="size-3.5 shrink-0" aria-hidden="true" />
                  <span class="min-w-0 truncate font-medium">{{ item.label }}</span>
                  <span v-if="item.target" class="min-w-0 truncate font-mono text-xs text-muted">{{ item.target }}</span>
                  <span v-if="item.summary && item.summary !== item.target" class="min-w-0 truncate text-muted">— {{ item.summary }}</span>
                  <UIcon v-if="item.status === 'running'" name="i-lucide-loader-circle" class="ml-auto size-3 shrink-0 animate-spin text-primary" aria-hidden="true" />
                  <span v-else-if="item.status === 'failed'" class="ml-auto text-xs text-error">failed</span>
                  <span v-else-if="item.status === 'stopped'" class="ml-auto text-xs text-muted">stopped</span>
                </span>
              </UButton>
            </template>
            <template #content>
              <p v-if="toolResultText(item)" class="ml-7 mt-1 font-mono text-xs text-muted">
                <span class="font-medium text-default">result:</span> {{ toolResultText(item) }}
              </p>
              <p v-if="item.reason" class="ml-7 mt-1 font-mono text-xs text-error">
                <span class="font-medium">error:</span> {{ item.reason }}
              </p>
            </template>
          </UCollapsible>
        </div>

        <div v-else-if="item.kind === 'question'" class="py-1" :data-question-status="item.status">
          <div class="flex items-start gap-2">
            <UIcon name="i-lucide-circle-help" class="mt-0.5 size-4 shrink-0 text-primary" aria-hidden="true" />
            <div class="min-w-0 flex-1">
              <p class="font-medium leading-5">{{ item.prompt }}</p>
              <div v-if="item.options.length" class="mt-2 text-xs text-muted">
                <p class="flex flex-wrap items-center gap-2">
                  <span class="font-medium text-default">Given options</span>
                  <UBadge v-if="item.choiceKind" :label="item.choiceKind.replaceAll('_', ' ')" color="neutral" variant="subtle" />
                </p>
                <ul class="mt-1 list-disc space-y-1 pl-5">
                  <li v-for="option in item.options" :key="option.id">{{ option.label }}</li>
                </ul>
              </div>
              <p v-if="item.answer" class="mt-2 text-sm">
                <span class="font-medium">answer:</span> {{ item.answer }}
              </p>
              <p v-else-if="item.status === 'cancelled'" class="mt-2 text-xs text-muted">cancelled</p>
              <p v-else-if="item.status === 'open'" class="mt-2 text-xs text-muted">waiting for answer</p>
            </div>
          </div>
        </div>

        <div v-else-if="item.kind === 'event'" class="py-1" :data-activity-kind="item.unknown ? 'unknown' : 'event'">
          <template v-if="item.unknown">
            <strong class="font-medium">{{ item.title }}</strong>
            <p v-if="item.description" class="mt-1 text-muted">{{ item.description }}</p>
            <p class="mt-1 font-mono text-xs text-muted">{{ item.event.type }}</p>
            <pre class="mt-1 overflow-auto">{{ JSON.stringify(item.event.payload, null, 2) }}</pre>
          </template>
          <div v-else class="flex min-w-0 items-center gap-2 overflow-hidden">
            <UIcon name="i-lucide-chevron-right" class="size-3.5 shrink-0 text-muted" aria-hidden="true" />
            <UIcon :name="eventActivityIcon(item.event.type)" class="size-3.5 shrink-0" aria-hidden="true" />
            <span class="min-w-0 truncate font-medium">{{ item.title }}</span>
            <span
              v-if="item.description"
              class="min-w-0 truncate text-xs text-muted"
              :class="{ 'font-mono': item.event.type !== 'decision.recorded' }"
            >{{ item.description }}</span>
          </div>
        </div>
        <time class="ml-7 mt-0.5 block text-xs text-muted" :datetime="item.occurredAt">{{ formatActivityTime(item.occurredAt) }}</time>
      </li>
    </ol>
    </div>
  </div>
</template>
