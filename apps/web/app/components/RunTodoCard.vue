<script setup lang="ts">
import type { TodoActivityItem } from '../utils/events'
import { todoStatusClass, todoStatusIcon } from '../utils/todos'

defineProps<{ todos: TodoActivityItem[] }>()
</script>

<template>
  <UCard data-run-todo>
    <h2 class="section-label mb-3">Todo</h2>
    <ul class="run-activity-todo-list space-y-1 p-2">
      <li
        v-for="(todo, index) in todos"
        :key="`${todo.content}-${index}`"
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
  </UCard>
</template>
