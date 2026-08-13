import { shortKey } from './format'

export type EndpointParts = {
  host: string
  port: string
}

export type PeerBlock = {
  publicKey: string
  allowedIPs: string
  endpoint: string
  endpointHost: string
  endpointPort: string
  presharedKey: string
  keepalive: string
  label: string
}

export function validatePeerConfigDraft(content: string) {
  const normalized = content.toLowerCase()
  if (normalized.includes('[interface]') || normalized.includes('privatekey')) return '这里只能编辑 Peer 配置，不能包含 [Interface] 或 PrivateKey'
  const text = content.trim()
  if (!text) return ''
  if (!/^\s*(#.*|;.*|\[Peer\])/m.test(text)) return 'Peer 配置需要以 [Peer] 段落开始'
  return ''
}

export function validWireGuardInterfaceName(value: string) {
  return /^[A-Za-z0-9_.-]{1,15}$/.test(value.trim())
}

export function validWireGuardKey(value: string) {
  return /^[A-Za-z0-9+/]{43}=$/.test(value.trim())
}

export function randomWireGuardKey() {
  const bytes = new Uint8Array(32)
  window.crypto.getRandomValues(bytes)
  let binary = ''
  bytes.forEach((byte) => { binary += String.fromCharCode(byte) })
  return window.btoa(binary)
}

export function splitEndpoint(endpoint: string): EndpointParts {
  const value = endpoint.trim()
  if (!value) return { host: '', port: '' }
  if (value.startsWith('[')) {
    const end = value.indexOf(']')
    if (end >= 0) {
      const host = value.slice(1, end)
      const rest = value.slice(end + 1)
      return { host, port: rest.startsWith(':') ? rest.slice(1) : '' }
    }
  }
  const colon = value.lastIndexOf(':')
  if (colon > 0 && value.indexOf(':') === colon) return { host: value.slice(0, colon), port: value.slice(colon + 1) }
  return { host: value, port: '' }
}

export function joinEndpoint(host: string, port: string) {
  const cleanHost = host.trim()
  const cleanPort = port.trim()
  if (!cleanHost || !cleanPort) return ''
  const wrappedHost = cleanHost.includes(':') && !cleanHost.startsWith('[') ? `[${cleanHost}]` : cleanHost
  return `${wrappedHost}:${cleanPort}`
}

export function validPort(value: string) {
  const parsed = Number(value)
  return Number.isInteger(parsed) && parsed >= 1 && parsed <= 65535
}

export function validIPv4(value: string) {
  const parts = value.split('.')
  return parts.length === 4 && parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) >= 0 && Number(part) <= 255)
}

export function validIPv6(value: string) {
  const text = value.trim()
  if (!text.includes(':')) return false
  if (text.includes(':::')) return false
  const doubleColon = (text.match(/::/g) || []).length
  if (doubleColon > 1) return false
  const parts = text.split(':').filter(Boolean)
  if (doubleColon === 0 && parts.length !== 8) return false
  if (doubleColon === 1 && parts.length >= 8) return false
  return parts.every((part) => /^[0-9a-f]{1,4}$/i.test(part))
}

export function validPrefix(value: string) {
  const [ip, bitsText] = value.trim().split('/')
  if (!ip || bitsText === undefined || !/^\d+$/.test(bitsText)) return false
  const bits = Number(bitsText)
  if (validIPv4(ip)) return bits >= 0 && bits <= 32
  if (validIPv6(ip)) return bits >= 0 && bits <= 128
  return false
}

export function normalizeAllowedIPs(value: string) {
  return value.split(',').map((item) => item.trim()).filter(Boolean).join(', ')
}

export function validateAllowedIPs(label: string, value: string) {
  const entries = value.split(',').map((item) => item.trim()).filter(Boolean)
  if (!entries.length) return `${label} 不能为空`
  const bad = entries.find((item) => !validPrefix(item))
  return bad ? `${label} 包含无效网段：${bad}` : ''
}

export function validateEndpoint(label: string, host: string, port: string, required = false) {
  const hasHost = Boolean(host.trim())
  const hasPort = Boolean(port.trim())
  if (!hasHost && !hasPort) return required ? `${label} 不能为空` : ''
  if (!hasHost) return `${label} 地址不能为空`
  if (!hasPort) return `${label} 端口不能为空`
  if (!validPort(port)) return `${label} 端口必须在 1-65535 之间`
  return ''
}

export function buildPeerBlock(publicKey: string, allowedIPs: string, endpoint: string, presharedKey: string, keepalive: string) {
  const lines = ['[Peer]', `PublicKey = ${publicKey.trim()}`]
  if (presharedKey.trim()) lines.push(`PresharedKey = ${presharedKey.trim()}`)
  lines.push(`AllowedIPs = ${normalizeAllowedIPs(allowedIPs)}`)
  if (endpoint.trim()) lines.push(`Endpoint = ${endpoint.trim()}`)
  if (keepalive.trim() && keepalive.trim() !== '0') lines.push(`PersistentKeepalive = ${keepalive.trim()}`)
  return lines.join('\n')
}

export function splitPeerBlocks(content: string) {
  const normalized = content.replace(/\r\n/g, '\n').replace(/\r/g, '\n').trim()
  if (!normalized) return [] as string[]
  const blocks: string[][] = []
  let current: string[] = []
  for (const line of normalized.split('\n')) {
    const trimmed = line.trim().toLowerCase()
    if (trimmed === '[peer]' && current.length) {
      blocks.push(current)
      current = [line]
    } else {
      current.push(line)
    }
  }
  if (current.length) blocks.push(current)
  return blocks.map((block) => block.join('\n').trim()).filter(Boolean)
}

export function peerBlockPublicKey(block: string) {
  return peerBlockField(block, 'PublicKey')
}

export function peerBlockField(block: string, field: string) {
  for (const line of block.split('\n')) {
    const [key, value] = line.split('=', 2)
    if (key && value !== undefined && key.trim().toLowerCase() === field.toLowerCase()) return value.trim()
  }
  return ''
}

export function parsePeerBlock(block: string): PeerBlock {
  const publicKey = peerBlockPublicKey(block)
  const allowedIPs = peerBlockField(block, 'AllowedIPs')
  const endpoint = peerBlockField(block, 'Endpoint')
  const endpointParts = splitEndpoint(endpoint)
  const presharedKey = peerBlockField(block, 'PresharedKey')
  const keepalive = peerBlockField(block, 'PersistentKeepalive')
  const label = endpoint || allowedIPs || shortKey(publicKey) || '未命名 Peer'
  return { publicKey, allowedIPs, endpoint, endpointHost: endpointParts.host, endpointPort: endpointParts.port, presharedKey, keepalive, label }
}

export function peerExists(content: string, publicKey: string) {
  return splitPeerBlocks(content).some((block) => peerBlockPublicKey(block) === publicKey.trim())
}

export function upsertPeerBlock(content: string, block: string, publicKey: string) {
  const blocks = splitPeerBlocks(content)
  if (!blocks.length) return block
  let replaced = false
  const next = blocks.map((current) => {
    if (peerBlockPublicKey(current) === publicKey.trim()) {
      replaced = true
      return block
    }
    return current
  })
  if (!replaced) next.push(block)
  return next.join('\n\n')
}
