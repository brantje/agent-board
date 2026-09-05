export default defineNuxtConfig({
  modules: ['@nuxt/ui'],
  css: ['~/assets/css/main.css'],
  devtools: { enabled: false },
  compatibilityDate: '2026-09-05',
  nitro: {
    preset: 'node-server'
  }
})
