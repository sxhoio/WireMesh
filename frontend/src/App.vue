<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, session, type AuditEvent, type Delivery, type Network, type Node, type Project, type Topology, type User } from './api'

type View = 'overview' | 'networks' | 'nodes' | 'activity'
const user = ref<User | null>(null)
const view = ref<View>('overview')
const projects = ref<Project[]>([])
const networks = ref<Network[]>([])
const nodes = ref<Node[]>([])
const deliveries = ref<Delivery[]>([])
const audit = ref<AuditEvent[]>([])
const selectedProject = ref('')
const selectedNetwork = ref('')
const error = ref('')
const notice = ref('')
const loading = ref(false)
const login = reactive({ email: 'admin@wiremesh.local', password: 'wiremesh-dev' })
const projectForm = reactive({ name: '', description: '' })
const networkForm = reactive<{ name: string; cidr: string; dns: string; topology: Topology }>({ name: '', cidr: '10.42.0.0/24', dns: '1.1.1.1', topology: 'full_mesh' })
const nodeForm = reactive({ name: '', endpoint: '', region: '', os: 'linux', agent_version: '0.1.0', role: 'spoke' })

const currentNetwork = computed(() => networks.value.find((network) => network.id === selectedNetwork.value))
const networkNodes = computed(() => selectedNetwork.value ? nodes.value.filter((node) => node.network_id === selectedNetwork.value) : nodes.value)
const onlineNodes = computed(() => nodes.value.filter((node) => node.last_seen && new Date(node.last_seen).getTime() > Date.now() - 120000).length)
const canWrite = computed(() => user.value?.role === 'admin' || user.value?.role === 'operator')

async function refresh() {
  if (!session.token) return
  loading.value = true; error.value = ''
  try {
    const [me, nextProjects] = await Promise.all([api.me(), api.projects()])
    user.value = me; projects.value = nextProjects
    if (!selectedProject.value && nextProjects[0]) selectedProject.value = nextProjects[0].id
    networks.value = selectedProject.value ? await api.networks(selectedProject.value) : []
    if (selectedNetwork.value && !networks.value.some((network) => network.id === selectedNetwork.value)) selectedNetwork.value = ''
    nodes.value = await api.nodes()
    deliveries.value = await api.deliveries()
    if (me.role === 'admin') audit.value = await api.audit()
  } catch (reason) { error.value = reason instanceof Error ? reason.message : 'Unable to load the control plane' }
  finally { loading.value = false }
}
async function signIn() { error.value = ''; try { const result = await api.login(login.email, login.password); session.token = result.token; user.value = result.user; await refresh() } catch (reason) { error.value = reason instanceof Error ? reason.message : 'Sign-in failed' } }
function signOut() { session.clear(); user.value = null; projects.value = []; networks.value = []; nodes.value = []; selectedNetwork.value = '' }
async function createProject() { await action(async () => { await api.createProject(projectForm); projectForm.name = ''; projectForm.description = ''; await refresh(); notice.value = 'Project created' }) }
async function createNetwork() { await action(async () => { if (!selectedProject.value) throw new Error('Create a project first'); await api.createNetwork({ project_id: selectedProject.value, ...networkForm }); networkForm.name = ''; await refresh(); notice.value = 'Network created' }) }
async function createNode() { await action(async () => { if (!selectedNetwork.value) throw new Error('Select a network first'); await api.createNode({ network_id: selectedNetwork.value, ...nodeForm, labels: { 'wiremesh.role': nodeForm.role } }); nodeForm.name = ''; nodeForm.endpoint = ''; await refresh(); notice.value = 'Node created with a managed WireGuard key' }) }
async function publish() { await action(async () => { if (!selectedNetwork.value) throw new Error('Select a network first'); const revision = await api.publish(selectedNetwork.value); await refresh(); notice.value = `Configuration version ${revision.version} is pending delivery` }) }
async function enrollment() { await action(async () => { if (!selectedNetwork.value || !selectedProject.value) throw new Error('Select a network first'); const created = await api.enrollment(selectedProject.value, selectedNetwork.value); notice.value = `One-time agent token: ${created.token}` }) }
async function action(work: () => Promise<void>) { error.value = ''; notice.value = ''; try { await work() } catch (reason) { error.value = reason instanceof Error ? reason.message : 'Operation failed' } }
onMounted(() => refresh())
</script>

<template>
  <main v-if="!user" class="login-shell">
    <section class="login-panel">
      <p class="brand">WireMesh</p>
      <h1>Mesh control plane</h1>
      <p class="muted">Manage WireGuard nodes, topology, and versioned configuration.</p>
      <form @submit.prevent="signIn">
        <label>Email<input v-model="login.email" type="email" autocomplete="email" /></label>
        <label>Password<input v-model="login.password" type="password" autocomplete="current-password" /></label>
        <button type="submit">Sign in</button>
      </form>
      <p v-if="error" class="message error">{{ error }}</p>
    </section>
  </main>

  <main v-else class="app-shell">
    <aside class="sidebar">
      <div class="brand-block"><span class="brand-mark">W</span><span>WireMesh</span></div>
      <nav aria-label="Primary navigation">
        <button :class="{ active: view === 'overview' }" @click="view = 'overview'">Overview</button>
        <button :class="{ active: view === 'networks' }" @click="view = 'networks'">Networks</button>
        <button :class="{ active: view === 'nodes' }" @click="view = 'nodes'">Nodes</button>
        <button :class="{ active: view === 'activity' }" @click="view = 'activity'">Audit activity</button>
      </nav>
      <div class="account"><strong>{{ user.name }}</strong><span>{{ user.role }}</span><button @click="signOut">Sign out</button></div>
    </aside>

    <section class="workspace">
      <header class="topbar">
        <div><span class="eyebrow">Tenant: {{ user.tenant_id }}</span><h1>{{ view === 'overview' ? 'Operations overview' : view }}</h1></div>
        <div class="selectors"><label>Project<select v-model="selectedProject" @change="refresh"><option value="">Select project</option><option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</option></select></label><label>Network<select v-model="selectedNetwork"><option value="">All networks</option><option v-for="network in networks" :key="network.id" :value="network.id">{{ network.name }}</option></select></label></div>
      </header>
      <p v-if="error" class="message error">{{ error }}</p><p v-if="notice" class="message notice">{{ notice }}</p>

      <section v-if="view === 'overview'" class="content">
        <div class="metrics"><article><span>Networks</span><strong>{{ networks.length }}</strong></article><article><span>Managed nodes</span><strong>{{ nodes.length }}</strong></article><article><span>Online in last 2m</span><strong>{{ onlineNodes }}</strong></article><article><span>Pending delivery</span><strong>{{ deliveries.filter(d => d.state === 'pending').length }}</strong></article></div>
        <div class="split"><section class="surface"><div class="section-head"><h2>Selected topology</h2><button v-if="canWrite && selectedNetwork" @click="publish">Publish configuration</button></div><div v-if="currentNetwork" class="topology"><div v-for="node in networkNodes" :key="node.id" class="topology-node"><strong>{{ node.name }}</strong><span>{{ node.address }}</span><small>{{ node.labels?.['wiremesh.role'] || 'peer' }}</small></div></div><p v-else class="empty">Select a project and network to inspect its topology.</p></section><section class="surface"><h2>Recent delivery</h2><div v-for="delivery in deliveries.slice(0, 6)" :key="delivery.id" class="row"><span>{{ delivery.node_id }}</span><span>v{{ delivery.version }}</span><b :class="delivery.state">{{ delivery.state }}</b></div><p v-if="!deliveries.length" class="empty">No configuration releases yet.</p></section></div>
      </section>

      <section v-else-if="view === 'networks'" class="content">
        <div class="split"><section class="surface"><h2>Networks</h2><div v-for="network in networks" :key="network.id" class="network-row" :class="{ selected: selectedNetwork === network.id }" @click="selectedNetwork = network.id"><div><strong>{{ network.name }}</strong><span>{{ network.cidr }} · {{ network.topology }}</span></div><span>{{ nodes.filter(n => n.network_id === network.id).length }} nodes</span></div><p v-if="!networks.length" class="empty">No networks in this project.</p></section><section v-if="canWrite" class="surface"><h2>Create network</h2><form class="compact-form" @submit.prevent="createNetwork"><label>Name<input v-model="networkForm.name" required /></label><label>CIDR<input v-model="networkForm.cidr" required /></label><label>DNS<input v-model="networkForm.dns" /></label><label>Topology<select v-model="networkForm.topology"><option value="full_mesh">Full mesh</option><option value="hub_spoke">Hub spoke</option><option value="custom">Custom peers</option></select></label><button type="submit">Create network</button></form></section></div>
      </section>

      <section v-else-if="view === 'nodes'" class="content">
        <div class="split"><section class="surface"><div class="section-head"><h2>Nodes</h2><button v-if="canWrite && selectedNetwork" @click="enrollment">Create agent token</button></div><table><thead><tr><th>Name</th><th>Address</th><th>Endpoint</th><th>Role</th><th>Last seen</th></tr></thead><tbody><tr v-for="node in networkNodes" :key="node.id"><td>{{ node.name }}</td><td>{{ node.address }}</td><td>{{ node.endpoint || 'pending' }}</td><td>{{ node.labels?.['wiremesh.role'] || 'peer' }}</td><td>{{ node.last_seen ? new Date(node.last_seen).toLocaleString() : 'never' }}</td></tr></tbody></table><p v-if="!networkNodes.length" class="empty">No nodes match this selection.</p></section><section v-if="canWrite" class="surface"><h2>Create managed node</h2><form class="compact-form" @submit.prevent="createNode"><label>Name<input v-model="nodeForm.name" required /></label><label>Endpoint<input v-model="nodeForm.endpoint" placeholder="vpn.example.com:51820" /></label><label>Region<input v-model="nodeForm.region" placeholder="ap-east" /></label><label>Operating system<input v-model="nodeForm.os" /></label><label>Topology role<select v-model="nodeForm.role"><option value="spoke">Spoke</option><option value="hub">Hub</option></select></label><button type="submit">Create node</button></form></section></div>
      </section>

      <section v-else class="content"><div class="split"><section class="surface"><h2>Audit activity</h2><div v-for="event in audit" :key="event.id" class="row"><span>{{ event.action }}</span><span>{{ event.resource_type }}</span><time>{{ new Date(event.created_at).toLocaleString() }}</time></div><p v-if="!audit.length" class="empty">Audit events require an administrator role.</p></section><section v-if="canWrite" class="surface"><h2>Create project</h2><form class="compact-form" @submit.prevent="createProject"><label>Name<input v-model="projectForm.name" required /></label><label>Description<textarea v-model="projectForm.description" rows="3" /></label><button type="submit">Create project</button></form></section></div></section>
      <p v-if="loading" class="loading">Refreshing control-plane state...</p>
    </section>
  </main>
</template>
