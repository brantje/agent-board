export function navigation(projectId?: string) {
  const global = [
    { label: 'Projects', to: '/projects', icon: 'i-lucide-folders' },
    { label: 'Agents', to: '/agents', icon: 'i-lucide-bot' },
    { label: 'Runs', to: '/runs', icon: 'i-lucide-play' },
    { label: 'Inbox', to: '/inbox', icon: 'i-lucide-inbox' },
    { label: 'Settings', to: '/settings', icon: 'i-lucide-settings' }
  ]
  const project = projectId ? ['Board', 'Agents', 'Runs', 'Settings'].map(label => ({ label, to: `/projects/${encodeURIComponent(projectId)}/${label.toLowerCase()}` })) : []
  return { global, project }
}
