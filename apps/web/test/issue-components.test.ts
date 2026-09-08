import {mount,flushPromises} from '@vue/test-utils'
import {afterEach,describe,expect,it,vi} from 'vitest'
import {defineComponent,h} from 'vue'
import ProjectBoard from '../app/components/ProjectBoard.vue'
import IssueDetail from '../app/components/IssueDetail.vue'
import IssueEditor from '../app/components/IssueEditor.vue'
import IssueCard from '../app/components/IssueCard.vue'
import {useRefresh} from '../app/composables/useRefresh'
import {uiStubs} from './ui-stubs'
const issue={id:'issue-123456',projectId:'p',title:'Fix scheduler',description:'Persist leases',status:'TODO',assignedAgentId:null,createdAt:'',updatedAt:''}
const agent={id:'a',name:'Coder',state:'ENABLED'}
const global={stubs:{...uiStubs,IssueCard,IssueEditor,NuxtLink:{props:['to'],template:'<a :href="to"><slot/></a>'}}}
const button=(w:ReturnType<typeof mount>,label:string)=>w.findAll('button').find(b=>b.text()===label)!
afterEach(()=>{vi.unstubAllGlobals();vi.useRealTimers()})
describe('Issue workflow components',()=>{
 it('renders exact card hierarchy and safe links for assigned/unassigned Issues',async()=>{
  const w=mount(IssueCard,{props:{issue},global})
  expect(w.get('a').attributes('href')).toBe('/projects/p/issues/issue-123456')
  expect(w.text()).toContain('Unassigned')
  await w.setProps({issue:{...issue,assignedAgentId:'a'}});expect(w.text()).toContain('Assigned Agent')
  await w.setProps({agentName:'Coder'});expect(w.text()).toContain('Coder')
 })
 it('creates only valid Issues and preserves protected transition choices on edit',async()=>{
  const fetch=vi.fn(async()=>new Response(JSON.stringify(issue)));vi.stubGlobal('fetch',fetch)
  const w=mount(IssueEditor,{props:{projectId:'p'},global})
  await w.get('form').trigger('submit');expect(fetch).not.toHaveBeenCalled()
  await w.get('input').setValue('New task');await w.get('textarea').setValue('Details');await w.get('select').setValue('TODO')
  await w.get('form').trigger('submit');await flushPromises()
  expect(w.emitted('saved')?.[0]).toEqual([issue])
  expect(fetch.mock.calls[0]?.[1]).toMatchObject({method:'POST'})
  await button(w,'Cancel').trigger('click');expect(w.emitted('cancel')).toHaveLength(1)
  const done=mount(IssueEditor,{props:{projectId:'p',issue:{...issue,status:'DONE'}},global})
  expect(done.findAll('option').map(o=>o.text())).toEqual(['Select…','DONE','TODO'])
  await done.get('select').setValue('TODO');await done.get('form').trigger('submit');await flushPromises()
  expect(fetch.mock.calls.at(-1)?.[1]).toMatchObject({method:'PATCH'})
  const review=mount(IssueEditor,{props:{projectId:'p',issue:{...issue,status:'REVIEW'}},global})
  expect(review.text()).toContain('Review decision')
  vi.stubGlobal('fetch',vi.fn(async()=>new Response('{}',{status:409})))
  await review.get('form').trigger('submit');await flushPromises();expect(review.text()).toContain('conflicts with workflow policy')
 })
 it('renders all columns, filters Issues, and reconciles after creation',async()=>{
  vi.stubGlobal('fetch',vi.fn(async(path:string)=>new Response(JSON.stringify(path.endsWith('/agents') ? [agent] : path.endsWith('/issues') ? [issue] : {name:'Workspace'}))))
  const w=mount(ProjectBoard,{props:{projectId:'p'},global});await flushPromises()
  expect(w.text()).toContain('Workspace / Board')
  expect(w.findAll('header h2')).toHaveLength(6)
  expect(w.text()).toContain('Fix scheduler')
  await w.get('input').setValue('absent');expect(w.text()).not.toContain('Fix scheduler');expect(w.text()).toContain('No matching issues')
  await button(w,'New issue').trigger('click');await button(w,'Cancel').trigger('click');expect(w.find('[role=dialog]').exists()).toBe(false)
  await button(w,'New issue').trigger('click');await w.get('[data-field=title] input').setValue('Created')
  await w.get('form').trigger('submit');await flushPromises();expect(w.find('[role=dialog]').exists()).toBe(false)
  w.unmount()
 })
 it('assigns eligible Agents and reconstructs the latest Run; supports errors and editing',async()=>{
  let status='TODO';let fail=false;let run=false
  const fetch=vi.fn(async(path:string,o:RequestInit)=>{
   if(o.method==='POST'){if(fail)return new Response('{}',{status:422});status='IN_PROGRESS';run=true;return new Response('{}')}
   return new Response(JSON.stringify(path.endsWith('/agents') ? [agent,{id:'disabled',name:'Disabled',state:'DISABLED'}] : path.endsWith('/runs') ? run ? [{id:'r',issueId:issue.id,attempt:1,status:'QUEUED',queueReason:'capacity',failureReason:'Example failure'}] : [] : {...issue,status,assignedAgentId:run ? 'a':null}))
  })
  vi.stubGlobal('fetch',fetch)
  const w=mount(IssueDetail,{props:{projectId:'p',issueId:issue.id},global});await flushPromises()
  expect(w.text()).toContain('No Runs yet');expect(w.find('option[value=disabled]').exists()).toBe(false)
  await w.get('form').trigger('submit');expect(fetch.mock.calls.filter(([,o])=>o.method==='POST')).toHaveLength(0)
  await w.get('select').setValue('a');fail=true;await w.get('form').trigger('submit');await flushPromises();expect(w.text()).toContain('Unable to assign')
  fail=false;await w.get('form').trigger('submit');await flushPromises()
  expect(w.text()).toContain('IN_PROGRESS');expect(w.text()).toContain('Attempt 1');expect(w.text()).toContain('capacity')
  await button(w,'Edit issue').trigger('click');await button(w,'Cancel').trigger('click')
  await button(w,'Edit issue').trigger('click');await w.findAll('form').at(-1)!.trigger('submit');await flushPromises();expect(w.find('[role=dialog]').exists()).toBe(false)
  w.unmount()
  status='DONE';const done=mount(IssueDetail,{props:{projectId:'p',issueId:issue.id},global});await flushPromises()
  expect(button(done,'Assign Agent').attributes('disabled')).toBeDefined();expect(done.text()).toContain('Reopen this Issue');done.unmount()
 })
 it('refreshes on focus and interval without overlapping or leaking after unmount',async()=>{
  vi.useFakeTimers();let resolve!:()=>void
  const refresh=vi.fn(()=>new Promise<void>(r=>{resolve=r}))
  const w=mount(defineComponent({setup(){useRefresh(refresh,100);return()=>h('div')}}))
  window.dispatchEvent(new Event('focus'));expect(refresh).toHaveBeenCalledTimes(1)
  await vi.advanceTimersByTimeAsync(100);expect(refresh).toHaveBeenCalledTimes(1)
  resolve();await flushPromises();await vi.advanceTimersByTimeAsync(100);expect(refresh).toHaveBeenCalledTimes(2)
  w.unmount();resolve();await flushPromises();window.dispatchEvent(new Event('focus'));await vi.advanceTimersByTimeAsync(300);expect(refresh).toHaveBeenCalledTimes(2)
 })
})
