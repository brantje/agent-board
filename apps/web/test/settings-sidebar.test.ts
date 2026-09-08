import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Component } from 'vue'
import SettingsSidebar from '../app/components/SettingsSidebar.vue'
import SettingsShell from '../app/components/SettingsShell.vue'
import { settingsNavigation } from '../app/utils/settings-navigation'
import { uiStubs } from './ui-stubs'

const menuStub = {
  props: ['items'],
  template: '<nav data-testid="settings-menu"><template v-for="group in items"><a v-for="item in group" :key="item.label" :href="item.to">{{ item.label }}</a></template></nav>'
}

const collapsibleStub = {
  template: '<div data-testid="settings-mobile-nav"><slot /><slot name="content" /></div>'
}

const global = {
  stubs: {
    ...uiStubs,
    UNavigationMenu: menuStub,
    UCollapsible: collapsibleStub,
    UButton: { props: ['label'], template: '<button>{{ label }}</button>' },
    UIcon: { template: '<span />' },
    PageFrame: {
      props: ['title', 'description'],
      template: '<div data-pageframe><h1>{{ title }}</h1><slot name="actions" /><slot /></div>'
    },
    ConfigManager: {
      props: ['kind', 'projectId', 'resourceId'],
      template: '<div data-kind="{{ kind }}" />'
    }
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('settings navigation helper', () => {
  it('builds global settings links with providers always global', () => {
    const groups = settingsNavigation()
    const links = groups.flat().filter(item => item.to)
    expect(links.find(item => item.label === 'Overview')?.to).toBe('/settings')
    expect(links.find(item => item.label === 'Providers')?.to).toBe('/settings/providers')
    expect(links.find(item => item.label === 'Model Profiles')?.to).toBe('/settings/model-profiles')
    expect(links.find(item => item.label === 'Agents')?.to).toBe('/agents')
    expect(links.find(item => item.label === 'Executor Profiles')).toBeUndefined()
  })

  it('builds project-scoped settings links while keeping providers global', () => {
    const groups = settingsNavigation('p')
    const links = groups.flat().filter(item => item.to)
    expect(links.find(item => item.label === 'Project')?.to).toBe('/projects/p/settings')
    expect(links.find(item => item.label === 'Providers')?.to).toBe('/settings/providers')
    expect(links.find(item => item.label === 'Runtimes')?.to).toBe('/projects/p/settings/runtimes')
    expect(links.find(item => item.label === 'Agents')?.to).toBe('/projects/p/agents')
    expect(links.find(item => item.label === 'Executor Profiles')).toBeUndefined()
  })
})

describe('settings sidebar shell', () => {
  it('renders grouped secondary navigation without a back link', () => {
    vi.stubGlobal('useRoute', () => ({ path: '/settings/providers', params: {} }))
    const wrapper = mount(SettingsSidebar, { global })
    expect(wrapper.get('[data-testid="settings-secondary-nav"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/settings/providers"]').text()).toBe('Providers')
    expect(wrapper.text()).not.toMatch(/back to/i)
  })

  it('wraps settings pages so ConfigManager renders inside the shell', () => {
    vi.stubGlobal('useRoute', () => ({ path: '/settings/providers', params: {} }))
    const wrapper = mount(SettingsShell, {
      slots: { default: '<div data-child>content</div>' },
      global
    })
    expect(wrapper.get('[data-testid="settings-shell"]').exists()).toBe(true)
    expect(wrapper.get('[data-child]').exists()).toBe(true)
  })

  it('fills remaining dashboard width instead of shrinking to content', () => {
    vi.stubGlobal('useRoute', () => ({ path: '/settings/model-profiles', params: {} }))
    const wrapper = mount(SettingsShell, {
      slots: { default: '<div data-child>content</div>' },
      global
    })
    const classes = wrapper.get('[data-testid="settings-shell"]').classes()
    expect(classes).toContain('min-w-0')
    expect(classes).toContain('flex-1')
    expect(classes).toContain('w-full')
    expect(classes.join(' ')).not.toMatch(/max-w-|mx-auto/)
  })
})

describe('settings route wiring', () => {
  const pages = import.meta.glob('../app/pages/**/*.vue', { eager: true, import: 'default' }) as Record<string, Component>

  it('wraps global and project settings pages with SettingsShell', () => {
    vi.stubGlobal('useRoute', () => ({ params: { projectID: 'project-a' } }))
    const settingsPages = Object.entries(pages).filter(([path]) =>
      path.includes('/settings/') || path.endsWith('/pages/agents.vue') || path.endsWith('/agents.vue')
    )
    for (const [path, page] of settingsPages) {
      const wrapper = mount(page, {
        global: {
          stubs: {
            ...global.stubs,
            SettingsShell: { template: '<div data-settings-shell><slot /></div>' },
            ConfigManager: {
              props: ['kind', 'projectId', 'resourceId'],
              template: '<div :data-kind="kind" :data-project="projectId" :data-resource="resourceId" />'
            }
          }
        }
      })
      expect(wrapper.find('[data-settings-shell]').exists()).toBe(true)
      if (path.endsWith('/settings/index.vue') && !path.includes('[projectID]')) continue
      if (path.endsWith('/pages/agents.vue')) {
        expect(wrapper.get('[data-kind]').attributes('data-kind')).toBe('agents')
        continue
      }
      const manager = wrapper.get('[data-kind]')
      if (path.includes('[projectID]')) {
        expect(manager.attributes(path.endsWith('/settings/index.vue') ? 'data-resource' : 'data-project')).toBe('project-a')
      } else {
        expect(manager.attributes('data-project')).toBeUndefined()
      }
    }
  })
})
