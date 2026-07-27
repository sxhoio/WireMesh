export const apiBase = import.meta.env.VITE_API_URL || (import.meta.env.DEV ? 'http://localhost:8080' : '')
const TOKEN_KEY = 'wiremesh.token'

export interface ApiUser { id: string; tenant_id: string; email: string; name: string; role: 'admin' | 'operator' | 'viewer'; last_login_at?: string | null; created_at?: string }
export interface ApiProject { id: string; tenant_id: string; name: string; description: string; created_at: string }
export interface ApiNetwork { id: string; tenant_id: string; project_id: string; name: string; cidr: string; dns: string; topology: 'full_mesh' | 'hub_spoke' | 'custom'; created_at: string }
export interface ApiWireGuardPeer { public_key: string; endpoint: string; allowed_ips: string[]; latest_handshake_at?: string; receive_bytes: number; transmit_bytes: number; persistent_keepalive?: number; location_name?: string; latitude?: number; longitude?: number }
export interface ApiWireGuardInterface { name: string; public_key: string; listen_port: number; addresses: string[]; mtu: number; up: boolean; peers: ApiWireGuardPeer[] }
export interface ApiNode { id: string; tenant_id: string; project_id: string; network_id: string; name: string; hostname?: string; interface_selector?: string; collection_error?: string; enabled: boolean; listen_port: number; mtu: number; address: string; endpoint: string; region: string; location_name: string; location_source: string; latitude: number; longitude: number; os: string; agent_version: string; labels: Record<string, string>; public_key: string; wireguard?: ApiWireGuardInterface[]; last_seen: string; created_at: string }
export interface ApiAgentCommand { id: string; tenant_id: string; node_id: string; type: string; state: string; result?: string; created_at: string; started_at?: string; completed_at?: string }
export interface ApiPeerConfigFile { interface: string; path?: string; content: string; updated_at?: string }
export interface ApiPeerConfigResponse { node_id: string; files: ApiPeerConfigFile[]; pending_files?: ApiPeerConfigFile[]; has_pending: boolean }
export interface ApiPeerConfigUpdateResult { node_id: string; files: ApiPeerConfigFile[]; command: ApiAgentCommand; offline: boolean; message: string }
export interface ApiNodeLog { id: string; level: string; source: string; message: string; created_at: string }
export interface ApiNodeLogPage { items: ApiNodeLog[]; current_error?: string; limit: number; offset: number; has_more: boolean }
export interface ApiTrafficPoint { recorded_at: string; receive_bytes: number; transmit_bytes: number; rx_mbps: number; tx_mbps: number }
export interface ApiDelivery { id: string; tenant_id: string; node_id: string; version: number; state: string; message: string; updated_at: string }
export interface ApiConfigPublishResult {
  revision_id?: string
  network_id: string
  version: number
  changed_node_ids: string[]
  queued_node_ids: string[]
  offline_node_ids: string[]
  unchanged: boolean
}
export type ApiNodeUpdateResult = ApiNode & { delivery?: ApiConfigPublishResult; delivery_error?: string }
export interface ApiAudit { id: string; tenant_id: string; actor_id: string; action: string; resource_type: string; resource_id: string; metadata?: Record<string, string>; created_at: string }
export interface ApiAuditPage { items: ApiAudit[]; limit: number; offset: number; has_more: boolean }
export interface EnrollmentResult { token: string; expires_at: string; network_id: string }
export interface ApiSystemSettings {
  dashboardName: string
  sessionTimeoutMin: number
  netDefaults: { dns: string; port: number; mtu: number; keepalive: number; defaultTopology: 'full-mesh' | 'hub-spoke' | 'custom' }
  statusRules: { agentOfflineSec: number; handshakeSec: number; redFailCount: number }
  collect: { reportSec: number; probeSec: number; mapRefreshSec: number }
  retention: { rawDays: number; hourlyDays: number; dailyDays: number }
  agent: { labels: string; upgradePolicy: 'manual' | 'auto-stable' }
  updatedAt?: string
}
export interface ApiGeoIPStatus { dbPath: string; version: string; updatedAt: string; entryCount: number }
export interface ApiGeoIPLookup { ip: string; city: string; country: string; countryCode: string; latitude: number; longitude: number; timezone: string }
export interface ApiNotificationHeader { name: string; value?: string; valueConfigured?: boolean }
export interface ApiNotificationConfig {
  url?: string; urlConfigured?: boolean; method?: 'POST' | 'PUT' | 'PATCH'; contentType?: string
  headers?: ApiNotificationHeader[]; signatureType?: 'none' | 'hmac-sha256' | 'bearer'; secret?: string; secretConfigured?: boolean
  timeoutSec?: number; allowPrivate?: boolean; messageType?: 'text' | 'markdown' | 'post'; atAll?: boolean
  atMobiles?: string[]; atMobilesConfigured?: boolean; atMobileCount?: number; atUserIds?: string[]; atUserIdsConfigured?: boolean; atUserIdCount?: number; botToken?: string; botTokenConfigured?: boolean; chatId?: string; chatIdConfigured?: boolean
  threadId?: string; parseMode?: '' | 'HTML' | 'MarkdownV2'; disableWebPagePreview?: boolean; disableNotification?: boolean
  smtpHost?: string; smtpPort?: number; username?: string; password?: string; passwordConfigured?: boolean
  fromAddress?: string; fromName?: string; to?: string[]; recipientsConfigured?: boolean; recipientCount?: number
  cc?: string[]; ccConfigured?: boolean; ccCount?: number; encryption?: 'none' | 'starttls' | 'tls'; skipTlsVerify?: boolean
}
export interface ApiNotificationChannel {
  id: string; name: string; type: 'webhook' | 'dingtalk' | 'wecom' | 'feishu' | 'telegram' | 'email'
  config: ApiNotificationConfig; template: string; subjectTemplate?: string; enabled: boolean; agents: 'all' | string[]; createdAt: string; updatedAt: string
}
export interface ApiNotificationLog { id: string; channelId: string; channelName: string; channelType: ApiNotificationChannel['type']; agentName: string; message: string; status: 'sent' | 'failed' | 'test'; createdAt: string }

export type DatabaseDriver = 'sqlite' | 'mysql' | 'postgres'
export interface DatabaseSetupConfig {
  driver: DatabaseDriver
  sqlite_path?: string
  host?: string
  port?: number
  database?: string
  username?: string
  password?: string
  ssl_mode?: string
}
export interface SetupStatus {
  initialized: boolean
  database_configured: boolean
  database_configurable?: boolean
  database_driver?: DatabaseDriver
}

export const session = {
  get token() { return localStorage.getItem(TOKEN_KEY) || '' },
  set token(value: string) { value ? localStorage.setItem(TOKEN_KEY, value) : localStorage.removeItem(TOKEN_KEY) },
  clear() { localStorage.removeItem(TOKEN_KEY) },
}

export class ApiError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(url: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  if (session.token) headers.set('Authorization', 'Bearer ' + session.token)
  if (init.body) headers.set('Content-Type', 'application/json')
  const response = await fetch(apiBase + url, { ...init, headers })
  if (!response.ok) {
    const payload = await response.json().catch(() => ({})) as { error?: string }
    if (response.status === 401) session.clear()
    throw new ApiError(response.status, payload.error || '请求失败（' + response.status + '）')
  }
  if (response.status === 204) return undefined as T
  return response.json() as Promise<T>
}

async function requestArray<T>(url: string): Promise<T[]> {
  const value = await request<T[] | null>(url)
  return Array.isArray(value) ? value : []
}

export const api = {
  setupStatus: () => request<SetupStatus>('/api/v1/setup/status'),
  databaseStatus: () => request<{ configured: boolean; driver?: DatabaseDriver }>('/api/v1/setup/database'),
  testDatabase: (payload: DatabaseSetupConfig) => request<{ connected: boolean }>('/api/v1/setup/database/test', { method: 'POST', body: JSON.stringify(payload) }),
  configureDatabase: (payload: DatabaseSetupConfig) => request<{ configured: boolean; driver: DatabaseDriver; initialized: boolean }>('/api/v1/setup/database', { method: 'POST', body: JSON.stringify(payload) }),
  setup: (payload: { email: string; name: string; password: string }) => request<{ user: ApiUser }>('/api/v1/setup', { method: 'POST', body: JSON.stringify(payload) }),
  login: (email: string, password: string) => request<{ token: string; user: ApiUser }>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ email, password }) }),
  me: () => request<ApiUser>('/api/v1/auth/me'),
  projects: () => requestArray<ApiProject>('/api/v1/projects'),
  createProject: (payload: { name: string; description: string }) => request<ApiProject>('/api/v1/projects', { method: 'POST', body: JSON.stringify(payload) }),
  networks: (projectId: string) => requestArray<ApiNetwork>('/api/v1/networks?project_id=' + encodeURIComponent(projectId)),
  createNetwork: (payload: { project_id: string; name: string; cidr: string; dns: string; topology: ApiNetwork['topology'] }) => request<ApiNetwork>('/api/v1/networks', { method: 'POST', body: JSON.stringify(payload) }),
  nodes: () => requestArray<ApiNode>('/api/v1/nodes'),
  node: (id: string) => request<ApiNode>('/api/v1/nodes/' + encodeURIComponent(id)),
  updateNode: (id: string, payload: Partial<Pick<ApiNode, 'name' | 'address' | 'endpoint' | 'listen_port' | 'mtu' | 'enabled' | 'interface_selector' | 'labels' | 'location_name' | 'location_source' | 'latitude' | 'longitude'>>) => request<ApiNodeUpdateResult>('/api/v1/nodes/' + encodeURIComponent(id), { method: 'PATCH', body: JSON.stringify(payload) }),
  deleteNode: (id: string) => request<void>('/api/v1/nodes/' + encodeURIComponent(id), { method: 'DELETE' }),
  nodePeerConfig: (id: string) => request<ApiPeerConfigResponse>('/api/v1/nodes/' + encodeURIComponent(id) + '/peer-config'),
  updateNodePeerConfig: (id: string, payload: { interface: string; content: string }) => request<ApiPeerConfigUpdateResult>('/api/v1/nodes/' + encodeURIComponent(id) + '/peer-config', { method: 'PUT', body: JSON.stringify(payload) }),
  collectNode: (id: string) => request<ApiAgentCommand>('/api/v1/nodes/' + encodeURIComponent(id) + '/collect', { method: 'POST' }),
  collectAllNodes: (nodeIds?: string[]) => request<{ created: number; node_ids?: string[] }>('/api/v1/nodes/collect', { method: 'POST', body: JSON.stringify({ node_ids: nodeIds || [] }) }),
  checkNodeConnectivity: (id: string) => request<ApiAgentCommand>('/api/v1/nodes/' + encodeURIComponent(id) + '/connectivity-check', { method: 'POST' }),
  nodeLogs: (id: string, limit = 50, offset = 0, errorsOnly = false) => request<ApiNodeLogPage>('/api/v1/nodes/' + encodeURIComponent(id) + '/logs?limit=' + limit + '&offset=' + offset + (errorsOnly ? '&level=error' : '')),
  clearNodeLogs: (id: string) => request<void>('/api/v1/nodes/' + encodeURIComponent(id) + '/logs', { method: 'DELETE' }),
  traffic: (id: string, interfaceName: string, range: '24h' | '7d' | '30d') => requestArray<ApiTrafficPoint>('/api/v1/nodes/' + encodeURIComponent(id) + '/traffic?interface=' + encodeURIComponent(interfaceName) + '&range=' + range),
  deliveries: () => requestArray<ApiDelivery>('/api/v1/deliveries'),
  audit: (limit = 50, offset = 0) => request<ApiAuditPage>('/api/v1/audit?limit=' + limit + '&offset=' + offset),
  clearAudit: () => request<void>('/api/v1/audit', { method: 'DELETE' }),
  publish: (networkId: string) => request<ApiConfigPublishResult>('/api/v1/networks/' + encodeURIComponent(networkId) + '/publish', { method: 'POST' }),
  addPeer: (networkId: string, sourceNodeId: string, targetNodeId: string) => request('/api/v1/networks/' + encodeURIComponent(networkId) + '/peers', { method: 'POST', body: JSON.stringify({ source_node_id: sourceNodeId, target_node_id: targetNodeId }) }),
  createEnrollment: (projectId: string, networkId: string, ttlMinutes = 30) => request<EnrollmentResult>('/api/v1/agent/enrollment-tokens', { method: 'POST', body: JSON.stringify({ project_id: projectId, network_id: networkId, ttl_minutes: ttlMinutes }) }),
  settings: () => request<ApiSystemSettings>('/api/v1/settings'),
  updateSettings: (payload: ApiSystemSettings) => request<ApiSystemSettings>('/api/v1/settings', { method: 'PUT', body: JSON.stringify(payload) }),
  geoIPStatus: () => request<ApiGeoIPStatus>('/api/v1/settings/geoip'),
  updateGeoIP: (dbPath: string) => request<ApiGeoIPStatus>('/api/v1/settings/geoip', { method: 'PUT', body: JSON.stringify({ dbPath }) }),
  reloadGeoIP: () => request<ApiGeoIPStatus>('/api/v1/settings/geoip/reload', { method: 'POST' }),
  lookupGeoIP: (ip: string) => request<ApiGeoIPLookup>('/api/v1/settings/geoip/lookup?ip=' + encodeURIComponent(ip)),
  notificationChannels: () => requestArray<ApiNotificationChannel>('/api/v1/settings/notifications'),
  createNotificationChannel: (payload: Omit<ApiNotificationChannel, 'id' | 'createdAt' | 'updatedAt'>) => request<ApiNotificationChannel>('/api/v1/settings/notifications', { method: 'POST', body: JSON.stringify(payload) }),
  updateNotificationChannel: (id: string, payload: Omit<ApiNotificationChannel, 'id' | 'createdAt' | 'updatedAt'>) => request<ApiNotificationChannel>('/api/v1/settings/notifications/' + encodeURIComponent(id), { method: 'PUT', body: JSON.stringify(payload) }),
  deleteNotificationChannel: (id: string) => request<void>('/api/v1/settings/notifications/' + encodeURIComponent(id), { method: 'DELETE' }),
  testNotificationChannel: (id: string) => request<ApiNotificationLog>('/api/v1/settings/notifications/' + encodeURIComponent(id) + '/test', { method: 'POST' }),
  notificationLogs: () => requestArray<ApiNotificationLog>('/api/v1/settings/notification-logs'),
  users: () => requestArray<ApiUser>('/api/v1/users'),
  createUser: (payload: { name: string; email: string; password: string; role: ApiUser['role'] }) => request<ApiUser>('/api/v1/users', { method: 'POST', body: JSON.stringify(payload) }),
}
