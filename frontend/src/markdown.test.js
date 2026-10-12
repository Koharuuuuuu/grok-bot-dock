import { describe, expect, it } from 'vitest'
import { renderSanitizedMarkdown } from './markdown'

describe('sanitized Markdown rendering', () => {
  it('removes remote images from anonymous share content', () => {
    const html = renderSanitizedMarkdown('![private image](https://tracker.example/pixel.png)', undefined, { allowImages: false })
    expect(html).not.toContain('<img')
    expect(html).not.toContain('tracker.example')
  })

  it('keeps safe formatting while removing script payloads', () => {
    const html = renderSanitizedMarkdown('# Report\n\n<script>alert(1)</script> **safe**')
    expect(html).toContain('<h1>Report</h1>')
    expect(html).toContain('<strong>safe</strong>')
    expect(html).not.toContain('<script')
    const unsafeLink = renderSanitizedMarkdown('[click](javascript:alert(1))')
    expect(unsafeLink).not.toContain('href="javascript:')
  })
})
