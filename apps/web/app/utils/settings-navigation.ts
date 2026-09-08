import type { NavigationMenuItem } from '@nuxt/ui'

function projectPath(projectId: string, segment: string) {
  return `/projects/${encodeURIComponent(projectId)}/settings${segment ? `/${segment}` : ''}`
}

export function settingsNavigation(projectId?: string): NavigationMenuItem[][] {
  const modelProfiles = projectId ? projectPath(projectId, 'model-profiles') : '/settings/model-profiles'
  const runtimes = projectId ? projectPath(projectId, 'runtimes') : '/settings/runtimes'
  const executorProfiles = projectId ? projectPath(projectId, 'executor-profiles') : '/settings/executor-profiles'
  const agents = projectId ? `/projects/${encodeURIComponent(projectId)}/agents` : '/agents'
  const overview = projectId ? projectPath(projectId, '') : '/settings'

  return [
    [
      { label: 'Settings', type: 'label' },
      {
        label: projectId ? 'Project' : 'Overview',
        to: overview,
        exact: true
      }
    ],
    [
      { label: 'Models', type: 'label' },
      { label: 'Providers', to: '/settings/providers' },
      { label: 'Model Profiles', to: modelProfiles }
    ],
    [
      { label: 'Infrastructure', type: 'label' },
      { label: 'Runtimes', to: runtimes }
    ],
    [
      { label: 'Execution', type: 'label' },
      { label: 'Agents', to: agents },
      { label: 'Executor Profiles', to: executorProfiles }
    ]
  ]
}
