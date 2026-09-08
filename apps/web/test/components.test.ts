import { mount, flushPromises } from '@vue/test-utils'
import { reactive, defineComponent, h } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from '../app/app.vue'
import Shell from '../app/components/AppShell.vue'
import Page from '../app/components/PageFrame.vue'
import Index from '../app/pages/index.vue'
import AsyncState from '../app/components/AsyncState.vue'
import { navigation } from '../app/utils/navigation'
import { useResource } from '../app/composables/useResource'

const pass = { template: '<div><slot name="header" :collapsed="false" /><slot name="default" :collapsed="false" /><slot name="footer" /><slot name="body" /><slot name="leading" /></div>' }
const stubs = Object.fromEntries(['UApp','UDashboardGroup','UDashboardSidebar','UDashboardPanel','UDashboardNavbar','UDashboardToolbar','AppShell','PageFrame','USeparator','ThemeSelector','UDashboardSidebarCollapse','NuxtRouteAnnouncer','USkeleton'].map(name => [name, pass]))
const menu = { props: ['items'], template: '<nav><a v-for="item in items" :href="item.to">{{ item.label }}</a></nav>' }
const alert = { props: ['title','description','actions'], template: '<div role="alert">{{ title }} {{ description }}<button @click="actions[0].onClick()">Retry</button></div>' }
const empty = { props: ['title','description'], template: '<div>{{ title }} {{ description }}</div>' }
const global = { stubs: { ...stubs, UNavigationMenu: menu, UButton: { props:['label'], template:'<a>{{ label }}</a>' }, UEmpty: empty, UAlert: alert } }
afterEach(() => vi.unstubAllGlobals())
describe('application foundation', () => {
  it('renders accessible route content and skip navigation', () => {
    const wrapper = mount(App, { global: { stubs: { ...global.stubs, NuxtPage: { template:'<p>Active page</p>' } } } })
    expect(wrapper.text()).toContain('Active page')
    expect(wrapper.get('a').attributes('href')).toBe('#main-content')
  })
  it('renders only canonical navigation, adding project routes in project context', async () => {
    const route = reactive({ params: {} as Record<string,string> })
    vi.stubGlobal('useRoute', () => route)
    const wrapper = mount(Shell, { global })
    expect(wrapper.findAll('nav').length).toBe(2)
    expect(wrapper.find('[data-testid="settings-main-nav"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Plugins')
    route.params.projectID = 'project-1'
    await flushPromises()
    expect(wrapper.find('a[href="/projects/project-1/board"]').exists()).toBe(true)
    delete route.params.projectID
    await flushPromises()
    expect(wrapper.find('a[href="/projects/project-1/board"]').exists()).toBe(false)
    expect(navigation().global.map(item => item.label)).toEqual(['Projects','Agents','Runs','Inbox'])
    expect(navigation().settings.map(item => item.label)).toEqual(['Settings'])
  })
  it('pins Settings above the sidebar footer divider', () => {
    vi.stubGlobal('useRoute', () => ({ params: {} }))
    const sidebar = {
      template: '<aside><div data-testid="sidebar-body"><slot name="default" :collapsed="false" /></div><div data-testid="sidebar-footer"><slot name="footer" :collapsed="false" /></div></aside>'
    }
    const wrapper = mount(Shell, {
      global: {
        stubs: {
          ...global.stubs,
          UDashboardSidebar: sidebar,
          ThemeSelector: { template: '<div data-testid="theme-selector" aria-label="Application theme">Theme</div>' }
        }
      }
    })
    const settings = wrapper.get('[data-testid="sidebar-body"] [data-testid="settings-main-nav"]')
    expect(settings.classes()).toContain('mt-auto')
    expect(wrapper.find('[data-testid="sidebar-footer"] [data-testid="settings-main-nav"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="sidebar-footer"]').text()).toContain('Self-hosted')
    expect(wrapper.find('[data-testid="sidebar-footer"] [data-testid="theme-selector"]').exists()).toBe(true)
  })
  it('provides a viewport page and compact action slots', () => {
    const wrapper = mount(Page, { props: { title: 'Board', description: 'Project context' }, slots: { actions: '<span>New issue</span>', default: '<p>Work</p>' }, global })
    expect(wrapper.get('main').attributes('id')).toBe('main-content')
    expect(wrapper.get('main').classes()).toContain('w-full')
    expect(wrapper.get('#main-content').element.parentElement?.className).toContain('w-full')
    expect(wrapper.text()).toContain('Project context')
    expect(wrapper.text()).toContain('New issue')
    expect(mount(Page, { props: { title:'Empty' }, global }).text()).not.toContain('Project context')
    expect(mount(Index, { global }).text()).toContain('Your work starts with a project')
  })
  it('prioritizes loading/error/empty states and exposes retry', async () => {
    const wrapper = mount(AsyncState, { props: { pending:true }, slots: { default:'Loaded data' }, global })
    expect(wrapper.get('[role=status]').attributes('aria-label')).toBe('Loading')
    await wrapper.setProps({ pending:false, error: new Error('Permission denied') })
    expect(wrapper.get('[role=alert]').text()).toContain('Permission denied')
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('retry')).toHaveLength(1)
    await wrapper.setProps({ error:undefined, empty:true })
    expect(wrapper.text()).toContain('Nothing here yet')
    expect(wrapper.text()).not.toContain('New work will appear here when it is created.')
    await wrapper.setProps({ emptyTitle:'No projects', emptyDescription:'Create a Project with a local repository to open a board.' })
    expect(wrapper.text()).toContain('No projects')
    expect(wrapper.text()).toContain('Create a Project with a local repository to open a board.')
    await wrapper.setProps({ empty:false })
    expect(wrapper.text()).toBe('Loaded data')
  })
})
describe('durable API resource lifecycle', () => {
  it('loads on mount, discards stale requests across scope changes and retries failures', async () => {
    const resolvers: ((response: Response) => void)[] = []
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>(resolve => resolvers.push(resolve))))
    const path = reactive({ value:'/api/projects/a' })
    let resource!: ReturnType<typeof useResource<{name:string}>>
    const wrapper = mount(defineComponent({ setup() { resource = useResource(() => path.value); return () => h('div', resource.data.value?.name) } }))
    path.value = '/api/projects/b'; await flushPromises()
    resolvers[0]!(new Response('{"name":"A"}')); await flushPromises()
    expect(resource.data.value).toBeUndefined()
    resolvers[1]!(new Response('{"name":"B"}')); await flushPromises()
    expect(wrapper.text()).toBe('B')
    const refresh = resource.refresh()
    resolvers[2]!(new Response('denied', {status:403})); await refresh
    expect(resource.error.value?.status).toBe(403)
    expect(resource.data.value).toBeUndefined()
    const retry = resource.refresh(); resolvers[3]!(new Response('{"name":"B2"}')); await retry
    expect(resource.error.value).toBeUndefined()
    expect(resource.data.value?.name).toBe('B2')
    const last = resource.refresh(); wrapper.unmount(); resolvers[4]!(new Response('bad', {status:500})); await last
    expect(resource.data.value?.name).toBe('B2')
  })
})
