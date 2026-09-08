import { mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import type { Component } from 'vue'
import { uiStubs } from './ui-stubs'
const pages = import.meta.glob('../app/pages/**/*.vue',{eager:true,import:'default'}) as Record<string,Component>
afterEach(()=>vi.unstubAllGlobals())
function isConfigPage(path: string) {
  return !path.endsWith('/pages/index.vue')
    && !path.includes('/issues/')
    && !path.endsWith('/board.vue')
    && !path.endsWith('/runs.vue')
    && !path.endsWith('/inbox.vue')
    && !path.includes('/runs/')
    && !path.includes('/reviews/')
}

it('binds configuration routes to the correct scope and resource',()=>{
  vi.stubGlobal('useRoute',()=>({params:{projectID:'project-a'}}))
  for(const [path,page] of Object.entries(pages)) {
    if(!isConfigPage(path)) continue
    const wrapper = mount(page, {
      global: {
        stubs: {
          ...uiStubs,
          SettingsShell: { template: '<div data-settings-shell><slot /></div>' },
          ConfigManager: {
            props: ['kind', 'projectId', 'resourceId'],
            template: '<div :data-kind="kind" :data-project="projectId" :data-resource="resourceId" />'
          }
        }
      }
    })
    if(path.endsWith('/settings/index.vue') && !path.includes('[projectID]')) { expect(wrapper.find('[data-settings-shell]').exists()).toBe(true);continue }
    const manager=wrapper.get('[data-kind]')
    if(path.includes('[projectID]')) expect(manager.attributes(path.endsWith('/settings/index.vue') ? 'data-resource' : 'data-project')).toBe('project-a')
    else expect(manager.attributes('data-project')).toBeUndefined()
    expect(manager.attributes('data-kind')).toBeTruthy()
  }
})

it('binds Issue and Board routes to Project and Issue IDs', async()=>{
 vi.stubGlobal('useRoute',()=>({params:{projectID:'p',issueID:'i'}}))
 for(const path of Object.keys(pages).filter(p=>p.includes('/issues/') || p.endsWith('/board.vue'))){
  const w=mount(pages[path]!,{global:{stubs:{ProjectBoard:{props:['projectId'],template:'<div :data-project="projectId"/>'},IssueDetail:{props:['projectId','issueId'],template:'<div :data-project="projectId" :data-issue="issueId"/>'}}}})
  expect(w.get('[data-project]').attributes('data-project')).toBe('p')
  if(path.includes('/issues/'))expect(w.get('[data-issue]').attributes('data-issue')).toBe('i')
 }
})

it('binds Runs, Inbox, Run detail and Review routes', ()=>{
  vi.stubGlobal('useRoute',()=>({params:{projectID:'p',runID:'run-1',reviewID:'review-1'}}))
  const stubs = {
    RunList:{props:['projectId'],template:'<div data-runs="true" :data-project="projectId"/>'},
    InboxView:{template:'<div data-inbox="true"/>'},
    RunDetail:{props:['projectId','runId'],template:'<div data-run-detail="true" :data-project="projectId" :data-run="runId"/>'},
    ReviewDetail:{props:['projectId','reviewId'],template:'<div data-review-page="true" :data-project="projectId" :data-review="reviewId"/>'}
  }
  const wrapperFor = (suffix: string) => {
    const path = Object.keys(pages).find(candidate => candidate.endsWith(suffix))
    return mount(pages[path!]!, { global: { stubs } })
  }
  expect(wrapperFor('/pages/runs.vue').get('[data-runs]').attributes('data-project')).toBeUndefined()
  expect(wrapperFor('/pages/inbox.vue').get('[data-inbox]').exists()).toBe(true)
  expect(wrapperFor('/runs/index.vue').get('[data-runs]').attributes('data-project')).toBe('p')
  expect(wrapperFor('/runs/[runID].vue').get('[data-run]').attributes('data-run')).toBe('run-1')
  expect(wrapperFor('/reviews/[reviewID].vue').get('[data-review-page]').attributes('data-review')).toBe('review-1')
})
