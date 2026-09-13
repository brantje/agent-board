import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useProjectPermissions } from '../app/composables/useProjectPermissions'

afterEach(() => vi.unstubAllGlobals())

async function permissionsFor(role: 'viewer' | 'member' | 'admin') {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ role })))
  vi.stubGlobal('fetch', fetch)
  let permissions!: ReturnType<typeof useProjectPermissions>
  const wrapper = mount(defineComponent({
    setup() {
      permissions = useProjectPermissions('project-a')
      return () => h('div')
    }
  }))
  await flushPromises()
  return { permissions, wrapper, fetch }
}

describe('useProjectPermissions', () => {
  it('keeps viewers read-only', async () => {
    const { permissions, wrapper } = await permissionsFor('viewer')
    expect(permissions.role.value).toBe('viewer')
    expect(permissions.canMutate.value).toBe(false)
    expect(permissions.canAdmin.value).toBe(false)
    wrapper.unmount()
  })

  it('allows normal workflow mutations for members without Project administration', async () => {
    const { permissions, wrapper } = await permissionsFor('member')
    expect(permissions.canMutate.value).toBe(true)
    expect(permissions.canAdmin.value).toBe(false)
    wrapper.unmount()
  })

  it('allows Project administration for admins', async () => {
    const { permissions, wrapper } = await permissionsFor('admin')
    expect(permissions.canMutate.value).toBe(true)
    expect(permissions.canAdmin.value).toBe(true)
    wrapper.unmount()
  })
})
