<script setup lang="ts">
import { computed } from 'vue'
import type { NavigationMenuItem } from '@nuxt/ui'
import { settingsNavigation } from '../utils/settings-navigation'

const props = defineProps<{ projectId?: string }>()
const route = useRoute()

const navigationMenuUi = {
  label: 'section-label mb-2 px-2',
  link: 'rounded-xs',
  separator: 'my-2'
}

function itemPath(item: NavigationMenuItem) {
  return typeof item.to === 'string' ? item.to : undefined
}

function active(to: string, exact = false) {
  return exact ? route.path === to : route.path === to || route.path.startsWith(`${to}/`)
}

function withActiveState(item: NavigationMenuItem): NavigationMenuItem {
  const to = itemPath(item)
  if (!to) return item
  const isActive = active(to, item.exact === true)
  return {
    ...item,
    active: isActive,
    ...(isActive ? { 'aria-current': 'page' } : {})
  }
}

const desktopItems = computed(() =>
  settingsNavigation(props.projectId).map(group => group.map(withActiveState))
)
const mobileItems = computed(() => desktopItems.value)

const currentItem = computed(() => {
  for (const group of desktopItems.value) {
    for (const item of group) {
      const to = itemPath(item)
      if (to && active(to, item.exact === true)) return item
    }
  }
  return desktopItems.value[0]?.[1]
})
</script>

<template>
  <aside
    class="w-full shrink-0 border-b border-default pb-4 lg:w-[216px] lg:border-r lg:border-b-0 lg:pr-4 lg:pb-0"
    data-testid="settings-secondary-nav"
  >
    <UCollapsible class="space-y-2 lg:hidden" data-testid="settings-mobile-nav">
      <template #default="{ open }">
        <UButton type="button" color="neutral" variant="outline" class="min-h-11 w-full rounded-xs">
          <span class="flex w-full items-center justify-between gap-3 text-left">
            <span class="min-w-0 text-sm font-semibold leading-5">Settings · {{ currentItem?.label }}</span>
            <UIcon
              :name="open ? 'i-lucide-chevron-up' : 'i-lucide-chevron-down'"
              class="size-4 shrink-0 text-muted"
              aria-hidden="true"
            />
          </span>
        </UButton>
      </template>
      <template #content>
        <UNavigationMenu
          :items="mobileItems"
          orientation="vertical"
          class="w-full border border-default"
          :ui="navigationMenuUi"
          aria-label="Settings sections"
        />
      </template>
    </UCollapsible>
    <div class="hidden lg:block" data-testid="settings-desktop-nav">
      <UNavigationMenu
        :items="desktopItems"
        orientation="vertical"
        class="w-full"
        :ui="navigationMenuUi"
        aria-label="Settings"
      />
    </div>
  </aside>
</template>
