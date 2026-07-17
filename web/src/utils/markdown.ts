import { marked } from 'marked'
import DOMPurify from 'dompurify'

marked.setOptions({
  gfm: true,
  breaks: true,
})

/** 将 Markdown 文本渲染为经过 DOMPurify 净化的安全 HTML 字符串。 */
export function renderMarkdown(src: string): string {
  if (!src) return ''
  const raw = marked.parse(src, { async: false }) as string
  return DOMPurify.sanitize(raw, {
    USE_PROFILES: { html: true },
  }) as unknown as string
}
