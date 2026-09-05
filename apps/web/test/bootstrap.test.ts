import { describe, expect, it } from 'vitest'

import { bootstrapCopy } from '../app/utils/bootstrap'

describe('bootstrap copy', () => {
  it('identifies the application and bootstrap state', () => {
    expect(bootstrapCopy.title).toBe('Agent Board')
    expect(bootstrapCopy.status).toContain('ready')
  })
})
