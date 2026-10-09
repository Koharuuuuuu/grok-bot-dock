import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'

const parser = new MarkdownIt({ html: false, linkify: true, breaks: false, typographer: true })

export function renderSanitizedMarkdown(source, sanitizer = DOMPurify) {
  return sanitizer.sanitize(parser.render(source || ''), {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ['style', 'svg', 'math'],
    FORBID_ATTR: ['style'],
  })
}
