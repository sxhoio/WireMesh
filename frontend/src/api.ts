export type Role = 'viewer' | 'operator' | 'admin'
export type Topology = 'full_mesh' | 'hub_spoke' | 'custom'
export interface User { id: string; tenant_id: string; email: string; name: string; role: Role }
export interface Project { id: string; name: string; description: string }
export interface Network { id: string; project_id: string; name: string; cidr: string; dns: string; topology: Topology }
export interface Node { id: string; network_id: string; name: string; address: string; endpoint: string; region: string; os: string; agent_version: string; labels: Record<string, string>; last_seen: string }
export interface Delivery { id: string; node_id: string; version: number; state: string; message: string; updated_at: string }
export interface AuditEvent { id: string; action: string; resource_type: string; resource_id: string; created_at: string }

const base = import.meta.env.VITE_API_URL || (import.meta.env.DEV ? 'http://localhost:8080' : '')
let token = localStorage.getItem('wiremesh.token') || ''
export const session = { get token() { return token }, set token(value: string) { token = value; localStorage.setItem('wiremesh.token', value) }, clear() { token = ''; localStorage.removeItem('wiremesh.token') } }

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(base + path, { ...init, headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', ...init.headers } })
  if (!response.ok) { const body = await response.json().catch(() => ({})); throw new Error(body.error || `Request failed (${response.status})`) }
  return response.json() as Promise<T>
}
export const api = {
  login: (email: string, password: string) => request<{ token: string; user: User }>('/api/v1/auth/login', { method: 'POST', body: JSON.stringify({ email, password }) }),
  me: () => request<User>('/api/v1/auth/me'),
  projects: () => request<Project[]>('/api/v1/projects'),
  createProject: (body: { name: string; description: string }) => request<Project>('/api/v1/projects', { method: 'POST', body: JSON.stringify(body) }),
  networks: (projectID = '') => request<Network[]>(`/api/v1/networks?project_id=${encodeURIComponent(projectID)}`),
  createNetwork: (body: Omit<Network, 'id'>) => request<Network>('/api/v1/networks', { method: 'POST', body: JSON.stringify(body) }),
  nodes: (networkID = '') => request<Node[]>(`/api/v1/nodes?network_id=${encodeURIComponent(networkID)}`),
  createNode: (body: Omit<Node, 'id' | 'address' | 'last_seen'>) => request<Node>('/api/v1/nodes', { method: 'POST', body: JSON.stringify(body) }),
  publish: (networkID: string) => request<{ version: number }>(`/api/v1/networks/${networkID}/publish`, { method: 'POST' }),
  deliveries: () => request<Delivery[]>('/api/v1/deliveries'),
  audit: () => request<AuditEvent[]>('/api/v1/audit'),
  enrollment: (project_id: string, network_id: string) => request<{ token: string; expires_at: string }>('/api/v1/agent/enrollment-tokens', { method: 'POST', body: JSON.stringify({ project_id, network_id }) }),
}
