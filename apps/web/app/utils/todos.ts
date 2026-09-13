import type { TodoActivityItem } from './events'

export function todoStatusIcon(status: TodoActivityItem['status']) {
  if (status === 'completed') return 'i-lucide-circle-check'
  if (status === 'in_progress') return 'i-lucide-loader-circle'
  if (status === 'cancelled') return 'i-lucide-circle-x'
  return 'i-lucide-circle'
}

export function todoStatusClass(status: TodoActivityItem['status']) {
  if (status === 'in_progress') return 'text-default'
  if (status === 'completed' || status === 'cancelled') return 'text-muted line-through'
  return 'text-muted'
}
