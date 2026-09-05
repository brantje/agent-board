import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import AppShell from '../app/app.vue'
import IndexPage from '../app/pages/index.vue'

const passThrough = {
  template: '<div><slot /></div>'
}

const cardStub = {
  template: '<section><slot name="header" /><slot /></section>'
}

const badgeStub = {
  props: ['label'],
  template: '<span>{{ label }}</span>'
}

describe('bootstrap page', () => {
  it('renders the bootstrap state from shared copy', () => {
    const wrapper = mount(IndexPage, {
      global: {
        stubs: {
          UMain: passThrough,
          UContainer: passThrough,
          UCard: cardStub,
          UBadge: badgeStub
        }
      }
    })

    expect(wrapper.text()).toContain('Agent Board')
    expect(wrapper.text()).toContain('Bootstrap environment ready')
    expect(wrapper.text()).toContain('v0.1 bootstrap')
  })
})

describe('application shell', () => {
  it('renders the Nuxt page inside the app shell', () => {
    const wrapper = mount(AppShell, {
      global: {
        stubs: {
          UApp: passThrough,
          NuxtPage: {
            template: '<p>current page</p>'
          }
        }
      }
    })

    expect(wrapper.text()).toContain('current page')
  })
})
