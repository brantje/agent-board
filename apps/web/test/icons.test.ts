import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

function source(path: string) {
  return readFileSync(resolve(process.cwd(), path), 'utf8')
}

describe('local Lucide icon delivery', () => {
  it('does not proxy the Nuxt Icon API through the Go /api route and bundles app icons including .ts navigation', () => {
    const config = source('nuxt.config.ts')
    expect(source('package.json')).toContain('"@iconify-json/lucide"')
    expect(config).toContain("localApiEndpoint: '/_nuxt_icon'")
    expect(config).not.toMatch(/localApiEndpoint:\s*['"]\/api\//)
    expect(config).toContain("'/api/**'")
    expect(config).toContain('serverBundle: \'local\'')
    expect(config).toContain('globInclude: [\'app/**/*.{vue,ts}\']')
    expect(source('app/utils/navigation.ts')).toContain("icon: 'i-lucide-")
  })
})
