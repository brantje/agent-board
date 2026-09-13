<script setup lang="ts">
import { computed } from 'vue'
import type { IdentityKind } from '../utils/identity'
import { identityDisplayName, identityInitial, userIdentityColor } from '../utils/identity'

const props = withDefaults(defineProps<{
  kind: IdentityKind
  name: string
  size?: '3xs' | '2xs' | 'xs' | 'sm' | 'md' | 'lg' | 'xl' | '2xl' | '3xl'
}>(), {
  size: '2xs'
})

const label = computed(() => identityDisplayName(props.name))
const color = computed(() => props.kind === 'agent' ? 'neutral' : userIdentityColor(props.name))
</script>

<template>
  <UAvatar
    v-if="kind === 'agent'"
    :alt="label"
    :aria-label="label"
    icon="i-lucide-bot"
    :color="color"
    :size="size"
    class="issue-identity"
  />
  <UAvatar
    v-else
    :alt="label"
    :aria-label="label"
    :text="identityInitial(name)"
    :color="color"
    :size="size"
    class="issue-identity"
  />
</template>
