export default defineNuxtConfig({
  modules: ['@nuxt/ui'],
  css: ['~/assets/css/main.css'],
  colorMode: { preference: 'dark', fallback: 'dark' },
  routeRules: { '/api/**': { proxy: `${process.env.AGENT_BOARD_API_URL || 'http://127.0.0.1:3001'}/api/**` } },
  devtools: { enabled: false },
  compatibilityDate: '2026-09-05',
  nitro: {
    preset: 'node-server'
  }
})
