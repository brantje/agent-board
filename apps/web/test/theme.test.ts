import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { uiStubs } from './ui-stubs'
import { DEFAULT_THEME_ID, THEME_STORAGE_KEY, resolveThemeId, themeMeta, themes, type ThemeId } from '../app/themes'
import { useAppTheme } from '../app/composables/useAppTheme'
import ThemeSelector from '../app/components/ThemeSelector.vue'
import Shell from '../app/components/AppShell.vue'

function source(path: string) {
  return readFileSync(resolve(process.cwd(), path), 'utf8')
}

const themeState = new Map<string, ReturnType<typeof ref>>()

function stubThemeRuntime() {
  vi.stubGlobal('useState', (key: string, init?: () => unknown) => {
    if (!themeState.has(key)) {
      themeState.set(key, ref(typeof init === 'function' ? init() : init))
    }
    return themeState.get(key)
  })
}

beforeEach(() => {
  themeState.clear()
  localStorage.clear()
  document.documentElement.removeAttribute('data-theme')
  document.documentElement.classList.remove('dark', 'light')
  document.documentElement.style.colorScheme = ''
  stubThemeRuntime()
})

afterEach(() => vi.unstubAllGlobals())

describe('theme CSS architecture', () => {
  it('owns shared, dark and light token files and maps them into application CSS', () => {
    const main = source('app/assets/css/main.css')
    const shared = source('app/themes/shared.css')
    const dark = source('app/themes/dark.css')
    const light = source('app/themes/light.css')

    expect(main).toContain('@import "../../themes/shared.css"')
    expect(main).toContain('@import "../../themes/dark.css"')
    expect(main).toContain('@import "../../themes/light.css"')
    expect(main).toContain('--ui-bg: var(--color-bg)')
    expect(main).toContain('--ui-bg-elevated: var(--color-surface)')
    expect(main).toContain('--ui-border: var(--color-divider)')
    expect(main).toContain('--color-accent-600: var(--accent-600)')
    expect(main).toContain('border-radius: 0 !important')
    expect(main).toContain('.issue-identity')
    expect(main).toContain('.issue-priority')
    expect(main).toContain('.issue-last-event')
    expect(main).not.toContain('--color-steel-500')
    expect(main).not.toContain('.dark {')

    expect(shared).toContain('--radius-sm: 0')
    expect(shared).toContain('--font-body')
    expect(shared).toContain('--font-size-kicker')

    expect(dark).toContain(":root")
    expect(dark).toContain("html[data-theme='dark']")
    expect(dark).toContain('--color-bg:')
    expect(dark).toContain('--accent-600:')
    expect(dark).toContain('--board-column-backlog:')
    expect(dark).toContain('--board-column-todo:')
    expect(dark).toContain('--board-column-in-progress:')
    expect(dark).toContain('--board-column-blocked:')
    expect(dark).toContain('--board-column-review:')
    expect(dark).toContain('--board-column-done:')

    expect(light).toContain("html[data-theme='light']")
    expect(light).toContain('--color-bg:')
    expect(light).toContain('--board-column-backlog:')
    expect(light).toContain('--board-column-todo:')
    expect(light).toContain('--board-column-in-progress:')
    expect(light).toContain('--board-column-blocked:')
    expect(light).toContain('--board-column-review:')
    expect(light).toContain('--board-column-done:')
    expect(light).not.toContain(':root,')

    expect(main).toContain('.board-column-backlog')
    expect(main).toContain('.board-column-todo')
    expect(main).toContain('var(--board-column-backlog)')
    expect(main).toContain('var(--board-column-todo)')
    expect(main).toContain('var(--board-column-in-progress)')
    expect(main).toContain('var(--board-column-blocked)')
    expect(main).toContain('var(--board-column-review)')
    expect(main).toContain('var(--board-column-done)')
  })
})

describe('theme registry', () => {
  it('registers Dark as the default and safely resolves registered and unknown ids', () => {
    expect(DEFAULT_THEME_ID).toBe('dark')
    expect(THEME_STORAGE_KEY).toBe('agent-board-theme')
    expect(themes.map(theme => theme.id)).toEqual(['dark', 'light'])
    expect(resolveThemeId('dark')).toBe('dark')
    expect(resolveThemeId('light')).toBe('light')
    expect(resolveThemeId('removed-theme')).toBe('dark')
    expect(themeMeta('light').colorScheme).toBe('light')
    expect(themeMeta('removed-theme' as ThemeId).id).toBe('dark')
  })
})

describe('theme bootstrap and plugin', () => {
  it('applies the stored theme before first paint via head script and a client plugin', () => {
    const plugin = source('app/plugins/theme.client.ts')
    const config = source('nuxt.config.ts')
    expect(plugin).toContain('initializeTheme()')
    expect(config).toContain('themeBootstrap')
    expect(config).toContain('script: [{ innerHTML: themeBootstrap }]')
    expect(config).toContain("const key = 'agent-board-theme'")
    expect(config).toContain("document.documentElement.dataset.theme = value")
    expect(config).toContain("document.documentElement.classList.toggle('dark', value === 'dark')")
  })

  it('initializes the document theme from the client plugin', async () => {
    vi.stubGlobal('defineNuxtPlugin', (setup: () => void) => setup)
    const plugin = (await import('../app/plugins/theme.client')).default as () => void
    plugin()
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})

describe('theme application', () => {
  it('applies, persists and restores the selected theme with an unknown-id fallback', () => {
    const theme = useAppTheme()
    theme.initializeTheme()
    expect(theme.currentTheme.value).toBe('dark')
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(document.documentElement.style.colorScheme).toBe('dark')

    theme.setTheme('light')
    expect(theme.currentTheme.value).toBe('light')
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(document.documentElement.style.colorScheme).toBe('light')

    theme.setTheme('not-registered')
    expect(theme.currentTheme.value).toBe('dark')
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('keeps the applied theme when localStorage persistence fails', () => {
    const theme = useAppTheme()
    theme.initializeTheme()
    const originalSetItem = Storage.prototype.setItem
    Storage.prototype.setItem = () => {
      throw new Error('quota')
    }
    try {
      expect(() => theme.setTheme('light')).not.toThrow()
      expect(theme.currentTheme.value).toBe('light')
      expect(document.documentElement.dataset.theme).toBe('light')
    } finally {
      Storage.prototype.setItem = originalSetItem
    }
  })

  it('falls back to the default theme when localStorage reads fail', () => {
    const originalGetItem = Storage.prototype.getItem
    Storage.prototype.getItem = () => {
      throw new Error('blocked')
    }
    try {
      const theme = useAppTheme()
      expect(() => theme.initializeTheme()).not.toThrow()
      expect(theme.currentTheme.value).toBe('dark')
      expect(document.documentElement.dataset.theme).toBe('dark')
    } finally {
      Storage.prototype.getItem = originalGetItem
    }
  })

  it('restores a valid saved choice and rejects a removed saved theme', () => {
    localStorage.setItem(THEME_STORAGE_KEY, 'light')
    let theme = useAppTheme()
    theme.initializeTheme()
    expect(theme.currentTheme.value).toBe('light')

    themeState.clear()
    stubThemeRuntime()
    localStorage.setItem(THEME_STORAGE_KEY, 'future-theme')
    theme = useAppTheme()
    theme.initializeTheme()
    expect(theme.currentTheme.value).toBe('dark')
  })

  it('does not re-read storage after the first initialize', () => {
    const theme = useAppTheme()
    theme.initializeTheme()
    localStorage.setItem(THEME_STORAGE_KEY, 'light')
    theme.initializeTheme()
    expect(theme.currentTheme.value).toBe('dark')
  })
})

describe('theme selector', () => {
  it('renders the global selector with an accessible label', async () => {
    const wrapper = mount(ThemeSelector, { global: { stubs: uiStubs } })
    await flushPromises()
    expect(wrapper.find('[data-testid="theme-selector"]').exists()).toBe(true)
    expect(wrapper.find('[aria-label="Application theme"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Dark')
    expect(wrapper.text()).toContain('Light')
  })

  it('changes theme from the selector', async () => {
    const wrapper = mount(ThemeSelector, { global: { stubs: uiStubs } })
    await flushPromises()
    await wrapper.get('[data-testid="theme-selector"]').setValue('light')
    expect(document.documentElement.dataset.theme).toBe('light')
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('light')
  })
})

describe('theme defaults', () => {
  it('centralizes square surfaces, compact controls and theme-owned accent colors', async () => {
    vi.stubGlobal('defineAppConfig', (value: unknown) => value)
    const { default: config } = await import('../app/app.config')
    expect(config.ui.card.slots.root).toContain('rounded-none')
    expect(config.ui.modal.slots.content).toContain('rounded-none')
    expect(config.ui.button.defaultVariants.size).toBe('sm')
    expect(config.ui.colors.primary).toBe('accent')
    expect(config.ui.colors.neutral).toBe('neutral')
  })

  it('places the theme selector in the shell footer instead of a parallel color-mode control', () => {
    vi.stubGlobal('useRoute', () => ({ params: {} }))
    const sidebar = {
      template: '<aside><div data-testid="sidebar-body"><slot name="default" :collapsed="false" /></div><div data-testid="sidebar-footer"><slot name="footer" :collapsed="false" /></div></aside>'
    }
    const wrapper = mount(Shell, {
      global: {
        stubs: {
          UDashboardGroup: { template: '<div><slot /></div>' },
          UDashboardSidebar: sidebar,
          UNavigationMenu: { props: ['items'], template: '<nav />' },
          USeparator: { template: '<hr />' },
          UButton: { template: '<a />' },
          ThemeSelector: { template: '<div data-testid="theme-selector" aria-label="Application theme">Theme</div>' }
        }
      }
    })
    expect(wrapper.find('[data-testid="sidebar-footer"] [data-testid="theme-selector"]').exists()).toBe(true)
    expect(wrapper.find('[aria-label="Color mode"]').exists()).toBe(false)
  })
})
