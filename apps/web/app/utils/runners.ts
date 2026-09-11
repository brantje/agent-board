import type { Runner } from '../types/api'

export function runnerEngines(runner: Runner): string[] {
  const engines = runner.capabilities.engines
  if (!Array.isArray(engines)) return []
  return engines.filter((engine): engine is string => typeof engine === 'string' && engine.trim().length > 0)
}

export function runnerSessionSummary(runner: Runner): string {
  const used = runner.connected
    ? (runner.activeSessions ?? 0)
    : runner.reservedSessions
  const mode = runner.connected ? 'active' : 'reserved'
  return `Sessions: ${used} / ${runner.maxActiveSessions} ${mode}`
}
