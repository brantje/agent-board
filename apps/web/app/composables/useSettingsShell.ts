import { inject, type ComputedRef, type InjectionKey } from 'vue'

export type SettingsShellContext = {
  projectId: ComputedRef<string | undefined>
}

export const settingsShellKey: InjectionKey<SettingsShellContext> = Symbol('settingsShell')

export function useSettingsShell() {
  return inject(settingsShellKey, null)
}
