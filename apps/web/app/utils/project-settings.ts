export type NewIssuePlacement = 'top' | 'bottom'

export function projectNewIssuePlacement(settings: Record<string, unknown> | null | undefined): NewIssuePlacement {
  return settings?.newIssuePlacement === 'top' ? 'top' : 'bottom'
}
