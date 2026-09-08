<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { themes, type ThemeId } from '../themes'
import { useAppTheme } from '../composables/useAppTheme'

const { currentTheme, initializeTheme, setTheme } = useAppTheme()
const selected = computed({
  get: () => currentTheme.value,
  set: (value: ThemeId) => setTheme(value)
})

onMounted(initializeTheme)

const items = themes.map(theme => ({ label: theme.label, value: theme.id }))
</script>

<template>
  <USelect
    v-model="selected"
    data-testid="theme-selector"
    aria-label="Application theme"
    :items="items"
    value-key="value"
    label-key="label"
    size="xs"
    class="w-24"
  />
</template>
