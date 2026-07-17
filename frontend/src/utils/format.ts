export function ago(ts: number): string {
  if (!ts) return '—'
  const s = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (s < 60) return `${s}s 前`
  if (s < 3600) return `${Math.floor(s / 60)}m 前`
  if (s < 86400) return `${Math.floor(s / 3600)}h 前`
  return `${Math.floor(s / 86400)}d 前`
}

export function fmtTime(ts: number): string {
  return ts ? new Date(ts).toLocaleTimeString('zh-CN', { hour12: false }) : '—'
}

export function fmtDateTime(ts: number): string {
  if (!ts) return '—'
  const d = new Date(ts)
  return `${d.getMonth() + 1}-${d.getDate()} ${d.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' })}`
}

export function fmtHandshake(secAgo: number): string {
  if (secAgo < 0) return '从未握手'
  if (secAgo < 60) return `${secAgo}s 前`
  if (secAgo < 3600) return `${Math.floor(secAgo / 60)}m 前`
  return `${Math.floor(secAgo / 3600)}h 前`
}

export function shortKey(key: string): string {
  if (!key) return '—'
  return `${key.slice(0, 10)}…${key.slice(-6)}`
}

export function fmtMbps(value: number): string {
  return Number.isFinite(value) ? value.toFixed(2) : '0.00'
}
