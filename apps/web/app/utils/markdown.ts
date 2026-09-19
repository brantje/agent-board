import { Marked } from 'marked'

function escapeHtml(value: string) {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
}

function safeHref(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  try {
    const parsed = new URL(trimmed, 'https://agent-board.invalid/')
    return ['http:', 'https:', 'mailto:'].includes(parsed.protocol) ? trimmed : undefined
  } catch {
    return undefined
  }
}

const markdown = new Marked({
  gfm: true,
  breaks: true,
  renderer: {
    html({ text }) {
      return escapeHtml(text)
    },
    link({ href, title, tokens }) {
      const label = this.parser.parseInline(tokens)
      const safe = safeHref(href)
      if (!safe) return label
      const titleAttribute = title ? ` title="${escapeHtml(title)}"` : ''
      return `<a href="${escapeHtml(safe)}"${titleAttribute} rel="noopener noreferrer">${label}</a>`
    },
    image({ text }) {
      return escapeHtml(text)
    }
  }
})

export function renderMarkdown(value: string) {
  return markdown.parse(value) as string
}
