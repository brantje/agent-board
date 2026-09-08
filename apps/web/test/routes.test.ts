import { mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import type { Component } from 'vue'
import SettingsNav from '../app/components/SettingsNav.vue'
import { uiStubs } from './ui-stubs'
const pages = import.meta.glob('../app/pages/**/*.vue',{eager:true,import:'default'}) as Record<string,Component>
afterEach(()=>vi.unstubAllGlobals())
it('binds configuration routes to the correct scope and resource',()=>{
  vi.stubGlobal('useRoute',()=>({params:{projectID:'project-a'}}))
  for(const [path,page] of Object.entries(pages)) {
    if(path.endsWith('/pages/index.vue')) continue
    const wrapper=mount(page,{global:{stubs:{...uiStubs,ConfigManager:{props:['kind','projectId','resourceId'],template:'<div :data-kind="kind" :data-project="projectId" :data-resource="resourceId"/>'},SettingsNav:true}}})
    if(path.endsWith('/settings/index.vue') && !path.includes('[projectID]')) { expect(wrapper.find('settings-nav-stub').exists()).toBe(true);continue }
    const manager=wrapper.get('[data-kind]')
    if(path.includes('[projectID]')) expect(manager.attributes(path.endsWith('/settings/index.vue') ? 'data-resource' : 'data-project')).toBe('project-a')
    else expect(manager.attributes('data-project')).toBeUndefined()
    expect(manager.attributes('data-kind')).toBeTruthy()
  }
})
it('settings navigation follows global and scoped configuration hierarchy',()=>{
  const wrapper=mount(SettingsNav,{global:{stubs:uiStubs}})
  expect(wrapper.get('a[href="/settings/providers"]').text()).toBe('Providers')
  const scoped=mount(SettingsNav,{props:{projectId:'p'},global:{stubs:uiStubs}})
  expect(scoped.get('a[href="/projects/p/settings/runtimes"]').text()).toBe('Runtimes')
})
