export const apiBase = import.meta.env.VITE_API_URL || (import.meta.env.DEV ? 'http://localhost:8080' : '')

export interface ApiUser { id: string; tenant_id: string; email: string; name: string; role: 'admin' | 'operator' | 'viewer'; last_login_at?: string | null; created_at?: string }
export interface ApiProject { id: string; tenant_id: string; name: string; description: string; created_at: string }
export interface ApiNetwork { id: string; tenant_id: string; project_id: string; name: string; cidr: string; dns: string; topology: 'full_mesh' | 'hub_spoke' | 'custom'; created_at: string }
export interface ApiPeerRelation { id: string; network_id: string; source_node_id: string; target_node_id: string }
export interface ApiWireGuardPeer { public_key: string; endpoint: string; allowed_ips: string[]; latest_handshake_at?: string; receive_bytes: number; transmit_bytes: number; persistent_keepalive?: number; location_name?: string; latitude?: number; longitude?: number }
export interface ApiWireGuardInterface { name: string; public_key: string; listen_port: number; addresses: string[]; mtu: number; up: boolean; peers: ApiWireGuardPeer[] }
export interface ApiNode { id: string; tenant_id: string; project_id: string; network_id: string; name: string; hostname?: string; interface_selector?: string; collection_error?: string; enabled: boolean; listen_port: number; mtu: number; address: string; endpoint: string; region: string; location_name: string; location_source: string; latitude: number; longitude: number; os: string; agent_version: string; labels: Record<string, string>; public_key: string; wireguard?: ApiWireGuardInterface[]; last_seen: string; created_at: string }
export interface ApiAgentCommand { id: string; tenant_id: string; node_id: string; type: string; state: string; result?: string; created_at: string; started_at?: string; completed_at?: string }
export interface ApiClientConfig { node_id: string; name: string; address: string; content: string }
export interface ApiPeerConfigFile { interface: string; path?: string; content: string; updated_at?: string }
export interface ApiPeerConfigResponse { node_id: string; files: ApiPeerConfigFile[]; pending_files?: ApiPeerConfigFile[]; has_pending: boolean }
export interface ApiPeerConfigUpdateResult { node_id: string; files: ApiPeerConfigFile[]; command: ApiAgentCommand; offline: boolean; message: string }
export interface ApiNodeLog { id: string; level: string; source: string; message: string; created_at: string }
export interface ApiNodeLogPage { items: ApiNodeLog[]; current_error?: string; limit: number; offset: number; has_more: boolean }
export interface ApiAgentUpdateNodeStatus { node_id: string; updatable: boolean; needs_update: boolean; reason?: string }
export interface ApiAgentUpdateInfo {
  manifest: { available: boolean; version?: string; os?: string; arch?: string; size?: number; sha256?: string; download_url?: string; min_agent_version?: string; current_compatible?: boolean; error?: string }
  node_status: ApiAgentUpdateNodeStatus[]
}
export interface ApiAgentUpdateSkippedNode { node_id: string; name: string; agent_version?: string; reason: string }
export interface ApiAgentUpdateDispatchResult { created: number; node_ids?: string[]; skipped_node_ids?: string[]; skipped?: ApiAgentUpdateSkippedNode[] }
export type ApiTrafficRange = '5m' | '10m' | '30m' | '1h' | '2h' | '6h' | '12h' | '24h' | '7d' | '30d'
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
export interface ApiAlertRule { id: string; name: string; type: 'node_offline' | 'link_down' | 'config_failed'; threshold_sec: number; channel_ids: string[]; enabled: boolean; quiet_sec: number; scope_type: 'all' | 'network' | 'node'; scope_ids: string[]; created_at: string; updated_at: string }
export interface ApiAlertEvent { id: string; rule_id: string; rule_name: string; node_id: string; node_name: string; message: string; status: 'sent' | 'failed' | 'recorded' | 'recovered'; created_at: string }
export interface ApiAccessResource { id: string; network_id: string; name: string; gateway_node_id: string; target: string; port?: number; protocol?: string; description?: string; created_at: string }
export interface ApiAccessPolicy { id: string; network_id: string; name: string; source_label?: string; source_node_ids: string[]; resource_ids: string[]; enabled: boolean; created_at: string; updated_at: string }
export interface ApiDNSRecord { id: string; network_id: string; name: string; address: string; description?: string; created_at: string }
export interface ApiAPIToken { id: string; name: string; expires_at?: string | null; last_used_at?: string; created_at: string }
export interface ApiEgressConfig { network_id: string; egress_node_id: string; cidrs: string[]; updated_at: string }
export interface ApiUserSession { id: string; user_id: string; user_name: string; user_agent: string; created_at: string; last_seen_at: string; current: boolean }

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

// 认证 token 仅保留在内存中（不写入 localStorage），浏览器认证依赖后端下发的
// HttpOnly cookie，避免 XSS 窃取持久化 token。内存值仅作为非浏览器场景的
// Authorization 头回退。
let sessionToken = ''

export const session = {
  get token() { return sessionToken },
  set token(value: string) { sessionToken = value },
  clear() { sessionToken = '' },
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
  const response = await fetch(apiBase + url, { ...init, headers, credentials: 'include' })
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
  login: (email: string, password: string, otp?: string) => request<{ token: string; user: ApiUser }>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ email, password, otp }) }),
  logout: () => request<void>('/api/v1/auth/logout', { method: 'POST' }),
  me: () => request<ApiUser>('/api/v1/auth/me'),
  mfaStatus: () => request<{ enabled: boolean }>('/api/v1/auth/mfa/status'),
  mfaSetup: () => request<{ secret: string; uri: string }>('/api/v1/auth/mfa/setup', { method: 'POST' }),
  mfaEnable: (otp: string) => request<{ enabled: boolean }>('/api/v1/auth/mfa/enable', { method: 'POST', body: JSON.stringify({ otp }) }),
  mfaDisable: () => request<{ enabled: boolean }>('/api/v1/auth/mfa/disable', { method: 'POST' }),
  ssoConfig: () => request<{ issuer: string; client_id: string; client_secret_configured: boolean; enabled: boolean }>('/api/v1/settings/sso'),
  updateSSOConfig: (payload: { issuer: string; client_id: string; client_secret?: string; enabled: boolean }) => request<{ issuer: string; client_id: string; client_secret_configured: boolean; enabled: boolean }>('/api/v1/settings/sso', { method: 'PUT', body: JSON.stringify(payload) }),
  ssoLogin: (tenant?: string) => request<{ url?: string; tenants?: string[] }>('/api/v1/auth/sso/login' + (tenant ? '?tenant=' + encodeURIComponent(tenant) : '')),
  projects: () => requestArray<ApiProject>('/api/v1/projects'),
  createProject: (payload: { name: string; description: string }) => request<ApiProject>('/api/v1/projects', { method: 'POST', body: JSON.stringify(payload) }),
  networks: (projectId?: string) => requestArray<ApiNetwork>('/api/v1/networks' + (projectId ? '?project_id=' + encodeURIComponent(projectId) : '')),
  networkPeers: (networkId: string) => requestArray<ApiPeerRelation>('/api/v1/networks/' + encodeURIComponent(networkId) + '/peers'),
  createNetwork: (payload: { project_id: string; name: string; cidr: string; dns: string; topology: ApiNetwork['topology'] }) => request<ApiNetwork>('/api/v1/networks', { method: 'POST', body: JSON.stringify(payload) }),
  nodes: () => requestArray<ApiNode>('/api/v1/nodes'),
  node: (id: string) => request<ApiNode>('/api/v1/nodes/' + encodeURIComponent(id)),
  createNode: (payload: { network_id: string; name: string; endpoint?: string; labels?: Record<string, string> }) => request<ApiNode>('/api/v1/nodes', { method: 'POST', body: JSON.stringify(payload) }),
  nodeClientConfig: (id: string) => request<ApiClientConfig>('/api/v1/nodes/' + encodeURIComponent(id) + '/client-config'),
  updateNode: (id: string, payload: Partial<Pick<ApiNode, 'name' | 'address' | 'endpoint' | 'listen_port' | 'mtu' | 'enabled' | 'interface_selector' | 'labels' | 'location_name' | 'location_source' | 'latitude' | 'longitude'>>) => request<ApiNodeUpdateResult>('/api/v1/nodes/' + encodeURIComponent(id), { method: 'PATCH', body: JSON.stringify(payload) }),
  deleteNode: (id: string) => request<void>('/api/v1/nodes/' + encodeURIComponent(id), { method: 'DELETE' }),
  nodePeerConfig: (id: string) => request<ApiPeerConfigResponse>('/api/v1/nodes/' + encodeURIComponent(id) + '/peer-config'),
  updateNodePeerConfig: (id: string, payload: { interface: string; content: string }) => request<ApiPeerConfigUpdateResult>('/api/v1/nodes/' + encodeURIComponent(id) + '/peer-config', { method: 'PUT', body: JSON.stringify(payload) }),
  collectNode: (id: string) => request<ApiAgentCommand>('/api/v1/nodes/' + encodeURIComponent(id) + '/collect', { method: 'POST' }),
  collectAllNodes: (nodeIds?: string[]) => request<{ created: number; node_ids?: string[] }>('/api/v1/nodes/collect', { method: 'POST', body: JSON.stringify({ node_ids: nodeIds || [] }) }),
  updateAgent: (id: string) => request<ApiAgentCommand>('/api/v1/nodes/' + encodeURIComponent(id) + '/update-agent', { method: 'POST' }),
  updateAgents: (nodeIds?: string[]) => request<ApiAgentUpdateDispatchResult>('/api/v1/nodes/update-agent', { method: 'POST', body: JSON.stringify({ node_ids: nodeIds || [] }) }),
  checkNodeConnectivity: (id: string) => request<ApiAgentCommand>('/api/v1/nodes/' + encodeURIComponent(id) + '/connectivity-check', { method: 'POST' }),
  rotateNodeKey: (id: string) => request<{ status: string }>('/api/v1/nodes/' + encodeURIComponent(id) + '/rotate-key', { method: 'POST' }),
  nodeLogs: (id: string, limit = 50, offset = 0, errorsOnly = false) => request<ApiNodeLogPage>('/api/v1/nodes/' + encodeURIComponent(id) + '/logs?limit=' + limit + '&offset=' + offset + (errorsOnly ? '&level=error' : '')),
  clearNodeLogs: (id: string) => request<void>('/api/v1/nodes/' + encodeURIComponent(id) + '/logs', { method: 'DELETE' }),
  traffic: (id: string, interfaceName: string, range: ApiTrafficRange) => requestArray<ApiTrafficPoint>('/api/v1/nodes/' + encodeURIComponent(id) + '/traffic?interface=' + encodeURIComponent(interfaceName) + '&range=' + range),
  deliveries: () => requestArray<ApiDelivery>('/api/v1/deliveries'),
  audit: (limit = 50, offset = 0) => request<ApiAuditPage>('/api/v1/audit?limit=' + limit + '&offset=' + offset),
  clearAudit: () => request<void>('/api/v1/audit', { method: 'DELETE' }),
  publish: (networkId: string) => request<ApiConfigPublishResult>('/api/v1/networks/' + encodeURIComponent(networkId) + '/publish', { method: 'POST' }),
  addPeer: (networkId: string, sourceNodeId: string, targetNodeId: string) => request('/api/v1/networks/' + encodeURIComponent(networkId) + '/peers', { method: 'POST', body: JSON.stringify({ source_node_id: sourceNodeId, target_node_id: targetNodeId }) }),
  createEnrollment: (projectId: string, networkId: string, ttlMinutes = 30) => request<EnrollmentResult>('/api/v1/agent/enrollment-tokens', { method: 'POST', body: JSON.stringify({ project_id: projectId, network_id: networkId, ttl_minutes: ttlMinutes }) }),
  agentUpdateInfo: () => request<ApiAgentUpdateInfo>('/api/v1/agent/update'),
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
  alertRules: () => requestArray<ApiAlertRule>('/api/v1/settings/alert-rules'),
  createAlertRule: (payload: Omit<ApiAlertRule, 'id' | 'created_at' | 'updated_at'>) => request<ApiAlertRule>('/api/v1/settings/alert-rules', { method: 'POST', body: JSON.stringify(payload) }),
  updateAlertRule: (id: string, payload: Omit<ApiAlertRule, 'id' | 'created_at' | 'updated_at'>) => request<ApiAlertRule>('/api/v1/settings/alert-rules/' + encodeURIComponent(id), { method: 'PUT', body: JSON.stringify(payload) }),
  deleteAlertRule: (id: string) => request<void>('/api/v1/settings/alert-rules/' + encodeURIComponent(id), { method: 'DELETE' }),
  alertEvents: () => requestArray<ApiAlertEvent>('/api/v1/settings/alert-events'),
  clearAlertEvents: () => request<void>('/api/v1/settings/alert-events', { method: 'DELETE' }),
  evaluateAlertRule: (id: string) => request<{ evaluated: number; triggered: number }>('/api/v1/settings/alert-rules/' + encodeURIComponent(id) + '/evaluate', { method: 'POST' }),
  accessResources: (networkId: string) => requestArray<ApiAccessResource>('/api/v1/networks/' + encodeURIComponent(networkId) + '/access-resources'),
  createAccessResource: (networkId: string, payload: Omit<ApiAccessResource, 'id' | 'network_id' | 'created_at'>) => request<ApiAccessResource>('/api/v1/networks/' + encodeURIComponent(networkId) + '/access-resources', { method: 'POST', body: JSON.stringify(payload) }),
  updateAccessResource: (networkId: string, resourceId: string, payload: Omit<ApiAccessResource, 'id' | 'network_id' | 'created_at'>) => request<ApiAccessResource>('/api/v1/networks/' + encodeURIComponent(networkId) + '/access-resources/' + encodeURIComponent(resourceId), { method: 'PUT', body: JSON.stringify(payload) }),
  deleteAccessResource: (networkId: string, resourceId: string) => request<void>('/api/v1/networks/' + encodeURIComponent(networkId) + '/access-resources/' + encodeURIComponent(resourceId), { method: 'DELETE' }),
  accessPolicies: (networkId: string) => requestArray<ApiAccessPolicy>('/api/v1/networks/' + encodeURIComponent(networkId) + '/access-policies'),
  createAccessPolicy: (networkId: string, payload: Omit<ApiAccessPolicy, 'id' | 'network_id' | 'created_at' | 'updated_at'>) => request<ApiAccessPolicy>('/api/v1/networks/' + encodeURIComponent(networkId) + '/access-policies', { method: 'POST', body: JSON.stringify(payload) }),
  updateAccessPolicy: (networkId: string, policyId: string, payload: Omit<ApiAccessPolicy, 'id' | 'network_id' | 'created_at' | 'updated_at'>) => request<ApiAccessPolicy>('/api/v1/networks/' + encodeURIComponent(networkId) + '/access-policies/' + encodeURIComponent(policyId), { method: 'PUT', body: JSON.stringify(payload) }),
  deleteAccessPolicy: (networkId: string, policyId: string) => request<void>('/api/v1/networks/' + encodeURIComponent(networkId) + '/access-policies/' + encodeURIComponent(policyId), { method: 'DELETE' }),
  dnsRecords: (networkId: string) => requestArray<ApiDNSRecord>('/api/v1/networks/' + encodeURIComponent(networkId) + '/dns-records'),
  createDNSRecord: (networkId: string, payload: { name: string; address: string; description?: string }) => request<ApiDNSRecord>('/api/v1/networks/' + encodeURIComponent(networkId) + '/dns-records', { method: 'POST', body: JSON.stringify(payload) }),
  deleteDNSRecord: (networkId: string, recordId: string) => request<void>('/api/v1/networks/' + encodeURIComponent(networkId) + '/dns-records/' + encodeURIComponent(recordId), { method: 'DELETE' }),
  apiTokens: () => requestArray<ApiAPIToken>('/api/v1/settings/api-tokens'),
  createAPIToken: (payload: { name: string; ttl_days: number }) => request<{ token: string; api_token: ApiAPIToken }>('/api/v1/settings/api-tokens', { method: 'POST', body: JSON.stringify(payload) }),
  deleteAPIToken: (id: string) => request<void>('/api/v1/settings/api-tokens/' + encodeURIComponent(id), { method: 'DELETE' }),
  egress: (networkId: string) => request<ApiEgressConfig>('/api/v1/networks/' + encodeURIComponent(networkId) + '/egress'),
  updateEgress: (networkId: string, payload: { egress_node_id: string; cidrs: string[] }) => request<ApiEgressConfig>('/api/v1/networks/' + encodeURIComponent(networkId) + '/egress', { method: 'PUT', body: JSON.stringify(payload) }),
  sessions: () => requestArray<ApiUserSession>('/api/v1/auth/sessions'),
  revokeSession: (id: string) => request<void>('/api/v1/auth/sessions/' + encodeURIComponent(id), { method: 'DELETE' }),
  users: () => requestArray<ApiUser>('/api/v1/users'),
  createUser: (payload: { name: string; email: string; password: string; role: ApiUser['role'] }) => request<ApiUser>('/api/v1/users', { method: 'POST', body: JSON.stringify(payload) }),
}
