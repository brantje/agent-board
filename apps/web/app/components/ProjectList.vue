<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Project } from '../types/api'
import { apiPath } from '../utils/api'
import { useResource } from '../composables/useResource'
import { useRefresh } from '../composables/useRefresh'
import ProjectEditor from './ProjectEditor.vue'

const { data, pending, error, refresh } = useResource<Project[]>(() => apiPath('projects'))
const open = ref(false)
const selected = ref<Project>()
const saved = ref(false)

const visible = computed(() => data.value ?? [])

function edit(item?: Project) {
  selected.value = item
  open.value = true
}

function openBoard(projectId: string) {
  return navigateTo(`/projects/${projectId}/board`)
}

async function savedProject() {
  open.value = false
  saved.value = true
  await refresh()
}

useRefresh(refresh)
</script>

<template>
  <PageFrame title="Projects" description="Projects · backend-managed repository contexts">
    <template #actions>
      <UButton label="New project" icon="i-lucide-plus" @click="edit()" />
    </template>

    <UAlert v-if="saved" title="Saved" color="success" class="mb-4" />

    <AsyncState
      :pending="pending"
      :error="error"
      :empty="!visible.length"
      empty-title="No projects yet" 
      empty-description="Create a Project with a local repository, default branch, and Issue prefix to open a board."
      @retry="refresh"
    >
      <div class="grid w-full gap-3">
        <UCard v-for="item in visible" :key="item.id">
          <div class="cursor-pointer" role="link" tabindex="0" @click="openBoard(item.id)" @keydown.enter.prevent="openBoard(item.id)" @keydown.space.prevent="openBoard(item.id)">
            <div class="flex flex-wrap items-center gap-3">
              <div class="min-w-0 flex-1">
                <h2 class="font-medium text-highlighted break-words">{{ item.name }}</h2>
                <p class="text-xs text-muted">Backend-managed repository context</p>
              </div>
              <div class="flex flex-wrap items-center gap-3" @click.stop>
                <UButton label="Open board" :to="`/projects/${item.id}/board`" variant="outline" />
                <UButton label="Edit" color="neutral" variant="outline" @click="edit(item)" />
              </div>
            </div>
            <p class="mt-2 text-sm font-mono break-all">
              {{ item.issuePrefix }} · {{ item.repositoryPath }} · {{ item.defaultBranch }}
            </p>
          </div>
        </UCard>
      </div>
    </AsyncState>

    <UModal
      v-model:open="open"
      :title="`${selected ? 'Edit' : 'New'} project`"
      description="Changes are saved to Agent Board."
    >
      <template #body>
        <ProjectEditor :project="selected" @saved="savedProject" @cancel="open = false" />
      </template>
    </UModal>
  </PageFrame>
</template>
