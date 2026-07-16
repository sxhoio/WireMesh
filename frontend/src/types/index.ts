// ---- WireMesh v2 数据模型：一个 Agent 管理多个 WireGuard 接口 ----

export type AgentStatus = 'online' | 'offline'
/** 链路/Peer 状态：ok=绿 degraded=黄 down=红 unknown=灰虚线 */
export type PeerState = 'ok' | 'degraded' | 'down' | 'unknown'
export type Topology = 'full-mesh' | 'hub-spoke' | 'custom'
export type Role = 'admin' | 'operator' | 'viewer'

export interface Project {
  id: string
  name: string
  desc: string
}

export interface Network {
  id: string
  projectId: string
  name: string
  cidr: string
  topology: Topology
  /** custom 拓扑下手动选择的接口对（interface id 对） */
  customPairs: [string, string][]
}

export interface WGInterface {
  id: string
  agentId: string
  networkId: string
  name: string // wg0
  listenPort: number
  mtu: number
  publicKey: string
  tunnelIP: string
  role: 'hub' | 'spoke' | 'mesh' // hub-spoke 拓扑用
}

export interface Agent {
  id: string
  projectId: string
  name: string
  hostname: string
  status: AgentStatus
  enabled: boolean
  version: string
  osInfo: string
  labels: string[]
  publicIP: string
  city: string
  country: string
  lng: number
  lat: number
  lastSeen: number
  rxMbps: number
  txMbps: number
  totalRxGB: number
  totalTxGB: number
  interfaces: WGInterface[]
}

/** 两个接口之间的直连链路（由 Peer 观测推导） */
export interface PeerLink {
  id: string
  networkId: string
  a: string // interface id
  b: string // interface id
  state: PeerState
  latencyMs: number
  lossPct: number
  lastHandshakeSecAgo: number // -1 表示从未握手
  rxMbps: number
  txMbps: number
  failReason?: string
  singleSide?: boolean
}

/** 未匹配到受管接口的临时 Peer */
export interface TempPeer {
  id: string
  publicKey: string
  endpoint: string // 可能为空
  allowedIPs: string
  sourceIfaceId: string // 来源接口
  lastHandshakeSecAgo: number
  rxMB: number
  txMB: number
  geo?: { city: string; country: string; lng: number; lat: number }
  firstSeen: number
}

export interface FeedEvent {
  id: string | number
  time: number
  kind: 'report' | 'collect' | 'alert' | 'publish' | 'system' | 'check' | 'notify'
  message: string
}

// ---- 通知渠道 ----
export type NotifyChannelType = 'webhook' | 'dingtalk' | 'wecom' | 'feishu' | 'telegram' | 'email'

export interface NotifyChannel {
  id: string
  name: string
  type: NotifyChannelType
  /** Webhook URL / Bot Token / 邮箱地址等 */
  target: string
  enabled: boolean
  /** 'all' = 全部节点；否则为绑定的 Agent id 列表 */
  agents: 'all' | string[]
  createdAt: number
}

export interface NotifyLog {
  id: string | number
  time: number
  channelName: string
  channelType: NotifyChannelType
  agentName: string
  message: string
  status: 'sent' | 'failed' | 'test'
}

export interface AuditEntry {
  id: string | number
  time: number
  user: string
  action: string
  detail: string
}

export interface ConfigRevision {
  id: string
  version: number
  time: number
  operator: string
  changes: string[]
  targets: { agentName: string; status: 'success' | 'pending' | 'failed' }[]
}

export interface UserAccount {
  id: string
  name: string
  email: string
  role: Role
  active: boolean
  lastLogin: number
}

export interface GeoIPInfo {
  dbPath: string
  version: string
  updatedAt: number
  entryCount: number
}

export interface PendingChange {
  id: string | number
  time: number
  text: string
}

// ---- 系统设置分组 ----
export interface SystemSettings {
  dashboardName: string
  sessionTimeoutMin: number
  netDefaults: { dns: string; port: number; mtu: number; keepalive: number; defaultTopology: Topology }
  statusRules: { agentOfflineSec: number; handshakeSec: number; redFailCount: number }
  collect: { reportSec: number; probeSec: number; mapRefreshSec: number }
  retention: { rawDays: number; hourlyDays: number; dailyDays: number }
  agent: { token: string; labels: string; upgradePolicy: 'manual' | 'auto-stable' }
}

export const stateMeta: Record<PeerState, { label: string; color: string; text: string }> = {
  ok: { label: '正常', color: '#34d399', text: 'text-emerald-400' },
  degraded: { label: '波动', color: '#fbbf24', text: 'text-amber-400' },
  down: { label: '异常', color: '#f87171', text: 'text-red-400' },
  unknown: { label: '未知', color: '#64748b', text: 'text-slate-500' },
}
