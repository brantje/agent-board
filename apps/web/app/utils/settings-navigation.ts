import type { NavigationMenuItem } from '@nuxt/ui'

function projectPath(projectId: string, segment: string) {
  return `/projects/${encodeURIComponent(projectId)}/settings${segment ? `/${segment}` : ''}`
}

export function settingsNavigation(projectId?: string): NavigationMenuItem[][] {
  const providers = projectId ? projectPath(projectId, 'providers') : '/settings/providers'
  const modelProfiles = projectId ? projectPath(projectId, 'model-profiles') : '/settings/model-profiles'
  const overview = projectId ? projectPath(projectId, '') : '/settings'

  const groups: NavigationMenuItem[][] = [
    [
      { label: 'Settings', type: 'label' },
      { label: projectId ? 'Project' : 'Overview', to: overview, exact: true }
    ],
    [
      { label: 'Models', type: 'label' },
      { label: 'Providers', to: providers },
      { label: 'Model Profiles', to: modelProfiles }
    ]
  ]
  if (!projectId) {
    groups.push(
      [
        { label: 'Identity', type: 'label' },
        { label: 'Users', to: '/settings/users' },
        { label: 'Authentication / Security', to: '/settings/authentication' }
      ],
      [
        { label: 'Execution', type: 'label' },
        { label: 'Agents', to: '/settings/agents' }
      ],
      [
        { label: 'Infrastructure', type: 'label' },
        { label: 'Runners', to: '/settings/runners' }
      ]
    )
  }
  return groups
}
