export function ago(ts: number): string {
  if (!ts) return '—'
  const s = Math.max(0, Math.floor((Date.now() - ts) / 1000))
  if (s < 60) return `${s}s 前`
  if (s < 3600) return `${Math.floor(s / 60)}m 前`
  if (s < 86400) return `${Math.floor(s / 3600)}h 前`
  return `${Math.floor(s / 86400)}d 前`
}

export function fmtDateTime(ts: number): string {
  if (!ts) return '—'
  const d = new Date(ts)
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${month}-${day} ${d.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit' })}`
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

/**
 * 压缩公网端点显示：IPv6（含 [::1]:port 形式）通常很长，保留头尾、
 * 中间用省略号压缩，避免撑宽表格；IPv4/短值原样返回。
 */
export function shortEndpoint(endpoint: string): string {
  const value = endpoint || ''
  if (!value) return '—'
  const isIPv6 = value.includes(':') && !/^\d+\.\d+\.\d+\.\d+/.test(value)
  const maxLength = isIPv6 ? 22 : 32
  if (value.length <= maxLength) return value
  // 保留协议/前缀前 8 个字符 + 尾部 12 个字符（覆盖端口），中间省略
  return `${value.slice(0, 8)}…${value.slice(-12)}`
}

export function fmtMbps(value: number): string {
  return Number.isFinite(value) ? value.toFixed(2) : '0.00'
}

/** 字节数 → 自适应单位（1024 进制，与 WireGuard 计数器一致） */
export function fmtBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / 1024 ** index
  return `${value >= 100 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`
}
