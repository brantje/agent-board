import { resolveAgentBoardApiUrl } from './app/utils/agent-board-api-url'

export default defineNuxtConfig({
  modules: ['@nuxt/ui'],
  css: ['~/assets/css/main.css'],
  colorMode: { preference: 'dark', fallback: 'dark' },
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
