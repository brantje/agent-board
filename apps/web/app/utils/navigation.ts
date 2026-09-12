export function navigation(projectId?: string) {
  const global = [
    { label: 'Projects', to: '/projects', icon: 'i-lucide-folders' },
    { label: 'Runs', to: '/runs', icon: 'i-lucide-play' },
    { label: 'Inbox', to: '/inbox', icon: 'i-lucide-inbox' },
    { label: 'Account', to: '/account', icon: 'i-lucide-user-round' }
  ]
  const settings = [
    { label: 'Settings', to: '/settings', icon: 'i-lucide-settings' }
  ]
  const project = projectId
    ? ['Board', 'Agents', 'Runs', 'Settings'].map(label => ({
        label,
        to: `/projects/${encodeURIComponent(projectId)}/${label.toLowerCase()}`
      }))
    : []
  return { global, settings, project }
}
