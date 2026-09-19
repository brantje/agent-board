import { describe, expect, it } from 'vitest'
import { renderMarkdown } from '../app/utils/markdown'

describe('safe Markdown rendering', () => {
  it('renders normal Markdown while escaping raw HTML and unsafe targets', () => {
    const html = renderMarkdown([
      '**bold** and `code`',
      '',
      '[safe](https://example.com/docs)',
      '',
      '[bad](javascript:alert(1))',
      '',
      '<script>alert("xss")</script>',
      '',
      '![tracking](javascript:alert(2))'
    ].join('\n'))

    expect(html).toContain('<strong>bold</strong>')
    expect(html).toContain('<code>code</code>')
    expect(html).toContain('href="https://example.com/docs"')
    expect(html).not.toContain('href="javascript:')
    expect(html).not.toContain('<script')
    expect(html).toContain('&lt;script&gt;')
    expect(html).not.toContain('<img')
  })
})
