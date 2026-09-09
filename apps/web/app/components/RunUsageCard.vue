<script setup lang="ts">
import { computed } from 'vue'
import type { RunUsageEvidence } from '../types/api'

const props = defineProps<{ usage?: RunUsageEvidence | null }>()

const contextPercent = computed(() => {
  const usage = props.usage
  if (!usage?.contextLimitTokens || usage.contextLimitTokens <= 0) return null
  return Math.min(100, Math.max(0, (usage.contextTokens / usage.contextLimitTokens) * 100))
})

function compactNumber(value: number) {
  const absolute = Math.abs(value)
  if (absolute >= 1_000_000) return `${trimDecimal(value / 1_000_000, absolute < 10_000_000)}M`
  if (absolute >= 1_000) return `${trimDecimal(value / 1_000, absolute < 100_000)}K`
  return String(Math.round(value))
}

function trimDecimal(value: number, keepDecimal: boolean) {
  return value.toFixed(keepDecimal ? 1 : 0).replace(/\.0$/, '')
}

function exactNumber(value: number) {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(value)
}

function waitLabel(value: number | null | undefined) {
  if (value == null) return '—'
  if (value < 1000) return `${Math.round(value)}ms`
  return `${(value / 1000).toFixed(1).replace(/\.0$/, '')}s`
}

function speedLabel(value: number | null | undefined) {
  return value == null ? '—' : value.toFixed(1)
}
</script>

<template>
  <UCard data-run-usage>
    <h2 class="section-label mb-3">Usage</h2>
    <div v-if="usage" class="space-y-4 text-sm">
      <div>
        <div class="mb-2 flex items-end justify-between gap-3">
          <span class="text-muted">Context</span>
          <UTooltip :text="`${exactNumber(usage.contextTokens)} / ${usage.contextLimitTokens ? exactNumber(usage.contextLimitTokens) : 'unknown'} tokens`">
            <span class="font-mono tabular-nums">
              {{ compactNumber(usage.contextTokens) }} / {{ usage.contextLimitTokens ? compactNumber(usage.contextLimitTokens) : '—' }}
            </span>
          </UTooltip>
        </div>
        <UProgress v-if="contextPercent != null" :model-value="contextPercent" :max="100" />
        <p v-if="contextPercent != null" class="mt-1 text-right text-xs text-muted">{{ Math.round(contextPercent) }}%</p>
      </div>

      <dl class="space-y-2">
        <div class="flex items-center justify-between gap-3">
          <dt class="text-muted">Avg. waiting</dt>
          <dd class="font-mono tabular-nums">{{ waitLabel(usage.averageWaitMs) }}</dd>
        </div>
        <div class="flex items-center justify-between gap-3">
          <dt class="text-muted">Input</dt>
          <dd>
            <UTooltip :text="`${exactNumber(usage.inputTokens)} tokens`">
              <span class="font-mono tabular-nums">{{ compactNumber(usage.inputTokens) }}</span>
            </UTooltip>
          </dd>
        </div>
        <div class="flex items-center justify-between gap-3">
          <dt class="text-muted">Output</dt>
          <dd>
            <UTooltip :text="`${exactNumber(usage.outputTokens)} tokens`">
              <span class="font-mono tabular-nums">{{ compactNumber(usage.outputTokens) }}</span>
            </UTooltip>
          </dd>
        </div>
        <div class="flex items-center justify-between gap-3">
          <dt class="text-muted">Cache read</dt>
          <dd>
            <UTooltip :text="`${exactNumber(usage.cacheReadTokens)} tokens`">
              <span class="font-mono tabular-nums">{{ compactNumber(usage.cacheReadTokens) }}</span>
            </UTooltip>
          </dd>
        </div>
        <div class="flex items-center justify-between gap-3">
          <dt class="text-muted">Tokens/sec</dt>
          <dd class="font-mono tabular-nums">{{ speedLabel(usage.tokensPerSecond) }}</dd>
        </div>
      </dl>
    </div>
    <p v-else class="text-sm text-muted">No model usage recorded yet.</p>
  </UCard>
</template>
