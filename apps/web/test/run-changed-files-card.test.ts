import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import RunChangedFilesCard from '../app/components/RunChangedFilesCard.vue'
import { event, evidence } from './execution-fixtures'
import { uiStubs } from './ui-stubs'

const global = { stubs: uiStubs }

afterEach(() => vi.unstubAllGlobals())

describe('RunChangedFilesCard', () => {
  it('groups files into folders, shows totals, and scrolls the tree', async () => {
    const snapshot = evidence({
      fileChanges: [
        event({ id: 'root', type: 'file.modified', sequence: 1, payload: { path: 'index.html' } }),
        event({ id: 'nested', type: 'file.modified', sequence: 2, payload: { path: 'src/app.ts' } }),
        event({ id: 'deep', type: 'file.created', sequence: 3, payload: { path: 'src/components/Button.vue', artifactId: 'art-btn' } })
      ],
      artifacts: [
        { id: 'art-patch', name: 'candidate-staged.patch', kind: 'candidate_patch', mediaType: 'text/plain', sizeBytes: 1, digest: null, safeMetadata: {}, contentPath: '', createdAt: '' },
        { id: 'art-btn', name: 'src/components/Button.vue', kind: 'candidate_file', mediaType: 'text/plain', sizeBytes: 1, digest: null, safeMetadata: {}, contentPath: '', createdAt: '' }
      ]
    })
    const patch = [
      'diff --git a/index.html b/index.html',
      '--- a/index.html',
      '+++ b/index.html',
      '@@ -1 +1,2 @@',
      '+one',
      '+two',
      'diff --git a/src/app.ts b/src/app.ts',
      '--- a/src/app.ts',
      '+++ b/src/app.ts',
      '@@ -1 +1 @@',
      '-old',
      '+new'
    ].join('\n')
    vi.stubGlobal('fetch', vi.fn(async (path: string) => {
      if (path.endsWith('/artifacts/art-patch')) {
        return new Response(patch, { headers: { 'Content-Type': 'text/plain' } })
      }
      if (path.endsWith('/artifacts/art-btn')) {
        return new Response('export default {}\n', { headers: { 'Content-Type': 'text/plain' } })
      }
      return new Response('', { headers: { 'Content-Type': 'text/plain' } })
    }))
    const wrapper = mount(RunChangedFilesCard, { props: { projectId: 'project-a', evidence: snapshot }, global })
    await flushPromises()

    expect(wrapper.get('[data-changed-files-totals]').text()).toContain('+4')
    expect(wrapper.get('[data-changed-files-totals]').text()).toContain('-1')
    expect(wrapper.get('[data-changed-files-scroll]').classes()).toContain('overflow-y-auto')
    expect(wrapper.get('[data-changed-files-scroll]').classes().some(className => className.startsWith('max-h-'))).toBe(true)

    const tree = wrapper.get('[data-changed-files-tree]')
    expect(tree.text()).toContain('src')
    expect(tree.text()).toContain('components')
    expect(tree.text()).toContain('Button.vue')
    expect(tree.text()).toContain('app.ts')
    expect(tree.text()).toContain('index.html')
    expect(tree.text().indexOf('src')).toBeLessThan(tree.text().indexOf('app.ts'))
    expect(tree.text().indexOf('components')).toBeLessThan(tree.text().indexOf('Button.vue'))

    await wrapper.get('[data-folder-path="src"]').trigger('click')
    expect(wrapper.get('[data-changed-files-tree]').text()).not.toContain('app.ts')
    expect(wrapper.get('[data-changed-files-tree]').text()).not.toContain('Button.vue')
    expect(wrapper.get('[data-changed-files-tree]').text()).toContain('index.html')
  })
})
