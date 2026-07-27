import { defineStore } from 'pinia'
import { ApiError, api, type ApiAudit, type ApiConfigPublishResult, type ApiDelivery, type ApiNetwork, type ApiNode } from '../api'
import type {
  Agent, AuditEntry, ConfigRevision, FeedEvent, GeoIPInfo, Network, NotifyChannel, NotifyLog,
  PendingChange, PeerLink, Project, TempPeer, UserAccount, WGInterface, PeerState,
} from '../types'
import { useAppStore } from './app'

let pollingTimer: number | undefined
const trafficSamples = new Map<string, { time: number; receiveBytes: number; transmitBytes: number }>()
const immediateCollectTimeoutMs = 10_000
const immediateCollectPollMs = 400
const immediateDeliveryTimeoutMs = 15_000
const immediateDeliveryPollMs = 500

function sleep(milliseconds: number) {
  return new Promise<void>((resolve) => window.setTimeout(resolve, milliseconds))
}

function timestamp(value?: string | null) {
  if (!value || value.startsWith('0001-')) return 0
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? parsed : 0
}
function topology(value: ApiNetwork['topology']): Network['topology'] {
  return value === 'full_mesh' ? 'full-mesh' : value === 'hub_spoke' ? 'hub-spoke' : 'custom'
}
function apiTopology(value: Network['topology']): ApiNetwork['topology'] {
  return value === 'full-mesh' ? 'full_mesh' : value === 'hub-spoke' ? 'hub_spoke' : 'custom'
}

function nodeRole(node: ApiNode): WGInterface['role'] {
  const value = node.labels?.['wiremesh.role']
  return value === 'hub' || value === 'spoke' || value === 'mesh' ? value : 'mesh'
}
function toAgent(node: ApiNode, offlineSeconds: number): Agent {
  const seen = timestamp(node.last_seen)
  const online = seen > 0 && Date.now() - seen <= offlineSeconds * 1000
  const observed = node.wireguard || []
  const receiveBytes = observed.flatMap((iface) => iface.peers || []).reduce((total, peer) => total + (peer.receive_bytes || 0), 0)
  const transmitBytes = observed.flatMap((iface) => iface.peers || []).reduce((total, peer) => total + (peer.transmit_bytes || 0), 0)
  const sampleTime = Date.now()
  const previousSample = trafficSamples.get(node.id)
  const elapsedSeconds = previousSample ? (sampleTime - previousSample.time) / 1000 : 0
  const rxMbps = previousSample && elapsedSeconds > 0 ? Math.max(0, receiveBytes - previousSample.receiveBytes) * 8 / elapsedSeconds / 1_000_000 : 0
  const txMbps = previousSample && elapsedSeconds > 0 ? Math.max(0, transmitBytes - previousSample.transmitBytes) * 8 / elapsedSeconds / 1_000_000 : 0
  trafficSamples.set(node.id, { time: sampleTime, receiveBytes, transmitBytes })
  return {
    id: node.id,
    projectId: node.project_id,
    networkId: node.network_id,
    address: node.address || '',
    publicKey: node.public_key || '',
    name: node.name,
    hostname: node.hostname || node.name,
    interfaceSelector: node.interface_selector || '',
    collectionError: node.collection_error || '',
    listenPort: node.listen_port || observed[0]?.listen_port || 51820,
    mtu: node.mtu || observed[0]?.mtu || 1420,
    status: online ? 'online' : 'offline',
    enabled: node.enabled !== false,
    version: node.agent_version || '',
    osInfo: node.os || '',
    labels: Object.entries(node.labels || {}).map(([key, value]) => key + '=' + value),
    publicIP: node.endpoint || '',
    city: node.location_name || node.region || '',
    country: '',
    locationSource: node.location_source || '',
    lng: node.location_source && Number.isFinite(node.longitude) ? node.longitude : Number.NaN,
    lat: node.location_source && Number.isFinite(node.latitude) ? node.latitude : Number.NaN,
    lastSeen: seen,
    rxMbps,
    txMbps,
    totalRxGB: receiveBytes / 1024 / 1024 / 1024,
    totalTxGB: transmitBytes / 1024 / 1024 / 1024,
    interfaces: observed.map((iface) => ({
      id: node.id + ':' + iface.name,
      agentId: node.id,
      networkId: node.network_id,
      name: iface.name,
      listenPort: iface.listen_port || 0,
      mtu: iface.mtu || 0,
      publicKey: iface.public_key || '',
      tunnelIP: iface.addresses?.[0]?.split('/')[0] || '',
      role: nodeRole(node),
      addresses: iface.addresses || [],
      up: Boolean(iface.up),
      peers: (iface.peers || []).map((peer) => ({
        publicKey: peer.public_key,
        endpoint: peer.endpoint || '',
        allowedIPs: peer.allowed_ips || [],
        latestHandshake: timestamp(peer.latest_handshake_at),
        receiveBytes: peer.receive_bytes || 0,
        transmitBytes: peer.transmit_bytes || 0,
        persistentKeepalive: peer.persistent_keepalive || 0,
        locationName: peer.location_name || '',
        lng: Number.isFinite(peer.longitude) ? peer.longitude : undefined,
        lat: Number.isFinite(peer.latitude) ? peer.latitude : undefined,
      })),
    })),
  }
}
function observedPeerState(secondsAgo: number, handshakeThreshold: number): PeerState {
  if (secondsAgo < 0) return 'unknown'
  if (secondsAgo > handshakeThreshold) return 'down'
  if (secondsAgo > handshakeThreshold / 2) return 'degraded'
  return 'ok'
}
function observedTopology(agents: Agent[], handshakeThreshold: number): { links: PeerLink[]; tempPeers: TempPeer[] } {
  const interfaceByKey = new Map<string, WGInterface>()
  agents.forEach((agent) => agent.interfaces.forEach((iface) => {
    if (iface.publicKey) interfaceByKey.set(iface.publicKey, iface)
  }))
  const links = new Map<string, PeerLink>()
  const tempPeers: TempPeer[] = []
  const now = Date.now()
  agents.forEach((agent) => agent.interfaces.forEach((source) => source.peers.forEach((peer) => {
    const handshakeSeconds = peer.latestHandshake > 0 ? Math.max(0, Math.floor((now - peer.latestHandshake) / 1000)) : -1
    const target = interfaceByKey.get(peer.publicKey)
    if (!target || target.id === source.id) {
      const hasGeo = Number.isFinite(peer.lng) && Number.isFinite(peer.lat) && (peer.lng !== 0 || peer.lat !== 0)
      tempPeers.push({
        id: source.id + ':' + peer.publicKey,
        publicKey: peer.publicKey,
        endpoint: peer.endpoint,
        allowedIPs: peer.allowedIPs.join(', '),
        sourceIfaceId: source.id,
        lastHandshakeSecAgo: handshakeSeconds,
        rxMB: peer.receiveBytes / 1024 / 1024,
        txMB: peer.transmitBytes / 1024 / 1024,
        firstSeen: peer.latestHandshake || agent.lastSeen,
        geo: hasGeo ? { city: peer.locationName || '', country: '', lng: peer.lng as number, lat: peer.lat as number } : undefined,
      })
      return
    }
    const endpoints = [source.id, target.id].sort()
    const id = 'observed:' + endpoints.join(':')
    const current = links.get(id)
    if (!current || current.lastHandshakeSecAgo < 0 || (handshakeSeconds >= 0 && handshakeSeconds < current.lastHandshakeSecAgo)) {
      links.set(id, {
        id,
        networkId: source.networkId,
        a: source.id,
        b: target.id,
        state: observedPeerState(handshakeSeconds, handshakeThreshold),
        latencyMs: 0,
        lossPct: 0,
        lastHandshakeSecAgo: handshakeSeconds,
        rxMbps: 0,
        txMbps: 0,
        singleSide: true,
      })
    } else if (current) {
      current.singleSide = false
    }
  })))
  return { links: [...links.values()], tempPeers }
}
function telemetryFromNodes(nodes: ApiNode[], offlineSeconds: number, handshakeSeconds: number) {
  const agents = nodes.map((node) => toAgent(node, offlineSeconds))
  const observed = observedTopology(agents, handshakeSeconds)
  return { agents, links: observed.links, tempPeers: observed.tempPeers }
}

function deliveryStatus(state: string): 'success' | 'pending' | 'failed' {
  if (state === 'applied') return 'success'
  if (state === 'pending') return 'pending'
  return 'failed'
}
function revisionsFrom(deliveries: ApiDelivery[], agents: Agent[], operator: string): ConfigRevision[] {
  const groups = new Map<number, ApiDelivery[]>()
  deliveries.forEach((delivery) => groups.set(delivery.version, [...(groups.get(delivery.version) || []), delivery]))
  return [...groups.entries()].sort((a, b) => b[0] - a[0]).map(([version, rows]) => ({
    id: 'delivery-v' + version,
    version,
    time: Math.max(...rows.map((row) => timestamp(row.updated_at))),
    operator,
    changes: [],
    targets: rows.map((row) => ({ agentName: agents.find((agent) => agent.id === row.node_id)?.name || row.node_id, status: deliveryStatus(row.state) })),
  }))
}
type AuditLookup = {
  agents?: Agent[]
  users?: UserAccount[]
  projects?: Project[]
  networks?: Network[]
  notifyChannels?: NotifyChannel[]
}

function compactID(value: string) {
  if (!value) return ''
  if (value.length <= 18) return value
  return value.slice(0, 10) + '…' + value.slice(-4)
}

function commandLabel(value?: string) {
  const labels: Record<string, string> = {
    collect: '立即采集状态',
    collect_all: '批量立即采集',
    apply_config: '应用 WireGuard 配置',
    apply_peer_config: '应用 Peer 配置',
    connectivity_check: '连通性检测',
  }
  return value ? (labels[value] || value) : ''
}

function auditActionLabel(row: ApiAudit) {
  const meta = row.metadata || {}
  const action = row.action
  const labels: Record<string, string> = {
    'auth.login': '用户登录',
    'project.create': '创建项目',
    'network.create': '创建网络',
    'node.create': '创建节点',
    'node.update': '更新节点设置',
    'node.delete': '删除节点',
    'peer.create': '创建 Peer 关系',
    'config.publish': '发布网络配置',
    'config.publish.noop': '发布网络配置（无变化）',
    'config.publish.auto': '保存后自动下发配置',
    'config.publish.auto.noop': '保存后自动下发（无变化）',
    'agent.enroll': 'Agent 接入',
    'agent.enrollment_token.create': '创建 Agent 接入令牌',
    'agent.config.read': 'Agent 拉取配置',
    'agent.config.applied': 'Agent 应用配置成功',
    'agent.config.failed': 'Agent 应用配置失败',
    'agent.config.rolled_back': 'Agent 配置回滚',
    'agent.config.observed': '采集到接口配置',
    'agent.command.collect': '下发立即采集',
    'agent.command.connectivity_check': '下发连通性检测',
    'agent.command.collect_all': '批量下发立即采集',
    'agent.command.completed': meta.type ? 'Agent 指令完成：' + commandLabel(meta.type) : 'Agent 指令完成',
    'agent.command.failed': meta.type ? 'Agent 指令失败：' + commandLabel(meta.type) : 'Agent 指令失败',
    'agent.peer_config.save': '保存并下发 Peer 配置',
    'agent.peer_config.read': 'Agent 拉取 Peer 配置',
    'settings.update': '更新系统设置',
    'geoip.update': '更新 GeoIP 数据库',
    'geoip.reload': '重载 GeoIP 数据库',
    'user.create': '创建用户',
    'notification.create': '创建通知渠道',
    'notification.update': '更新通知渠道',
    'notification.delete': '删除通知渠道',
    'notification.test': '测试通知渠道',
  }
  return labels[action] || action
}

function auditResourceLabel(row: ApiAudit, lookup: AuditLookup) {
  const id = row.resource_id
  switch (row.resource_type) {
    case 'node': {
      const agent = lookup.agents?.find((item) => item.id === id)
      return '节点：' + (agent?.name || compactID(id))
    }
    case 'network': {
      const network = lookup.networks?.find((item) => item.id === id)
      return '网络：' + (network?.name || compactID(id))
    }
    case 'project': {
      const project = lookup.projects?.find((item) => item.id === id)
      return '项目：' + (project?.name || compactID(id))
    }
    case 'user': {
      const user = lookup.users?.find((item) => item.id === id)
      return '用户：' + (user ? `${user.name} <${user.email}>` : compactID(id))
    }
    case 'notification_channel': {
      const channel = lookup.notifyChannels?.find((item) => item.id === id)
      return '通知渠道：' + (channel?.name || compactID(id))
    }
    case 'settings':
      return '系统设置'
    case 'tenant':
      return '当前租户'
    default:
      return (row.resource_type || '资源') + '：' + compactID(id)
  }
}

function auditActorLabel(actorID: string, lookup: AuditLookup) {
  const user = lookup.users?.find((item) => item.id === actorID)
  if (user) return user.name || user.email
  const agent = lookup.agents?.find((item) => item.id === actorID)
  if (agent) return 'Agent：' + agent.name
  if (!actorID) return '系统'
  if (actorID.startsWith('node_')) return 'Agent：' + compactID(actorID)
  if (actorID.startsWith('user_')) return '用户：' + compactID(actorID)
  return compactID(actorID)
}

function auditMetadataText(row: ApiAudit) {
  const meta = row.metadata || {}
  const parts: string[] = []
  if (meta.version) parts.push('版本 v' + meta.version)
  if (meta.count) parts.push('数量 ' + meta.count)
  if (meta.changed_nodes) parts.push('变更节点 ' + meta.changed_nodes)
  if (meta.offline_nodes) parts.push('离线节点 ' + meta.offline_nodes)
  if (meta.address) parts.push('隧道地址 ' + meta.address)
  if (meta.interface) parts.push('接口 ' + meta.interface)
  if (meta.listen_port) parts.push('监听端口 ' + meta.listen_port)
  if (meta.mtu) parts.push('MTU ' + meta.mtu)
  if (meta.enabled) parts.push(meta.enabled === 'true' ? '已启用' : '已停用')
  if (meta.role) parts.push('角色 ' + meta.role)
  if (meta.type && !row.action.startsWith('agent.command.')) parts.push('类型 ' + meta.type)
  if (meta.status) parts.push('状态 ' + meta.status)
  if (meta.files) parts.push('文件数 ' + meta.files)
  if (meta.offline === 'true') parts.push('客户端离线，等待上线下发')
  return parts.join(' · ')
}

function auditEntries(rows: ApiAudit[], lookup: AuditLookup = {}): AuditEntry[] {
  return rows.map((row) => {
    const metadata = auditMetadataText(row)
    return {
      id: row.id,
      time: timestamp(row.created_at),
      user: auditActorLabel(row.actor_id, lookup),
      action: auditActionLabel(row),
      detail: auditResourceLabel(row, lookup) + (metadata ? ' · ' + metadata : ''),
    }
  })
}
function feedEntries(audit: ApiAudit[], deliveries: ApiDelivery[]): FeedEvent[] {
  if (audit.length) return audit.slice(0, 80).map((row) => ({ id: row.id, time: timestamp(row.created_at), kind: row.action.includes('publish') ? 'publish' : row.action.includes('login') ? 'system' : 'report', message: row.action + ' · ' + row.resource_type + ' · ' + row.resource_id }))
  return deliveries.slice(0, 80).map((row) => ({ id: row.id, time: timestamp(row.updated_at), kind: row.state === 'failed' ? 'alert' : 'report', message: '配置 v' + row.version + ' · ' + row.node_id + ' · ' + row.state }))
}

function deliveryTargets(result?: ApiConfigPublishResult) {
  return result?.queued_node_ids || []
}
function deliverySummaryMessage(prefix: string, result: ApiConfigPublishResult | undefined, status: { applied: number; failed: number; pending: number; expected: number }) {
  if (!result) return prefix
  const offline = result.offline_node_ids?.length || 0
  if (result.unchanged && status.expected === 0) return prefix + '，WireGuard 配置未变化，无需下发'
  if (status.failed > 0) return prefix + '，但 ' + status.failed + ' 个节点应用失败，请在下发记录或 Agent 日志中查看原因'
  if (status.pending > 0) {
    const offlineText = offline > 0 ? '，其中 ' + offline + ' 个节点当前离线' : ''
    return prefix + '，配置 v' + result.version + ' 已排队下发；' + status.applied + '/' + status.expected + ' 个节点已确认' + offlineText
  }
  return prefix + '，配置 v' + result.version + ' 已下发并被 ' + status.applied + ' 个节点确认'
}

export const useMeshStore = defineStore('mesh', {
  state: () => ({
    projects: [] as Project[],
    networks: [] as Network[],
    agents: [] as Agent[],
    links: [] as PeerLink[],
    tempPeers: [] as TempPeer[],
    feed: [] as FeedEvent[],
    audit: [] as AuditEntry[],
    auditHasMore: false,
    auditOffset: 0,
    auditLoading: false,
    users: [] as UserAccount[],
    geoip: { dbPath: '', version: '', updatedAt: 0, entryCount: 0 } as GeoIPInfo,
    notifyChannels: [] as NotifyChannel[],
    notifyLogs: [] as NotifyLog[],
    revisions: [] as ConfigRevision[],
    pendingChanges: [] as PendingChange[],
    selectedProjectId: 'all',
    selectedNetworkId: 'all',
    autoRefresh: true,
    onlyErrors: false,
    linkFilter: 'all' as 'all' | PeerState,
    collecting: false,
    loading: false,
    error: '',
    notice: '',
    lastUpdated: 0,
  }),
  getters: {
    networkById: (s) => (id: string) => s.networks.find((n) => n.id === id),
    agentById: (s) => (id: string) => s.agents.find((a) => a.id === id),
    ifaceById: (s) => (id: string) => s.agents.flatMap((a) => a.interfaces).find((i) => i.id === id),
    ifaceWithAgent: (s) => (id: string) => {
      for (const agent of s.agents) { const iface = agent.interfaces.find((item) => item.id === id); if (iface) return { iface, agent } }
      return undefined
    },
    scopedNetworkIds(s): Set<string> {
      let rows = s.networks
      if (s.selectedProjectId !== 'all') rows = rows.filter((n) => n.projectId === s.selectedProjectId)
      if (s.selectedNetworkId !== 'all') rows = rows.filter((n) => n.id === s.selectedNetworkId)
      return new Set(rows.map((n) => n.id))
    },
    scopedAgents(s): Agent[] {
      return s.agents.filter((agent) => (
        (s.selectedProjectId === 'all' || agent.projectId === s.selectedProjectId)
        && (s.selectedNetworkId === 'all' || agent.networkId === s.selectedNetworkId || agent.interfaces.some((iface) => iface.networkId === s.selectedNetworkId))
      ))
    },
    scopedLinks(s): (PeerLink & { displayState: PeerState })[] {
      return s.links.filter((link) => this.scopedNetworkIds.has(link.networkId)).map((link) => ({ ...link, displayState: link.state }))
    },
    scopedTempPeers(s): TempPeer[] { return s.tempPeers.filter((peer) => this.scopedNetworkIds.has(this.ifaceById(peer.sourceIfaceId)?.networkId || '')) },
    stats(): { agentTotal: number; agentOnline: number; ifaceCount: number; linkOk: number; linkBad: number; linkDown: number; linkUnknown: number; rx: number; tx: number; tempCount: number } {
      const agents = this.scopedAgents
      const links = this.scopedLinks
      return {
        agentTotal: agents.length,
        agentOnline: agents.filter((agent) => agent.status === 'online').length,
        ifaceCount: agents.reduce((count, agent) => count + agent.interfaces.filter((iface) => this.scopedNetworkIds.has(iface.networkId)).length, 0),
        linkOk: links.filter((link) => link.displayState === 'ok').length,
        linkBad: links.filter((link) => link.displayState === 'degraded' || link.displayState === 'down').length,
        // 仅真实异常（down），用于侧边栏徽章与顶部异常提示；波动不计入。
        linkDown: links.filter((link) => link.displayState === 'down').length,
        linkUnknown: links.filter((link) => link.displayState === 'unknown').length,
        rx: agents.reduce((count, agent) => count + agent.rxMbps, 0),
        tx: agents.reduce((count, agent) => count + agent.txMbps, 0),
        tempCount: this.scopedTempPeers.length,
      }
    },
    recentAlerts(s): FeedEvent[] { return s.feed.filter((item) => item.kind === 'alert').slice(0, 8) },
  },
  actions: {
    setProject(id: string) { this.selectedProjectId = id; this.selectedNetworkId = 'all' },
    clearMessage() { this.error = ''; this.notice = '' },
    unsupported(feature: string) { this.error = feature + '：当前 WireMesh 后端尚未提供对应 API，页面不会创建本地伪造数据。' },
    async refresh() {
      const app = useAppStore()
      if (!app.authed || this.loading) return
      this.loading = true
      this.error = ''
      try {
        const failures: unknown[] = []
        const [nodesResult, projectsResult] = await Promise.allSettled([api.nodes(), api.projects()] as const)
        if (nodesResult.status === 'rejected') throw nodesResult.reason

        const telemetry = telemetryFromNodes(
          nodesResult.value,
          app.settings.statusRules.agentOfflineSec,
          app.settings.statusRules.handshakeSec,
        )
        this.agents = telemetry.agents
        this.links = telemetry.links
        this.tempPeers = telemetry.tempPeers

        if (projectsResult.status === 'fulfilled') {
          const projects = projectsResult.value
          this.projects = projects.map((project) => ({ id: project.id, name: project.name, desc: project.description || '' }))
          const networkResults = await Promise.allSettled(projects.map((project) => api.networks(project.id)))
          this.networks = networkResults.flatMap((result) => result.status === 'fulfilled' ? result.value : []).map((network) => ({ id: network.id, projectId: network.project_id, name: network.name, cidr: network.cidr, topology: topology(network.topology), customPairs: [] }))
          failures.push(...networkResults.filter((result) => result.status === 'rejected').map((result) => result.reason))
        } else {
          failures.push(projectsResult.reason)
          this.projects = []
          this.networks = []
          this.selectedProjectId = 'all'
          this.selectedNetworkId = 'all'
        }

        const [deliveriesResult, geoipResult, channelsResult, logsResult, usersResult, auditsResult] = await Promise.allSettled([
          api.deliveries(), api.geoIPStatus(), api.notificationChannels(), api.notificationLogs(),
          app.isAdmin ? api.users() : Promise.resolve(app.user ? [app.user] : []),
          app.isAdmin ? api.audit() : Promise.resolve({ items: [], limit: 50, offset: 0, has_more: false }),
        ] as const)
        const optionalResults = [deliveriesResult, geoipResult, channelsResult, logsResult, usersResult, auditsResult]
        failures.push(...optionalResults.filter((result) => result.status === 'rejected').map((result) => result.reason))

        const deliveries = deliveriesResult.status === 'fulfilled' ? deliveriesResult.value : []
        const users = usersResult.status === 'fulfilled'
          ? usersResult.value.map((user) => ({ id: user.id, name: user.name, email: user.email, role: user.role, active: true, lastLogin: timestamp(user.last_login_at) }))
          : this.users
        const notifyChannels = channelsResult.status === 'fulfilled'
          ? channelsResult.value.map((channel) => ({ id: channel.id, name: channel.name, type: channel.type, config: channel.config, template: channel.template, subjectTemplate: channel.subjectTemplate, enabled: channel.enabled, agents: channel.agents, createdAt: timestamp(channel.createdAt) }))
          : this.notifyChannels
        const auditPage = auditsResult.status === 'fulfilled' ? auditsResult.value : { items: [], limit: 50, offset: 0, has_more: false }
        const audits = auditPage.items
        if (usersResult.status === 'fulfilled') this.users = users
        if (channelsResult.status === 'fulfilled') this.notifyChannels = notifyChannels
        if (auditsResult.status === 'fulfilled') {
          this.audit = auditEntries(audits, { agents: this.agents, users, networks: this.networks, projects: this.projects, notifyChannels })
          this.auditOffset = audits.length
          this.auditHasMore = auditPage.has_more
        }
        if (deliveriesResult.status === 'fulfilled' || auditsResult.status === 'fulfilled') this.feed = feedEntries(audits, deliveries)
        if (deliveriesResult.status === 'fulfilled') this.revisions = revisionsFrom(deliveries, this.agents, app.username)
        if (geoipResult.status === 'fulfilled') {
          const geoip = geoipResult.value
          this.geoip = { dbPath: geoip.dbPath || '', version: geoip.version || '', updatedAt: timestamp(geoip.updatedAt), entryCount: geoip.entryCount || 0 }
        }
        if (logsResult.status === 'fulfilled') this.notifyLogs = logsResult.value.map((log) => ({ id: log.id, time: timestamp(log.createdAt), channelName: log.channelName, channelType: log.channelType, agentName: log.agentName, message: log.message, status: log.status }))
        this.pendingChanges = []
        if (this.selectedProjectId !== 'all' && !this.projects.some((project) => project.id === this.selectedProjectId)) this.selectedProjectId = 'all'
        if (this.selectedNetworkId !== 'all' && !this.networks.some((network) => network.id === this.selectedNetworkId)) this.selectedNetworkId = 'all'
        this.lastUpdated = Date.now()

        const authFailure = failures.find((reason) => reason instanceof ApiError && reason.status === 401)
        if (authFailure) app.logout()
        else if (failures.length) {
          const first = failures[0]
          this.error = '节点列表已加载，但部分辅助数据同步失败：' + (first instanceof Error ? first.message : '未知错误')
        }
      } catch (reason) {
        if (reason instanceof ApiError && reason.status === 401) app.logout()
        this.error = reason instanceof Error ? reason.message : '同步失败'
      } finally { this.loading = false }
    },
    async refreshNodeTelemetry(silent = false) {
      const app = useAppStore()
      if (!app.authed) return false
      try {
        const nodes = await api.nodes()
        const telemetry = telemetryFromNodes(
          nodes,
          app.settings.statusRules.agentOfflineSec,
          app.settings.statusRules.handshakeSec,
        )
        this.agents = telemetry.agents
        this.links = telemetry.links
        this.tempPeers = telemetry.tempPeers
        this.lastUpdated = Date.now()
        return true
      } catch (reason) {
        if (reason instanceof ApiError && reason.status === 401) app.logout()
        else if (!silent) this.error = reason instanceof Error ? reason.message : '刷新节点状态失败'
        return false
      }
    },
    async waitForCollectedTelemetry(nodeIDs: string[], previousLastSeen: Map<string, number>) {
      const onlineIDs = nodeIDs.filter((id) => {
        const agent = this.agentById(id)
        return agent?.enabled && agent.status === 'online'
      })
      if (!onlineIDs.length) {
        await this.refreshNodeTelemetry(true)
        return { received: 0, expected: 0 }
      }

      const receivedCount = () => onlineIDs.filter((id) => (this.agentById(id)?.lastSeen || 0) > (previousLastSeen.get(id) || 0)).length
      const deadline = Date.now() + immediateCollectTimeoutMs
      let received = 0
      do {
        await sleep(immediateCollectPollMs)
        await this.refreshNodeTelemetry(true)
        received = receivedCount()
        if (received === onlineIDs.length) break
      } while (Date.now() < deadline)
      return { received, expected: onlineIDs.length }
    },
    async waitForDeliveryResult(result?: ApiConfigPublishResult) {
      const targets = deliveryTargets(result)
      if (!result || !targets.length) return { applied: 0, failed: 0, pending: 0, expected: 0 }
      if ((result.offline_node_ids?.length || 0) >= targets.length) return { applied: 0, failed: 0, pending: targets.length, expected: targets.length }
      const deadline = Date.now() + immediateDeliveryTimeoutMs
      let summary = { applied: 0, failed: 0, pending: targets.length, expected: targets.length }
      do {
        await sleep(immediateDeliveryPollMs)
        const deliveries = await api.deliveries()
        const rows = deliveries.filter((delivery) => delivery.version === result.version && targets.includes(delivery.node_id))
        const applied = rows.filter((delivery) => delivery.state === 'applied').length
        const failed = rows.filter((delivery) => delivery.state === 'failed' || delivery.state === 'rolled_back').length
        summary = { applied, failed, pending: Math.max(0, targets.length - applied - failed), expected: targets.length }
        if (summary.pending === 0) break
      } while (Date.now() < deadline)
      return summary
    },
    startPolling() {
      void this.refresh()
      if (pollingTimer) return
      pollingTimer = window.setInterval(() => { if (this.autoRefresh) void this.refresh() }, 30000)
    },
    stopPolling() { if (pollingTimer) window.clearInterval(pollingTimer); pollingTimer = undefined },
    async loadAuditPage(reset = false) {
      const app = useAppStore()
      if (!app.authed || !app.isAdmin || this.auditLoading) return false
      const offset = reset ? 0 : this.auditOffset
      if (!reset && !this.auditHasMore) return true
      this.auditLoading = true
      try {
        const page = await api.audit(50, offset)
        const entries = auditEntries(page.items, { agents: this.agents, users: this.users, networks: this.networks, projects: this.projects, notifyChannels: this.notifyChannels })
        this.audit = reset ? entries : [...this.audit, ...entries]
        this.auditOffset = offset + entries.length
        this.auditHasMore = page.has_more
        return true
      } catch (reason) {
        this.error = reason instanceof Error ? reason.message : '加载审计日志失败'
        return false
      } finally {
        this.auditLoading = false
      }
    },
    async clearAudit() {
      this.error = ''
      try {
        await api.clearAudit()
        this.audit = []
        this.auditOffset = 0
        this.auditHasMore = false
        this.notice = '审计日志已清空'
        return true
      } catch (reason) {
        this.error = reason instanceof Error ? reason.message : '清空审计日志失败'
        return false
      }
    },
    async publish(_user?: string) {
      if (this.selectedNetworkId === 'all') { this.error = '请先选择一个网络'; return }
      try {
        const result = await api.publish(this.selectedNetworkId)
        const status = await this.waitForDeliveryResult(result)
        this.notice = deliverySummaryMessage('配置已发布', result, status)
        await this.refresh()
      } catch (reason) { this.error = reason instanceof Error ? reason.message : '发布失败' }
    },
    async createEnrollment(projectId: string, networkId: string, ttlMinutes = 30) { return api.createEnrollment(projectId, networkId, ttlMinutes) },
    async addProject(payload: { name: string; desc: string }, _user?: string) {
      try {
        await api.createProject({ name: payload.name, description: payload.desc })
        await this.refresh()
        this.notice = '项目已创建'
        return true
      } catch (reason) {
        this.error = reason instanceof Error ? reason.message : '创建项目失败'
        return false
      }
    },
    async addNetwork(payload: { projectId: string; name: string; cidr: string; topology: Network['topology'] }, _user?: string) {
      try {
        await api.createNetwork({ project_id: payload.projectId, name: payload.name, cidr: payload.cidr, dns: useAppStore().settings.netDefaults.dns, topology: apiTopology(payload.topology) })
        await this.refresh()
        this.notice = '网络已创建'
        return true
      } catch (reason) {
        this.error = reason instanceof Error ? reason.message : '创建网络失败'
        return false
      }
    },
    async setCustomPairs(networkId: string, pairs: [string, string][], _user?: string) {
      this.error = ''
      try {
        for (const [sourceNode, targetNode] of pairs) {
          if (sourceNode !== targetNode) await api.addPeer(networkId, sourceNode, targetNode)
        }
      } catch (reason) { this.error = reason instanceof Error ? reason.message : '保存 Peer 失败'; return false }
      try {
        const result = await api.publish(networkId)
        const status = await this.waitForDeliveryResult(result)
        this.notice = deliverySummaryMessage('自定义 Peer 已保存', result, status)
        await this.refresh()
        return true
      } catch (reason) {
        this.notice = '自定义 Peer 已保存，但自动下发失败：' + (reason instanceof Error ? reason.message : '未知错误')
        await this.refresh()
        return true
      }
    },
    async updateNodeConfig(id: string, patch: { name?: string; address?: string; endpoint?: string; listen_port?: number; mtu?: number; enabled?: boolean; interface_selector?: string; labels?: Record<string, string>; location_name?: string; location_source?: string; latitude?: number; longitude?: number }) {
      this.error = ''
      try {
        const saved = await api.updateNode(id, patch)
        const status = await this.waitForDeliveryResult(saved.delivery)
        await this.refresh()
        this.notice = saved.delivery_error
          ? '节点配置已保存，但自动下发失败：' + saved.delivery_error
          : deliverySummaryMessage('节点配置已保存', saved.delivery, status)
        return true
      } catch (reason) { this.error = reason instanceof Error ? reason.message : '保存节点配置失败'; return false }
    },
    async updateNodeAndPublish(id: string, patch: { name?: string; address?: string; endpoint?: string; listen_port?: number; mtu?: number; enabled?: boolean; interface_selector?: string; labels?: Record<string, string>; location_name?: string; location_source?: string; latitude?: number; longitude?: number }) {
      this.error = ''
      try {
        const saved = await api.updateNode(id, patch)
        const status = await this.waitForDeliveryResult(saved.delivery)
        await this.refresh()
        this.notice = saved.delivery_error
          ? '节点配置已保存，但自动下发失败：' + saved.delivery_error
          : deliverySummaryMessage('节点配置已保存', saved.delivery, status)
        return true
      } catch (reason) { this.error = reason instanceof Error ? reason.message : '保存节点配置失败'; return false }
    },
    async collectNow(id: string) {
      this.error = ''
      const previousLastSeen = new Map([[id, this.agentById(id)?.lastSeen || 0]])
      try {
        await api.collectNode(id)
        const result = await this.waitForCollectedTelemetry([id], previousLastSeen)
        this.notice = result.expected === 0
          ? '即时采集请求已发送；节点当前离线，恢复连接后会立即执行'
          : result.received === result.expected
            ? '已收到节点最新状态，界面已更新'
            : '即时采集请求已发送，但节点暂未在 10 秒内返回最新状态'
        return true
      }
      catch (reason) { this.error = reason instanceof Error ? reason.message : '下发采集命令失败'; return false }
    },
    async collectAll() {
      this.error = ''
      const knownLastSeen = new Map(this.agents.filter((agent) => agent.enabled).map((agent) => [agent.id, agent.lastSeen]))
      try {
        const result = await api.collectAllNodes()
        const targetIDs = result.node_ids?.length
          ? result.node_ids
          : this.agents.filter((agent) => agent.enabled).map((agent) => agent.id)
        const previousLastSeen = new Map(targetIDs.map((id) => [id, knownLastSeen.get(id) || 0]))
        const received = await this.waitForCollectedTelemetry(targetIDs, previousLastSeen)
        this.notice = result.created === 0
          ? '没有可即时采集的已启用节点'
          : received.expected === 0
            ? '已向 ' + result.created + ' 个节点发送即时采集请求；当前没有在线节点'
            : received.received === received.expected
              ? '已收到 ' + received.received + ' 个在线节点的最新状态，界面已更新'
              : '已收到 ' + received.received + '/' + received.expected + ' 个在线节点的最新状态，其余节点仍在等待回传'
        return true
      } catch (reason) { this.error = reason instanceof Error ? reason.message : '强制上报下发失败'; return false }
    },
    async checkConnectivity(id: string) {
      this.error = ''
      try { await api.checkNodeConnectivity(id); this.notice = '连通性检测命令已下发，可稍后在 Agent 日志中查看结果'; return true }
      catch (reason) { this.error = reason instanceof Error ? reason.message : '下发连通性检测失败'; return false }
    },
    async clearAgentLogs(id: string) {
      this.error = ''
      try {
        await api.clearNodeLogs(id)
        this.notice = 'Agent 日志已清空'
        return true
      } catch (reason) {
        this.error = reason instanceof Error ? reason.message : '清空 Agent 日志失败'
        return false
      }
    },
    async toggleAgentEnabled(id: string, _user?: string) {
      const agent = this.agents.find((value) => value.id === id); if (!agent) return false
      return this.updateNodeConfig(id, { enabled: !agent.enabled })
    },
    async removeAgent(id: string, _user?: string) {
      this.error = ''
      try { await api.deleteNode(id); await this.refresh(); this.notice = 'Agent 已删除'; return true }
      catch (reason) { this.error = reason instanceof Error ? reason.message : '删除 Agent 失败'; return false }
    },
    discardPending(_user?: string) { this.pendingChanges = [] },
    adoptTempPeer(_id: string, _target: { projectId: string; networkId: string; agentId: string }, _user?: string) { this.unsupported('纳入临时 Peer') },
    removeTempPeer(_id: string, _user?: string) { this.unsupported('清理临时 Peer') },
    async reloadGeoIP(_user?: string) {
      this.error = ''
      try {
        const value = await api.reloadGeoIP()
        this.geoip = { dbPath: value.dbPath || '', version: value.version || '', updatedAt: timestamp(value.updatedAt), entryCount: value.entryCount || 0 }
        this.notice = 'GeoIP 数据库已重新加载'
        return true
      } catch (reason) { this.error = reason instanceof Error ? reason.message : 'GeoIP 重载失败'; return false }
    },
    async updateGeoDbPath(path: string, _user?: string) {
      this.error = ''
      try {
        const value = await api.updateGeoIP(path)
        this.geoip = { dbPath: value.dbPath || '', version: value.version || '', updatedAt: timestamp(value.updatedAt), entryCount: value.entryCount || 0 }
        this.notice = 'GeoIP 数据库路径已保存并加载'
        return true
      } catch (reason) { this.error = reason instanceof Error ? reason.message : 'GeoIP 数据库加载失败'; return false }
    },
    async lookupGeoIP(ip: string) {
      this.error = ''
      try { return await api.lookupGeoIP(ip) }
      catch (reason) { this.error = reason instanceof Error ? reason.message : 'GeoIP 查询失败'; return null }
    },
    pushEvent(_kind: FeedEvent['kind'], _message: string) {},
    pushAudit(_user: string, _action: string, _detail: string) {},
    async addNotifyChannel(payload: Omit<NotifyChannel, 'id' | 'createdAt'>, _user?: string) {
      this.error = ''
      try {
        await api.createNotificationChannel(payload)
        await this.refresh()
        this.notice = '通知渠道已添加'
        return true
      } catch (reason) { this.error = reason instanceof Error ? reason.message : '添加通知渠道失败'; return false }
    },
    async updateNotifyChannel(id: string, patch: Partial<NotifyChannel>, _user?: string) {
      const current = this.notifyChannels.find((channel) => channel.id === id)
      if (!current) { this.error = '通知渠道不存在'; return false }
      const value = { ...current, ...patch }
      this.error = ''
      try {
        await api.updateNotificationChannel(id, { name: value.name, type: value.type, config: value.config, template: value.template, subjectTemplate: value.subjectTemplate, enabled: value.enabled, agents: value.agents })
        await this.refresh()
        this.notice = '通知渠道已更新'
        return true
      } catch (reason) { this.error = reason instanceof Error ? reason.message : '更新通知渠道失败'; return false }
    },
    async removeNotifyChannel(id: string, _user?: string) {
      this.error = ''
      try { await api.deleteNotificationChannel(id); await this.refresh(); this.notice = '通知渠道已删除'; return true }
      catch (reason) { this.error = reason instanceof Error ? reason.message : '删除通知渠道失败'; return false }
    },
    async testNotifyChannel(id: string, _user?: string) {
      this.error = ''
      try { await api.testNotificationChannel(id); await this.refresh(); this.notice = '测试通知已发送'; return true }
      catch (reason) {
        this.error = reason instanceof Error ? reason.message : '通知渠道测试失败'
        try { this.notifyLogs = (await api.notificationLogs()).map((log) => ({ id: log.id, time: timestamp(log.createdAt), channelName: log.channelName, channelType: log.channelType, agentName: log.agentName, message: log.message, status: log.status })) } catch {}
        return false
      }
    },
    async addUser(payload: { name: string; email: string; password: string; role: UserAccount['role'] }) {
      this.error = ''
      try { await api.createUser(payload); await this.refresh(); this.notice = '用户已创建'; return true }
      catch (reason) { this.error = reason instanceof Error ? reason.message : '创建用户失败'; return false }
    },
  },
})
