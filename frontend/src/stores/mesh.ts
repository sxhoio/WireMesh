import { defineStore } from 'pinia'
import { ApiError, api, type ApiAudit, type ApiDelivery, type ApiNetwork, type ApiNode } from '../api'
import type {
  Agent, AuditEntry, ConfigRevision, FeedEvent, GeoIPInfo, Network, NotifyChannel, NotifyLog,
  PendingChange, PeerLink, Project, TempPeer, UserAccount, WGInterface, PeerState,
} from '../types'
import { useAppStore } from './app'

let pollingTimer: number | undefined

function timestamp(value?: string) {
  if (!value || value.startsWith('0001-')) return 0
  const parsed = Date.parse(value)
  return Number.isFinite(parsed) ? parsed : 0
}
function topology(value: ApiNetwork['topology']): Network['topology'] {
  return value === 'full_mesh' ? 'full-mesh' : value === 'hub_spoke' ? 'hub-spoke' : 'custom'
}
function endpointPort(value: string) {
  const match = value.match(/:(\d+)$/)
  return match ? Number(match[1]) : 0
}
function nodeRole(node: ApiNode): WGInterface['role'] {
  const value = node.labels?.['wiremesh.role']
  return value === 'hub' || value === 'spoke' || value === 'mesh' ? value : 'mesh'
}
function toAgent(node: ApiNode, offlineSeconds: number): Agent {
  const seen = timestamp(node.last_seen)
  const online = seen > 0 && Date.now() - seen <= offlineSeconds * 1000
  return {
    id: node.id,
    projectId: node.project_id,
    name: node.name,
    hostname: node.name,
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
    rxMbps: 0,
    txMbps: 0,
    totalRxGB: 0,
    totalTxGB: 0,
    interfaces: [{
      id: node.id + ':wg0',
      agentId: node.id,
      networkId: node.network_id,
      name: 'wg0',
      listenPort: endpointPort(node.endpoint),
      mtu: 0,
      publicKey: node.public_key || '',
      tunnelIP: node.address || '',
      role: nodeRole(node),
    }],
  }
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
      return s.agents.filter((agent) => (s.selectedProjectId === 'all' || agent.projectId === s.selectedProjectId) && agent.interfaces.some((iface) => this.scopedNetworkIds.has(iface.networkId)))
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
        const [projects, nodes, deliveries] = await Promise.all([api.projects(), api.nodes(), api.deliveries()])
        const networkGroups = await Promise.all(projects.map((project) => api.networks(project.id)))
        const audits = app.isAdmin ? await api.audit() : []
        this.projects = projects.map((project) => ({ id: project.id, name: project.name, desc: project.description || '' }))
        this.networks = networkGroups.flat().map((network) => ({ id: network.id, projectId: network.project_id, name: network.name, cidr: network.cidr, topology: topology(network.topology), customPairs: [] }))
        this.agents = nodes.map((node) => toAgent(node, app.settings.statusRules.agentOfflineSec))
        this.links = []
        this.tempPeers = []
        this.audit = auditEntries(audits)
        this.feed = feedEntries(audits, deliveries)
        this.revisions = revisionsFrom(deliveries, this.agents, app.username)
        this.users = app.user ? [{ id: app.user.id, name: app.user.name, email: app.user.email, role: app.user.role, active: true, lastLogin: 0 }] : []
        this.notifyChannels = []
        this.notifyLogs = []
        this.pendingChanges = []
        if (this.selectedProjectId !== 'all' && !this.projects.some((project) => project.id === this.selectedProjectId)) this.selectedProjectId = 'all'
        if (this.selectedNetworkId !== 'all' && !this.networks.some((network) => network.id === this.selectedNetworkId)) this.selectedNetworkId = 'all'
        this.lastUpdated = Date.now()
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
    async setCustomPairs(networkId: string, pairs: [string, string][], _user?: string) {
      try {
        for (const [sourceIface, targetIface] of pairs) {
          const source = this.ifaceWithAgent(sourceIface)?.agent.id
          const target = this.ifaceWithAgent(targetIface)?.agent.id
          if (source && target && source !== target) await api.addPeer(networkId, source, target)
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
    reloadGeoIP(_user?: string) { this.unsupported('GeoIP 重载') },
    addAgentFromScript(_payload: unknown) { this.unsupported('浏览器内直接创建 Agent') },
    pushEvent(_kind: FeedEvent['kind'], _message: string) {},
    pushAudit(_user: string, _action: string, _detail: string) {},
    addNotifyChannel(_payload: Omit<NotifyChannel, 'id' | 'createdAt'>, _user?: string) { this.unsupported('通知渠道') },
    updateNotifyChannel(_id: string, _patch: Partial<NotifyChannel>, _user?: string) { this.unsupported('通知渠道') },
    removeNotifyChannel(_id: string, _user?: string) { this.unsupported('通知渠道') },
    testNotifyChannel(_id: string, _user?: string) { this.unsupported('通知渠道测试') },
  },
})
