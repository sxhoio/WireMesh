import { defineStore } from 'pinia'
import { ApiError, api, type ApiAudit, type ApiDelivery, type ApiNetwork, type ApiNode } from '../api'
import type {
  Agent, AuditEntry, ConfigRevision, FeedEvent, GeoIPInfo, Network, NotifyChannel, NotifyLog,
  PendingChange, PeerLink, Project, TempPeer, UserAccount, WGInterface, PeerState,
} from '../types'
import { useAppStore } from './app'

let pollingTimer: number | undefined
const trafficSamples = new Map<string, { time: number; receiveBytes: number; transmitBytes: number }>()

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
    status: online ? 'online' : 'offline',
    enabled: true,
    version: node.agent_version || '',
    osInfo: node.os || '',
    labels: Object.entries(node.labels || {}).map(([key, value]) => key + '=' + value),
    publicIP: node.endpoint || '',
    city: node.region || '',
    country: '',
    lng: Number.NaN,
    lat: Number.NaN,
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
function auditEntries(rows: ApiAudit[]): AuditEntry[] {
  return rows.map((row) => ({ id: row.id, time: timestamp(row.created_at), user: row.actor_id, action: row.action, detail: row.resource_type + ' · ' + row.resource_id }))
}
function feedEntries(audit: ApiAudit[], deliveries: ApiDelivery[]): FeedEvent[] {
  if (audit.length) return audit.slice(0, 80).map((row) => ({ id: row.id, time: timestamp(row.created_at), kind: row.action.includes('publish') ? 'publish' : row.action.includes('login') ? 'system' : 'report', message: row.action + ' · ' + row.resource_type + ' · ' + row.resource_id }))
  return deliveries.slice(0, 80).map((row) => ({ id: row.id, time: timestamp(row.updated_at), kind: row.state === 'failed' ? 'alert' : 'report', message: '配置 v' + row.version + ' · ' + row.node_id + ' · ' + row.state }))
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
    stats(): { agentTotal: number; agentOnline: number; ifaceCount: number; linkOk: number; linkBad: number; linkUnknown: number; rx: number; tx: number; tempCount: number } {
      const agents = this.scopedAgents
      const links = this.scopedLinks
      return {
        agentTotal: agents.length,
        agentOnline: agents.filter((agent) => agent.status === 'online').length,
        ifaceCount: agents.reduce((count, agent) => count + agent.interfaces.filter((iface) => this.scopedNetworkIds.has(iface.networkId)).length, 0),
        linkOk: links.filter((link) => link.displayState === 'ok').length,
        linkBad: links.filter((link) => link.displayState === 'degraded' || link.displayState === 'down').length,
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

        this.agents = nodesResult.value.map((node) => toAgent(node, app.settings.statusRules.agentOfflineSec))
        const observed = observedTopology(this.agents, app.settings.statusRules.handshakeSec)
        this.links = observed.links
        this.tempPeers = observed.tempPeers

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
          app.isAdmin ? api.audit() : Promise.resolve([]),
        ] as const)
        const optionalResults = [deliveriesResult, geoipResult, channelsResult, logsResult, usersResult, auditsResult]
        failures.push(...optionalResults.filter((result) => result.status === 'rejected').map((result) => result.reason))

        const deliveries = deliveriesResult.status === 'fulfilled' ? deliveriesResult.value : []
        const audits = auditsResult.status === 'fulfilled' ? auditsResult.value : []
        if (auditsResult.status === 'fulfilled') this.audit = auditEntries(audits)
        if (deliveriesResult.status === 'fulfilled' || auditsResult.status === 'fulfilled') this.feed = feedEntries(audits, deliveries)
        if (deliveriesResult.status === 'fulfilled') this.revisions = revisionsFrom(deliveries, this.agents, app.username)
        if (usersResult.status === 'fulfilled') this.users = usersResult.value.map((user) => ({ id: user.id, name: user.name, email: user.email, role: user.role, active: true, lastLogin: timestamp(user.last_login_at) }))
        if (geoipResult.status === 'fulfilled') {
          const geoip = geoipResult.value
          this.geoip = { dbPath: geoip.dbPath || '', version: geoip.version || '', updatedAt: timestamp(geoip.updatedAt), entryCount: geoip.entryCount || 0 }
        }
        if (channelsResult.status === 'fulfilled') this.notifyChannels = channelsResult.value.map((channel) => ({ id: channel.id, name: channel.name, type: channel.type, config: channel.config, template: channel.template, subjectTemplate: channel.subjectTemplate, enabled: channel.enabled, agents: channel.agents, createdAt: timestamp(channel.createdAt) }))
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
    startPolling() {
      void this.refresh()
      if (pollingTimer) return
      pollingTimer = window.setInterval(() => { if (this.autoRefresh) void this.refresh() }, 30000)
    },
    stopPolling() { if (pollingTimer) window.clearInterval(pollingTimer); pollingTimer = undefined },
    async publish(_user?: string) {
      if (this.selectedNetworkId === 'all') { this.error = '请先选择一个网络'; return }
      try {
        const result = await api.publish(this.selectedNetworkId)
        this.notice = '配置版本 v' + result.version + ' 已发布'
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
      try {
        for (const [sourceNode, targetNode] of pairs) {
          if (sourceNode !== targetNode) await api.addPeer(networkId, sourceNode, targetNode)
        }
        this.notice = '自定义 Peer 已提交到后端'
        await this.refresh()
      } catch (reason) { this.error = reason instanceof Error ? reason.message : '保存 Peer 失败' }
    },
    collectNow(_id: string) { void this.refresh() },
    checkConnectivity(_id: string) { this.unsupported('连通性检测') },
    toggleAgentEnabled(_id: string, _user?: string) { this.unsupported('启用或停用 Agent') },
    removeAgent(_id: string, _user?: string) { this.unsupported('删除 Agent') },
    updateInterface(_agentId: string, _ifaceId: string, _patch: { listenPort: number; mtu: number }, _user?: string) { this.unsupported('编辑接口') },
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
