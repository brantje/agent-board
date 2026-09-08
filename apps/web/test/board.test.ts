import { describe,it,expect } from 'vitest'
import { boardColumns, editableStatuses, latestRun } from '../app/utils/issues'
const issue = (id:string,status:string) => ({id,status,title:id,projectId:'p',description:'',assignedAgentId:null,createdAt:'',updatedAt:''})
describe('durable Issue board projection',()=>{
 it('keeps six Issue states separate from Run states and filters by title',()=>{
  const columns=boardColumns([issue('First','BLOCKED'),issue('Second','TODO')],'first')
  expect(columns.map(c=>c.status)).toEqual(['BACKLOG','TODO','IN_PROGRESS','BLOCKED','REVIEW','DONE'])
  expect(columns.find(c=>c.status==='BLOCKED')?.issues.map(i=>i.id)).toEqual(['First'])
  expect(columns.find(c=>c.status==='TODO')?.issues).toEqual([])
 })
 it('protects Review and Done and reopens only into Todo',()=>{
  expect(editableStatuses('REVIEW')).toEqual(['REVIEW'])
  expect(editableStatuses('DONE')).toEqual(['DONE','TODO'])
  expect(editableStatuses('TODO')).not.toContain('DONE')
  expect(editableStatuses()).toEqual(['BACKLOG','TODO'])
 })
 it('selects latest persisted attempt for the same Issue only',()=>{
  expect(latestRun([{id:'old',issueId:'i',attempt:1},{id:'other',issueId:'other',attempt:8},{id:'latest',issueId:'i',attempt:2}] as never,'i')?.id).toBe('latest')
  expect(latestRun([],'i')).toBeUndefined()
 })
})
