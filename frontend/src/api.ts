export const apiBase = import.meta.env.VITE_API_URL || (import.meta.env.DEV ? 'http://localhost:8080' : '')
const TOKEN_KEY = 'wiremesh.token'

export interface ApiUser { id: string; tenant_id: string; email: string; name: string; role: 'admin' | 'operator' | 'viewer' }
export interface ApiProject { id: string; tenant_id: string; name: string; description: string; created_at: string }
export interface ApiNetwork { id: string; tenant_id: string; project_id: string; name: string; cidr: string; dns: string; topology: 'full_mesh' | 'hub_spoke' | 'custom'; created_at: string }
export interface ApiNode { id: string; tenant_id: string; project_id: string; network_id: string; name: string; address: string; endpoint: string; region: string; os: string; agent_version: string; labels: Record<string, string>; public_key: string; last_seen: string; created_at: string }
export interface ApiDelivery { id: string; tenant_id: string; node_id: string; version: number; state: string; message: string; updated_at: string }
export interface ApiAudit { id: string; tenant_id: string; actor_id: string; action: string; resource_type: string; resource_id: string; metadata?: Record<string, string>; created_at: string }
export interface EnrollmentResult { token: string; expires_at: string; network_id: string }

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

export const api = {
  setupStatus: () => request<{ initialized: boolean }>('/api/v1/setup/status'),
  setup: (payload: { email: string; name: string; password: string }) => request<{ user: ApiUser }>('/api/v1/setup', { method: 'POST', body: JSON.stringify(payload) }),
  login: (email: string, password: string) => request<{ token: string; user: ApiUser }>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ email, password }) }),
  me: () => request<ApiUser>('/api/v1/auth/me'),
  projects: () => request<ApiProject[]>('/api/v1/projects'),
  networks: (projectId: string) => request<ApiNetwork[]>('/api/v1/networks?project_id=' + encodeURIComponent(projectId)),
  nodes: () => request<ApiNode[]>('/api/v1/nodes'),
  deliveries: () => request<ApiDelivery[]>('/api/v1/deliveries'),
  audit: () => request<ApiAudit[]>('/api/v1/audit'),
  publish: (networkId: string) => request<{ version: number }>('/api/v1/networks/' + encodeURIComponent(networkId) + '/publish', { method: 'POST' }),
  addPeer: (networkId: string, sourceNodeId: string, targetNodeId: string) => request('/api/v1/networks/' + encodeURIComponent(networkId) + '/peers', { method: 'POST', body: JSON.stringify({ source_node_id: sourceNodeId, target_node_id: targetNodeId }) }),
  createEnrollment: (projectId: string, networkId: string, ttlMinutes = 30) => request<EnrollmentResult>('/api/v1/agent/enrollment-tokens', { method: 'POST', body: JSON.stringify({ project_id: projectId, network_id: networkId, ttl_minutes: ttlMinutes }) }),
}
