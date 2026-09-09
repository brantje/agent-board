<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { Question, QuestionAnswer, QuestionAnswerResponse } from '../types/api'
import { apiPath, apiQuery, apiRequest } from '../utils/api'
import { refetchTargets } from '../utils/events'
import { useResource } from '../composables/useResource'
import { useProjectEvents } from '../composables/useProjectEvents'

const props = defineProps<{ projectId: string; issueId?: string; runId?: string }>()
const path = computed(() => apiQuery(apiPath('questions', props.projectId), {
  issueId: props.issueId,
  runId: props.runId,
  status: 'OPEN'
}))
const { data, pending, error, refresh } = useResource<Question[]>(path)
const answering = ref('')
const answerError = ref<Error>()
const drafts = reactive<Record<string, { optionId: string; optionIds: string[]; text: string; custom: boolean }>>({})

function draftFor(question: Question) {
  if (!drafts[question.id]) {
    drafts[question.id] = {
      optionId: question.kind === 'SINGLE_CHOICE' && question.recommendation ? question.recommendation : '',
      optionIds: [],
      text: '',
      custom: false
    }
  }
  return drafts[question.id]!
}

function recommendedLabel(question: Question) {
  if (!question.recommendation) return undefined
  return question.options.find(option => option.id === question.recommendation)?.label || question.recommendation
}

function toggleOption(question: Question, optionId: string, checked: boolean) {
  const draft = draftFor(question)
  if (checked) draft.optionIds = [...new Set([...draft.optionIds, optionId])]
  else draft.optionIds = draft.optionIds.filter(id => id !== optionId)
}

function answerBody(question: Question): QuestionAnswer | undefined {
  const draft = draftFor(question)
  if (question.kind === 'TEXT') {
    const text = draft.text.trim()
    return text ? { kind: 'TEXT', text } : undefined
  }
  if (draft.custom && question.custom) {
    const text = draft.text.trim()
    return text ? { kind: question.kind, text } : undefined
  }
  if (question.kind === 'SINGLE_CHOICE' && draft.optionId) return { kind: 'SINGLE_CHOICE', optionIds: [draft.optionId] }
  if (question.kind === 'MULTI_CHOICE' && draft.optionIds.length) return { kind: 'MULTI_CHOICE', optionIds: draft.optionIds }
  return undefined
}

const openQuestions = computed(() => (data.value || []).filter(question => question.status === 'OPEN'))

useProjectEvents(() => props.projectId, async event => {
  if (!refetchTargets(event.type).questions) return
  if (props.runId && event.runId && event.runId !== props.runId) return
  await refresh()
})

async function answer(question: Question) {
  const body = answerBody(question)
  if (!body || answering.value) return
  answering.value = question.id
  answerError.value = undefined
  try {
    const result = await apiRequest<QuestionAnswerResponse>(`${apiPath('questions', props.projectId, question.id)}/answer`, {
      method: 'POST',
      body
    })
    data.value = (data.value || []).map(item => item.id === result.question.id ? result.question : item)
  } catch (failure) {
    answerError.value = failure as Error
  } finally {
    answering.value = ''
  }
}

defineExpose({ refresh })
</script>

<template>
  <UCard>
    <h2 class="section-label mb-3">Questions</h2>
    <AsyncState :pending="pending" :error="error" @retry="refresh">
      <UAlert v-if="answerError" title="Unable to answer" :description="answerError.message" color="error" class="mb-4" />
      <AsyncState :empty="!openQuestions.length" empty-title="No open questions" empty-description="When a Run needs a human decision, the Agent's question will appear here.">
      <div class="space-y-4">
        <UCard v-for="question in openQuestions" :key="question.id">
          <div class="mb-3 flex flex-wrap items-center gap-2">
            <UBadge v-if="question.blocking" label="Blocking" color="warning" variant="subtle" />
            <UBadge :label="question.kind.replaceAll('_', ' ')" color="neutral" variant="subtle" />
          </div>
          <p class="mb-3 whitespace-pre-wrap break-words">{{ question.prompt }}</p>
          <UAlert v-if="recommendedLabel(question)" title="Recommended" :description="`Recommended: ${recommendedLabel(question)}`" color="neutral" class="mb-3" />
          <UForm :state="draftFor(question)" class="space-y-3" @submit="answer(question)">
            <UFormField v-if="question.kind === 'TEXT'" label="Answer" name="text">
              <UTextarea v-model="draftFor(question).text" class="w-full" :disabled="!!answering" />
            </UFormField>
            <UFormField v-else-if="question.kind === 'SINGLE_CHOICE' && !draftFor(question).custom" label="Choice" name="option">
              <URadioGroup v-model="draftFor(question).optionId" :items="question.options.map(option => ({ label: option.label, value: option.id }))" :disabled="!!answering" />
            </UFormField>
            <div v-else-if="question.kind === 'MULTI_CHOICE' && !draftFor(question).custom" class="space-y-2">
              <label v-for="option in question.options" :key="option.id" class="flex items-center gap-2 text-sm">
                <UCheckbox :model-value="draftFor(question).optionIds.includes(option.id)" :disabled="!!answering" @update:model-value="toggleOption(question, option.id, $event === true)" />
                <span>{{ option.label }}</span>
              </label>
            </div>
            <UFormField v-if="question.custom" :label="question.kind === 'TEXT' ? undefined : 'Alternate answer'" name="custom">
              <UCheckbox v-if="question.kind !== 'TEXT'" :model-value="draftFor(question).custom" :disabled="!!answering" @update:model-value="draftFor(question).custom = $event === true" />
              <UTextarea v-if="question.kind === 'TEXT' || draftFor(question).custom" v-model="draftFor(question).text" class="w-full" :disabled="!!answering" />
            </UFormField>
            <UButton label="Answer" type="submit" :loading="answering === question.id" :disabled="!answerBody(question) || !!answering" />
          </UForm>
        </UCard>
      </div>
      </AsyncState>
    </AsyncState>
  </UCard>
</template>
