import type { Runner } from '../types/api'

function runnerCapabilityStrings(runner: Runner, key: 'engines' | 'features'): string[] {
  const values = runner.capabilities[key]
  if (!Array.isArray(values)) return []
  return values.filter((value): value is string => typeof value === 'string' && value.trim().length > 0)
}

export function runnerEngines(runner: Runner): string[] {
  return runnerCapabilityStrings(runner, 'engines')
}

export function runnerFeatures(runner: Runner): string[] {
  return runnerCapabilityStrings(runner, 'features')
}

export function runnerSessionSummary(runner: Runner): string {
  const used = runner.connected
    ? Math.max(runner.activeSessions ?? 0, runner.reservedSessions)
    : runner.reservedSessions
  const mode = runner.connected ? 'in use' : 'reserved'
  return `Sessions: ${used} / ${runner.maxActiveSessions} ${mode}`
}
