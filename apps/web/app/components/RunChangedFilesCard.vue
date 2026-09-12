<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { RunEvidence } from '../types/api'
import {
  buildCandidateFileItems,
  buildCandidateFileTree,
  flattenCandidateFileTree,
  loadReviewFileStats,
  sumCandidateFileStats,
  type CandidateFileStats,
  type CandidateFileTreeRow
} from '../utils/candidate-files'

const props = defineProps<{ projectId: string; evidence?: RunEvidence }>()

const changedFiles = computed(() => buildCandidateFileItems(props.evidence?.fileChanges || []))
const fileStats = ref<Map<string, CandidateFileStats>>(new Map())
const collapsedFolders = ref<Set<string>>(new Set())

watch(() => props.evidence, async (value) => {
  fileStats.value = new Map()
  collapsedFolders.value = new Set()
  if (!value) return
  try {
    fileStats.value = await loadReviewFileStats(props.projectId, value)
  } catch {
    fileStats.value = new Map()
  }
}, { immediate: true })

const fileTree = computed(() => buildCandidateFileTree(changedFiles.value, fileStats.value))
const treeRows = computed(() => flattenCandidateFileTree(fileTree.value, collapsedFolders.value))
const totalStats = computed(() => sumCandidateFileStats(fileStats.value))

function toggleFolder(path: string) {
  const next = new Set(collapsedFolders.value)
  if (next.has(path)) next.delete(path)
  else next.add(path)
  collapsedFolders.value = next
}

function fileLabel(row: CandidateFileTreeRow & { kind: 'file' }) {
  return row.oldPath ? `${row.oldPath} → ${row.name}` : row.name
}

function rowPadding(depth: number) {
  return { paddingLeft: `${depth * 0.75 + 0.5}rem` }
}
</script>

<template>
  <UCard data-changed-files>
    <h2 class="section-label mb-3">Changed files {{ changedFiles.length }}</h2>

    <div
      v-if="changedFiles.length"
      data-changed-files-totals
      class="mb-2 flex items-center justify-between border border-default bg-default/40 px-3 py-2 text-xs font-mono"
    >
      <span class="text-muted">{{ changedFiles.length }} {{ changedFiles.length === 1 ? 'file' : 'files' }}</span>
      <span class="flex items-center gap-2">
        <span v-if="totalStats.added > 0" class="text-success">+{{ totalStats.added }}</span>
        <span v-if="totalStats.removed > 0" class="text-error">-{{ totalStats.removed }}</span>
        <span v-if="totalStats.added === 0 && totalStats.removed === 0" class="text-muted">No line stats</span>
      </span>
    </div>

    <div
      v-if="changedFiles.length"
      data-changed-files-scroll
      class="max-h-72 overflow-y-auto border border-default"
    >
      <ul data-changed-files-tree class="text-sm">
        <li
          v-for="row in treeRows"
          :key="`${row.kind}:${row.path}`"
          class="border-b border-default last:border-b-0"
        >
          <button
            v-if="row.kind === 'folder'"
            type="button"
            :data-folder-path="row.path"
            class="flex w-full items-start justify-between gap-3 px-2 py-1.5 text-left hover:bg-default/60 focus-visible:outline-2 focus-visible:outline-primary"
            :style="rowPadding(row.depth)"
            @click="toggleFolder(row.path)"
          >
            <span class="flex min-w-0 items-center gap-1.5">
              <UIcon
                :name="row.expanded ? 'i-lucide-chevron-down' : 'i-lucide-chevron-right'"
                class="size-3.5 shrink-0 text-muted"
              />
              <UIcon name="i-lucide-folder" class="size-3.5 shrink-0 text-muted" />
              <span class="truncate font-mono">{{ row.name }}</span>
            </span>
            <span v-if="row.stats.added > 0 || row.stats.removed > 0" class="shrink-0 font-mono text-xs">
              <span v-if="row.stats.added > 0" class="text-success">+{{ row.stats.added }}</span>
              <span v-if="row.stats.removed > 0" class="text-error">-{{ row.stats.removed }}</span>
            </span>
          </button>

          <div
            v-else
            class="flex items-start justify-between gap-3 px-2 py-1.5"
            :style="rowPadding(row.depth)"
          >
            <span class="min-w-0 break-all pl-5 font-mono">{{ fileLabel(row) }}</span>
            <span v-if="row.stats && (row.stats.added > 0 || row.stats.removed > 0)" class="shrink-0 font-mono text-xs">
              <span v-if="row.stats.added > 0" class="text-success">+{{ row.stats.added }}</span>
              <span v-if="row.stats.removed > 0" class="text-error">-{{ row.stats.removed }}</span>
            </span>
          </div>
        </li>
      </ul>
    </div>

    <p v-else class="text-sm text-muted">No changed files recorded for this candidate.</p>
  </UCard>
</template>
