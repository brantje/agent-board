import { describe, expect, it } from 'vitest'
import type { Runner } from '../app/types/api'
import { runnerEngines, runnerSessionSummary } from '../app/utils/runners'

const baseRunner: Runner = {
  id: 'runner-1',
  name: 'build-host',
  internal: false,
  managed: false,
  deletable: true,
  connected: true,
  registeredAt: '2026-09-10T12:00:00Z',
  revokedAt: null,
  lastSeenAt: null,
  capabilities: {},
  activeSessions: null,
  reservedSessions: 0,
  maxActiveSessions: 10,
  createdAt: '',
  updatedAt: ''
}

describe('runner presentation helpers', () => {
  it('reads supported engines from runner capabilities', () => {
    expect(runnerEngines({
      ...baseRunner,
      capabilities: { engines: ['opencode', 'scripted'] }
    })).toEqual(['opencode', 'scripted'])
    expect(runnerEngines({
      ...baseRunner,
      capabilities: { engines: ['opencode', 1, '', 'scripted'] }
    })).toEqual(['opencode', 'scripted'])
    expect(runnerEngines({ ...baseRunner, capabilities: {} })).toEqual([])
  })

  it('summarizes active sessions when connected and reserved sessions when offline', () => {
    expect(runnerSessionSummary({
      ...baseRunner,
      connected: true,
      activeSessions: 2,
      reservedSessions: 3,
      maxActiveSessions: 10
    })).toBe('Sessions: 2 / 10 active')

    expect(runnerSessionSummary({
      ...baseRunner,
      connected: false,
      activeSessions: null,
      reservedSessions: 3,
      maxActiveSessions: 10
    })).toBe('Sessions: 3 / 10 reserved')
  })

  it('shows idle capacity for registered runners without load', () => {
    expect(runnerSessionSummary({
      ...baseRunner,
      connected: true,
      activeSessions: 0,
      reservedSessions: 0,
      maxActiveSessions: 10
    })).toBe('Sessions: 0 / 10 active')
  })
})
