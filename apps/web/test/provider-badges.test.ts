import { describe, expect, it } from 'vitest'
import { providerHealthBadgeColor, providerHealthBadgeLabel, providerModelCounts, providerModelsBadgeLabel } from '../app/utils/provider-badges'

describe('provider badges', () => {
  it('maps health status to semantic badge colors and labels', () => {
    expect(providerHealthBadgeColor('HEALTHY')).toBe('success')
    expect(providerHealthBadgeColor('UNHEALTHY')).toBe('error')
    expect(providerHealthBadgeColor('UNKNOWN')).toBe('neutral')
    expect(providerHealthBadgeColor('HEALTHY', true)).toBe('warning')
    expect(providerHealthBadgeLabel('HEALTHY')).toBe('Health: Healthy')
    expect(providerHealthBadgeLabel('UNKNOWN', true)).toBe('Health: Checking')
  })

  it('formats model counts and ignores missing persisted counts', () => {
    expect(providerModelsBadgeLabel(3, 3)).toBe('Models: 3 / 3')
    expect(providerModelCounts({ filteredModelCount: 2, totalModelCount: 2 })).toEqual({ filtered: 2, total: 2 })
    expect(providerModelCounts({ filteredModelCount: null, totalModelCount: 2 })).toBeUndefined()
  })
})
