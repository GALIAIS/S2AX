import DOMPurify from 'dompurify'
import { marked } from 'marked'

marked.setOptions({
  breaks: true,
  gfm: true,
})

export function renderMarkdown(content: string): string {
  if (!content.trim()) return ''
  return DOMPurify.sanitize(marked.parse(content) as string)
}
