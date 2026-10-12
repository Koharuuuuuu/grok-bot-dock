import MarkdownIt from 'markdown-it'
import DOMPurify from 'dompurify'

const parser = new MarkdownIt({ html: false, linkify: true, breaks: false, typographer: true })

export function renderSanitizedMarkdown(source, sanitizer = DOMPurify, options = {}) {
  const forbiddenTags = ['style', 'svg', 'math', ...(options.allowImages === false ? ['img'] : [])]
  return sanitizer.sanitize(parser.render(source || ''), {
    USE_PROFILES: { html: true },
    FORBID_TAGS: forbiddenTags,
    FORBID_ATTR: ['style'],
  })
}
