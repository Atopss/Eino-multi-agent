/** 截断文本到指定字符数（按 rune，安全处理中文等多字节字符）。 */
export function truncate(value: string, max: number): string {
  if (!value) return ''
  const runes = Array.from(value)
  if (runes.length <= max) return value
  return runes.slice(0, max).join('') + '…'
}

/** 生成较短的唯一 id（用于本地消息/会话）。 */
export function uid(prefix = 'id'): string {
  return `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

export function formatTime(ts: number): string {
  const d = new Date(ts)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function formatScore(score: number): string {
  if (score === undefined || score === null || Number.isNaN(score)) return '-'
  return score.toFixed(3)
}
