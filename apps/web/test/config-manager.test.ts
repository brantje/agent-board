import { mount, flushPromises } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ConfigManager from '../app/components/ConfigManager.vue'
import { definitions, type ConfigKind } from '../app/utils/configuration'
import { uiStubs } from './ui-stubs'
const global = {stubs:uiStubs}
const button = (w:ReturnType<typeof mount>,label:string) => w.findAll('button').find(b=>b.text()===label)!
afterEach(()=>vi.unstubAllGlobals())
describe('configuration screens',()=>{
  it.each(Object.keys(definitions) as ConfigKind[])('creates and edits %s with intentional methods',async kind=>{
    const records = [{id:'one',name:'Existing',projectId:null,enabled:true,state:'ENABLED',healthStatus:'UNKNOWN',repositoryPath:'/repo',defaultBranch:'main',image:'runner',networkPolicy:'none'}]
    const fetch = vi.fn(async (_path:string,options:RequestInit)=>new Response(JSON.stringify(options.method==='GET' ? records : records[0])))
    vi.stubGlobal('fetch',fetch)
    const wrapper = mount(ConfigManager,{props:{kind},global});await flushPromises()
    expect(wrapper.text()).toContain('Existing')
    await button(wrapper,`New ${definitions[kind].singular.toLowerCase()}`).trigger('click')
    for (const field of definitions[kind].fields) {
      const control = wrapper.find(`[data-field="${field.key}"] input, [data-field="${field.key}"] textarea, [data-field="${field.key}"] select`)
      if(field.required && field.type!=='number') await control.setValue(field.resource ? 'one' : field.key==='repositoryPath' ? '/repo' : 'Value')
    }
    await wrapper.get('form').trigger('submit');await flushPromises()
    expect(fetch.mock.calls.some(([,o])=>o.method==='POST')).toBe(true)
    expect(wrapper.text()).toContain('Saved')
    await button(wrapper,'Edit').trigger('click')
    for (const field of definitions[kind].fields) {
      if (field.required && !['name','repositoryPath','defaultBranch'].includes(field.key)) {
        const control=wrapper.find(`[data-field="${field.key}"] input, [data-field="${field.key}"] select`)
        await control.setValue(field.resource ? 'one' : field.type==='number' ? '1' : 'value')
      }
    }
    // Response fixture lacks optional JSON metadata; draft restores safe defaults.
    await wrapper.get('form').trigger('submit');await flushPromises()
    expect(fetch.mock.calls.some(([,o])=>o.method===(kind==='projects' ? 'PATCH' : 'PUT'))).toBe(true)
    wrapper.unmount()
  })
  it('shows reference errors, retries, and disables shared edits in project scope',async()=>{
    let fail = true
    vi.stubGlobal('fetch',vi.fn(async(path:string)=>new Response(JSON.stringify(path.includes('executor-profiles') ? [] : [{id:'a',name:'Shared',projectId:null}]),{status:fail && path.includes('executor-profiles') ? 403 : 200})))
    const wrapper=mount(ConfigManager,{props:{kind:'agents',projectId:'p'},global});await flushPromises()
    await button(wrapper,'View shared').trigger('click');await flushPromises()
    expect(wrapper.text()).toContain('permission')
    fail=false;await button(wrapper,'Retry').trigger('click');await flushPromises()
    expect(wrapper.text()).toContain('Manage this resource from global Settings')
    expect(button(wrapper,'Save')).toBeUndefined()
    expect(wrapper.get('input').attributes('disabled')).toBeDefined()
    await button(wrapper,'Cancel').trigger('click');expect(wrapper.find('[role=dialog]').exists()).toBe(false)
  })
  it('validates drafts, handles failed saves, and clears credentials even on failure',async()=>{
    const fetch=vi.fn(async (_path:string,o:RequestInit)=>new Response(o.method==='GET' ? '[]' : '{}',{status:o.method==='GET' ? 200 : 403}))
    vi.stubGlobal('fetch',fetch)
    const wrapper=mount(ConfigManager,{props:{kind:'providers'},global});await flushPromises()
    await button(wrapper,'New provider').trigger('click');await wrapper.get('form').trigger('submit')
    expect(wrapper.text()).toContain('Check the highlighted')
    await wrapper.get('[data-field=name] input').setValue('Provider')
    await wrapper.findAll('input[type=password]')[0]!.setValue('never-show')
    await wrapper.get('form').trigger('submit');expect(wrapper.text()).toContain('capability are required')
    await wrapper.get('[data-field=credentialRef] input').setValue('key')
    await wrapper.findAll('input[type=password]')[1]!.setValue('capability')
    await wrapper.get('form').trigger('submit');await flushPromises()
    expect(wrapper.text()).toContain('permission')
    expect(wrapper.findAll('input[type=password]').map(x=>(x.element as HTMLInputElement).value)).toEqual(['',''])
    expect(fetch.mock.calls.some(([path])=>path==='/api/secrets')).toBe(true)
  })
  it('supports project settings selection and empty state',async()=>{
    vi.stubGlobal('fetch',vi.fn(async()=>new Response('[{"id":"p","name":"Project"}]')))
    const wrapper=mount(ConfigManager,{props:{kind:'projects',resourceId:'p'},global});await flushPromises()
    expect(wrapper.text()).toContain('Project')
    await wrapper.setProps({resourceId:'missing'});expect(wrapper.text()).toContain('No projects yet')
  })
})
