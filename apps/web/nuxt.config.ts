import { resolveAgentBoardApiUrl } from './app/utils/agent-board-api-url'

const themeBootstrap = `(() => {
  const key = 'agent-board-theme'
  const allowed = new Set(['dark', 'light'])
  let value = 'dark'
  try {
    const stored = localStorage.getItem(key)
    if (stored && allowed.has(stored)) value = stored
  } catch {}
  document.documentElement.dataset.theme = value
  document.documentElement.style.colorScheme = value
  document.documentElement.classList.toggle('dark', value === 'dark')
  document.documentElement.classList.toggle('light', value === 'light')
})()`

export default defineNuxtConfig({
  modules: ['@nuxt/ui'],
  css: ['~/assets/css/main.css'],
  colorMode: { preference: 'dark', fallback: 'dark', classSuffix: '-managed' },
  app: {
    head: {
      meta: [{ name: 'color-scheme', content: 'dark light' }],
      script: [{ innerHTML: themeBootstrap }]
    }
  },
  routeRules: {
    '/api/**': {
      proxy: `${resolveAgentBoardApiUrl(process.env.AGENT_BOARD_API_URL)}/api/**`
    }
  },
  devtools: { enabled: false },
  compatibilityDate: '2026-09-05',
  nitro: {
    preset: 'node-server'
  }
})
