import { describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
it('centralizes square surfaces and compact controls', async () => {
  vi.stubGlobal('defineAppConfig', (value: unknown) => value)
  const { default: config } = await import('../app/app.config')
  expect(config.ui.card.slots.root).toContain('rounded-none')
  expect(config.ui.modal.slots.content).toContain('rounded-none')
  expect(config.ui.button.defaultVariants.size).toBe('sm')
  expect(config.ui.colors.primary).toBe('steel')
  const css = readFileSync('app/assets/css/main.css','utf8')
  expect(css).toContain('.dark')
  expect(css).toContain(':root')
  vi.unstubAllGlobals()
})
