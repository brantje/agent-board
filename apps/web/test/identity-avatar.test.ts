import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import IdentityAvatar from '../app/components/IdentityAvatar.vue'
import { uiStubs } from './ui-stubs'

const global = { stubs: uiStubs }

describe('IdentityAvatar', () => {
  it('renders a bot icon for agents without a letter fallback', () => {
    const wrapper = mount(IdentityAvatar, {
      props: { kind: 'agent', name: 'Frontend Engineer' },
      global
    })

    expect(wrapper.find('[data-icon="i-lucide-bot"]').exists()).toBe(true)
    expect(wrapper.text()).not.toBe('F')
    expect(wrapper.find('[aria-label="Frontend Engineer"]').exists()).toBe(true)
  })

  it('renders the first letter for users without a bot icon', () => {
    const wrapper = mount(IdentityAvatar, {
      props: { kind: 'user', name: 'Frontend Engineer' },
      global
    })

    expect(wrapper.text()).toBe('F')
    expect(wrapper.find('[data-icon="i-lucide-bot"]').exists()).toBe(false)
    expect(wrapper.find('[aria-label="Frontend Engineer"]').exists()).toBe(true)
  })

  it('keeps an accessible label when the display name is empty', () => {
    const wrapper = mount(IdentityAvatar, {
      props: { kind: 'user', name: '   ' },
      global
    })

    expect(wrapper.find('[aria-label="Unknown"]').exists()).toBe(true)
    expect(wrapper.text()).toBe('?')
  })
})
