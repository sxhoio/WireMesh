<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, type ApiAccessPolicy, type ApiAccessResource, type ApiEgressConfig } from '../api'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import type { Agent } from '../types'
import { requestConfirm } from '../utils/confirm'

const app = useAppStore()
const mesh = useMeshStore()
const router = useRouter()

const selectedNetworkId = ref('')
const resources = ref<ApiAccessResource[]>([])
const policies = ref<ApiAccessPolicy[]>([])
const loading = ref(false)
const error = ref('')
const notice = ref('')
const publishing = ref(false)
const pendingChanges = ref(0)
const egressDirty = ref(false)
const togglingPolicyId = ref<string | null>(null)

const resourceForm = reactive({
  name: '',
  gateway_node_id: '',
  target: '',
  port: 0,
  protocol: 'any' as 'tcp' | 'udp' | 'any' | '',
  description: '',
})

const policyForm = reactive({
  name: '',
  source_label: '',
  source_node_ids: [] as string[],
  resource_ids: [] as string[],
  enabled: true,
})

const editingResourceId = ref<string | null>(null)
const editingPolicyId = ref<string | null>(null)
const savingResource = ref(false)
const savingPolicy = ref(false)

// 出口网关（Egress）
const egress = ref<ApiEgressConfig>({ network_id: '', egress_node_id: '', cidrs: [], updated_at: '' })
const egressNodeId = ref('')
const egressCIDRsText = ref('')
const savingEgress = ref(false)

const networkNodes = computed(() => mesh.agents.filter((agent) => agent.networkId === selectedNetworkId.value))
const selectedNetwork = computed(() => (selectedNetworkId.value ? mesh.networkById(selectedNetworkId.value) : undefined))
const agentNodes = computed(() => networkNodes.value.filter((agent) => !agent.labels.includes('wiremesh.client=true')))
const clientNodes = computed(() => networkNodes.value.filter((agent) => agent.labels.includes('wiremesh.client=true')))

/** 目标 CIDR 留空时预览将要使用的默认值（网关节点地址/32） */
const defaultTargetPreview = computed(() => {
  if (resourceForm.target.trim()) return ''
  const gateway = mesh.agentById(resourceForm.gateway_node_id)
  return gateway ? `${gateway.address}/32` : ''
})

/** 标签选择器在当前网络匹配到的节点数（发布时以实际节点标签为准） */
function labelMatchCount(label: string) {
  const trimmed = label.trim()
  if (!trimmed) return 0
  return networkNodes.value.filter((agent) => agent.labels.includes(trimmed)).length
}

function resourceName(id: string) {
  return resources.value.find((resource) => resource.id === id)?.name || '已删除的资源'
}

function resourceDetail(id: string) {
  return resources.value.find((resource) => resource.id === id)
}

function gatewayAgent(resource: ApiAccessResource) {
  return mesh.agentById(resource.gateway_node_id)
}

function gatewayLabel(resource: ApiAccessResource) {
  const node = gatewayAgent(resource)
  return node ? node.name : '已删除的节点'
}

function agentRoleLabel(agent: Agent | undefined) {
  const role = agent?.labels.find((label) => label.startsWith('wiremesh.role='))?.split('=')[1]
  if (role === 'hub') return 'Hub'
  if (role === 'spoke') return 'Spoke'
  return 'Mesh'
}

function agentStatusText(agent: Agent) {
  if (!agent.enabled) return '停用'
  return agent.status === 'online' ? '在线' : '离线'
}

function resourceChip(resource: ApiAccessResource) {
  const base = resource.target
  return resource.port ? `${base}:${resource.port}/${resource.protocol || 'any'}` : base
}

function topologyLabel() {
  const topology = selectedNetwork.value?.topology
  if (topology === 'full-mesh') return '全互联（Full Mesh）'
  if (topology === 'hub-spoke') return '中心辐射（Hub-Spoke）'
  if (topology === 'custom') return '自定义（Custom）'
  return ''
}

/** 两个节点在当前拓扑中是否存在 Peer 链路（决定策略路由能否下发） */
function pairLinked(aId: string, bId: string) {
  const topology = selectedNetwork.value?.topology
  if (topology === 'full-mesh') return true
  if (topology === 'custom') {
    const pairs = selectedNetwork.value?.customPairs || []
    return pairs.some(([x, y]) => (x === aId && y === bId) || (x === bId && y === aId))
  }
  if (topology === 'hub-spoke') {
    const isHub = (id: string) => mesh.agentById(id)?.labels.includes('wiremesh.role=hub') || false
    return isHub(aId) || isHub(bId)
  }
  return true
}

/** 策略中「源 ↔ 网关」未互联的组合数：这些路由在拓扑中落不到 Peer 配置，不会生效 */
function policyUnlinkedCount(policy: ApiAccessPolicy) {
  const topology = selectedNetwork.value?.topology
  if (!topology || topology === 'full-mesh') return 0
  const sources = policy.source_label
    ? networkNodes.value.filter((agent) => agent.labels.includes(policy.source_label || '')).map((agent) => agent.id)
    : policy.source_node_ids.length
      ? policy.source_node_ids
      : networkNodes.value.map((agent) => agent.id)
  let count = 0
  for (const sourceID of sources) {
    for (const resourceID of policy.resource_ids) {
      const resource = resourceDetail(resourceID)
      if (!resource || resource.gateway_node_id === sourceID) continue
      if (!pairLinked(sourceID, resource.gateway_node_id)) count++
    }
  }
  return count
}

/** 后端 409 引用保护错误映射为可操作的中文提示 */
function friendlyReferenceError(message: string) {
  const prefix = '该资源仍被以下策略引用: '
  if (message.startsWith(prefix)) {
    return `无法删除：${message}。请先调整或删除相关策略。`
  }
  return message
}

let loadRequestID = 0

async function load(silent = false) {
  if (!selectedNetworkId.value) return
  const current = ++loadRequestID
  const guard = () => current === loadRequestID
  if (!silent) {
    loading.value = true
    // 静默自动刷新不清空用户正在查看的错误提示，只有显式加载才重置
    error.value = ''
  }
  if (!silent || !egressDirty.value) {
    const [resourceResult, policyResult, egressResult] = await Promise.allSettled([
      api.accessResources(selectedNetworkId.value),
      api.accessPolicies(selectedNetworkId.value),
      api.egress(selectedNetworkId.value),
    ])
    if (!guard()) return
    if (resourceResult.status === 'fulfilled') resources.value = resourceResult.value
    else error.value = resourceResult.reason instanceof Error ? resourceResult.reason.message : '加载资源失败'
    if (policyResult.status === 'fulfilled') policies.value = policyResult.value
    if (egressResult.status === 'fulfilled') {
      egress.value = egressResult.value
      egressNodeId.value = egressResult.value.egress_node_id
      egressCIDRsText.value = (egressResult.value.cidrs || []).join(', ')
      egressDirty.value = false
    }
  } else {
    // 出口网关表单有未保存修改时，静默刷新只更新资源与策略列表
    const [resourceResult, policyResult] = await Promise.allSettled([
      api.accessResources(selectedNetworkId.value),
      api.accessPolicies(selectedNetworkId.value),
    ])
    if (!guard()) return
    if (resourceResult.status === 'fulfilled') resources.value = resourceResult.value
    if (policyResult.status === 'fulfilled') policies.value = policyResult.value
  }
  if (guard() && !silent) loading.value = false
}

function markDirty() {
  pendingChanges.value++
}

async function saveEgress() {
  if (!selectedNetworkId.value || savingEgress.value) return
  savingEgress.value = true
  error.value = ''
  const cidrs = egressCIDRsText.value.split(',').map((item) => item.trim()).filter(Boolean)
  try {
    egress.value = await api.updateEgress(selectedNetworkId.value, { egress_node_id: egressNodeId.value, cidrs })
    egressDirty.value = false
    markDirty()
    notice.value = '出口网关配置已保存，点「保存并发布」后生效'
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '保存出口网关失败'
  } finally {
    savingEgress.value = false
  }
}

watch(selectedNetworkId, () => {
  resetResourceForm()
  resetPolicyForm()
  pendingChanges.value = 0
  egressDirty.value = false
  notice.value = ''
  void load()
})

function restoreEgress() {
  egressNodeId.value = egress.value.egress_node_id
  egressCIDRsText.value = (egress.value.cidrs || []).join(', ')
  egressDirty.value = false
}

function resetResourceForm() {
  editingResourceId.value = null
  resourceForm.name = ''
  resourceForm.gateway_node_id = ''
  resourceForm.target = ''
  resourceForm.port = 0
  resourceForm.protocol = 'any'
  resourceForm.description = ''
}

function resetPolicyForm() {
  editingPolicyId.value = null
  policyForm.name = ''
  policyForm.source_label = ''
  policyForm.source_node_ids = []
  policyForm.resource_ids = []
  policyForm.enabled = true
}

function editResource(resource: ApiAccessResource) {
  editingResourceId.value = resource.id
  resourceForm.name = resource.name
  resourceForm.gateway_node_id = resource.gateway_node_id
  resourceForm.target = resource.target
  resourceForm.port = resource.port || 0
  resourceForm.protocol = (resource.protocol || 'any') as typeof resourceForm.protocol
  resourceForm.description = resource.description || ''
}

function editPolicy(policy: ApiAccessPolicy) {
  editingPolicyId.value = policy.id
  policyForm.name = policy.name
  policyForm.source_label = policy.source_label || ''
  policyForm.source_node_ids = [...policy.source_node_ids]
  policyForm.resource_ids = [...policy.resource_ids]
  policyForm.enabled = policy.enabled
}

function toggleSourceNode(id: string) {
  if (policyForm.source_label.trim()) return
  const index = policyForm.source_node_ids.indexOf(id)
  if (index >= 0) policyForm.source_node_ids.splice(index, 1)
  else policyForm.source_node_ids.push(id)
}

function onSourceLabelInput() {
  // 标签与节点二选一：填写标签后清空节点选择
  if (policyForm.source_label.trim()) policyForm.source_node_ids = []
}

function toggleResource(id: string) {
  const index = policyForm.resource_ids.indexOf(id)
  if (index >= 0) policyForm.resource_ids.splice(index, 1)
  else policyForm.resource_ids.push(id)
}

async function saveResource() {
  if (!selectedNetworkId.value || savingResource.value) return
  if (!resourceForm.name.trim()) {
    error.value = '请输入资源名称'
    return
  }
  if (!resourceForm.gateway_node_id) {
    error.value = '请选择网关节点'
    return
  }
  // 先捕获编辑状态：resetResourceForm 会清空 editingResourceId，避免提示文案误判
  const isEdit = Boolean(editingResourceId.value)
  const resourceId = editingResourceId.value
  savingResource.value = true
  error.value = ''
  const payload = {
    name: resourceForm.name.trim(),
    gateway_node_id: resourceForm.gateway_node_id,
    target: resourceForm.target.trim(),
    port: resourceForm.port,
    protocol: resourceForm.protocol,
    description: resourceForm.description.trim(),
  }
  try {
    if (isEdit && resourceId) await api.updateAccessResource(selectedNetworkId.value, resourceId, payload)
    else await api.createAccessResource(selectedNetworkId.value, payload)
    resetResourceForm()
    markDirty()
    notice.value = isEdit ? '资源已更新，点「保存并发布」后生效' : '资源已创建，点「保存并发布」后生效'
    await load(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '保存资源失败'
  } finally {
    savingResource.value = false
  }
}

async function removeResource(resource: ApiAccessResource) {
  const confirmed = await requestConfirm({
    title: '删除访问资源',
    message: `确定删除资源“${resource.name}”吗？`,
    confirmText: '删除资源',
    variant: 'danger',
  })
  if (!confirmed) return
  try {
    await api.deleteAccessResource(selectedNetworkId.value, resource.id)
    markDirty()
    notice.value = '资源已删除，点「保存并发布」后生效'
    await load(true)
  } catch (reason) {
    error.value = friendlyReferenceError(reason instanceof Error ? reason.message : '删除资源失败')
  }
}

async function savePolicy() {
  if (!selectedNetworkId.value || savingPolicy.value) return
  if (!policyForm.name.trim()) {
    error.value = '请输入策略名称'
    return
  }
  if (policyForm.source_label.trim() && !policyForm.source_label.trim().includes('=')) {
    error.value = '源标签选择器必须为 key=value 格式'
    return
  }
  if (!policyForm.resource_ids.length) {
    error.value = '请至少选择一个资源'
    return
  }
  savingPolicy.value = true
  error.value = ''
  const payload = {
    name: policyForm.name.trim(),
    source_label: policyForm.source_label.trim(),
    source_node_ids: policyForm.source_label.trim() ? [] : policyForm.source_node_ids,
    resource_ids: policyForm.resource_ids,
    enabled: policyForm.enabled,
  }
  try {
    if (editingPolicyId.value) await api.updateAccessPolicy(selectedNetworkId.value, editingPolicyId.value, payload)
    else await api.createAccessPolicy(selectedNetworkId.value, payload)
    resetPolicyForm()
    markDirty()
    notice.value = '策略已保存，点「保存并发布」后生效'
    await load(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '保存策略失败'
  } finally {
    savingPolicy.value = false
  }
}

async function removePolicy(policy: ApiAccessPolicy) {
  const confirmed = await requestConfirm({
    title: '删除访问策略',
    message: `确定删除策略“${policy.name}”吗？`,
    confirmText: '删除策略',
    variant: 'danger',
  })
  if (!confirmed) return
  try {
    await api.deleteAccessPolicy(selectedNetworkId.value, policy.id)
    if (editingPolicyId.value === policy.id) resetPolicyForm()
    markDirty()
    notice.value = '策略已删除，点「保存并发布」后生效'
    await load(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '删除策略失败'
  }
}

async function togglePolicy(policy: ApiAccessPolicy) {
  if (togglingPolicyId.value) return
  togglingPolicyId.value = policy.id
  try {
    await api.updateAccessPolicy(selectedNetworkId.value, policy.id, {
      name: policy.name,
      source_label: policy.source_label || '',
      source_node_ids: policy.source_node_ids,
      resource_ids: policy.resource_ids,
      enabled: !policy.enabled,
    })
    markDirty()
    notice.value = policy.enabled ? '策略已停用，点「保存并发布」后生效' : '策略已启用，点「保存并发布」后生效'
    await load(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '切换策略状态失败'
  } finally {
    togglingPolicyId.value = null
  }
}

async function publishChanges() {
  if (!selectedNetworkId.value || publishing.value) return
  if (pendingChanges.value > 0) {
    const confirmed = await requestConfirm({
      title: '发布网络配置',
      message: `将把当前网络的 ${pendingChanges.value} 项资源/策略变更发布到全部节点，继续吗？`,
      confirmText: '发布',
      variant: 'warning',
    })
    if (!confirmed) return
  }
  publishing.value = true
  error.value = ''
  try {
    const result = await api.publish(selectedNetworkId.value)
    const status = await mesh.waitForDeliveryResult(result)
    const offline = result.offline_node_ids?.length || 0
    let message = result.unchanged && status.expected === 0
      ? '配置未变化，无需下发'
      : status.failed > 0
        ? `已发布，但 ${status.failed} 个节点应用失败，请在下发记录中查看原因`
        : `已发布并下发：${status.applied}/${status.expected} 个节点已确认` + (offline ? `，${offline} 个离线` : '')
    notice.value = message
    pendingChanges.value = 0
    await load(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '发布失败'
  } finally {
    publishing.value = false
  }
}

let refreshTimer: number | undefined
onMounted(() => {
  refreshTimer = window.setInterval(() => {
    // 有未发布变更或正在编辑时跳过自动刷新，避免打断操作
    if (document.hidden || pendingChanges.value > 0 || editingPolicyId.value || editingResourceId.value) return
    void load(true)
  }, 30000)
})
onUnmounted(() => window.clearInterval(refreshTimer))
</script>

<template>
  <div class="flex h-full flex-col gap-5">
    <div class="sticky top-0 z-20 flex flex-wrap items-center gap-3 rounded-xl bg-ink-950/90 px-1 py-2 backdrop-blur">
      <div>
        <label class="label">网络</label>
        <select v-model="selectedNetworkId" class="input !w-64">
          <option value="">选择网络</option>
          <option v-for="n in mesh.networks" :key="n.id" :value="n.id">{{ n.name }}（{{ n.cidr }}）</option>
        </select>
      </div>
      <span v-if="selectedNetwork" class="chip mb-2 bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">{{ topologyLabel() }}</span>
      <p class="mb-2 text-xs text-slate-500">访问策略在发布网络配置时生效：策略允许的资源目标 CIDR 会加入源节点对应网关 Peer 的 AllowedIPs（IP 级路由控制），端口作为元数据保存。</p>
      <div class="ml-auto flex gap-2">
        <p v-if="notice" class="mb-2 self-end text-xs text-emerald-300">{{ notice }}</p>
        <button
          class="btn-primary"
          :class="{ 'animate-pulse ring-2 ring-amber-400/60': pendingChanges > 0 }"
          :disabled="!app.canOperate || !selectedNetworkId || publishing"
          :title="pendingChanges ? `有 ${pendingChanges} 项未发布的变更` : '发布当前网络配置'"
          @click="publishChanges"
        >
          <svg v-if="publishing" viewBox="0 0 24 24" fill="none" class="h-4 w-4 animate-spin" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" /></svg>
          {{ publishing ? '发布中…' : pendingChanges ? `保存并发布（${pendingChanges} 项变更）` : '保存并发布' }}
        </button>
      </div>
    </div>

    <!-- 未发布变更提示 -->
    <div v-if="pendingChanges > 0" class="flex items-center gap-3 rounded-xl bg-amber-500/10 px-4 py-2.5 ring-1 ring-amber-500/30">
      <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4 shrink-0 text-amber-400" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" /></svg>
      <p class="text-xs text-amber-300">有 {{ pendingChanges }} 项资源/策略变更尚未发布到节点，点击右上角「保存并发布」生效。</p>
    </div>

    <!-- 自定义拓扑提示 -->
    <div v-if="selectedNetwork && selectedNetwork.topology === 'custom'" class="flex items-center gap-3 rounded-xl bg-cyan-500/10 px-4 py-2.5 ring-1 ring-cyan-500/30">
      <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4 shrink-0 text-cyan-400" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z" /></svg>
      <p class="text-xs leading-relaxed text-cyan-300">自定义拓扑：策略路由只会下发到「源 ↔ 网关」已配对的节点组合，未配对组合不会生效。请到「系统设置 → 网络 → 自定义 Peer」维护配对关系。</p>
    </div>

    <p v-if="error" class="rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ error }}</p>

    <!-- 空状态：没有网络 -->
    <div v-if="!mesh.networks.length" class="panel p-8 text-center">
      <p class="text-sm text-slate-300">还没有可用的网络</p>
      <p class="mx-auto mt-1 max-w-xl text-xs leading-relaxed text-slate-500">访问策略作用于具体网络。请先在「系统设置 → 项目与网络」中创建项目与网络。</p>
      <div class="mt-4 flex justify-center gap-2.5">
        <button class="btn-primary" @click="router.push({ name: 'settings' })">前往系统设置</button>
      </div>
    </div>

    <div v-else class="grid min-h-0 flex-1 grid-cols-1 gap-5 xl:grid-cols-2">
      <!-- 资源 -->
      <div class="panel min-h-0 overflow-y-auto p-5">
        <div class="mb-3 flex items-center justify-between">
          <div>
            <h2 class="text-sm font-semibold text-white">资源（{{ resources.length }}）</h2>
            <p class="mt-0.5 text-xs text-slate-500">节点上的可访问服务：目标 CIDR + 可选端口。</p>
          </div>
        </div>
        <p v-if="!selectedNetworkId" class="py-6 text-center text-xs text-slate-500">请先选择网络</p>
        <p v-else-if="loading && !resources.length" class="py-6 text-center text-xs text-slate-500">加载中…</p>
        <template v-else>
          <div class="space-y-2">
            <div v-for="resource in resources" :key="resource.id" class="rounded-xl bg-ink-800/60 p-3.5 ring-1 ring-ink-600">
              <div class="flex items-center gap-2.5">
                <p class="min-w-0 flex-1 truncate text-sm font-medium text-slate-200">{{ resource.name }}</p>
                <span class="chip max-w-[14rem] shrink-0 truncate bg-cyan-500/10 font-mono text-cyan-300 ring-1 ring-cyan-500/30" :title="resourceChip(resource)">{{ resourceChip(resource) }}</span>
                <button v-if="app.canOperate" class="chip shrink-0 bg-slate-500/10 text-slate-300 ring-1 ring-slate-500/30" @click="editResource(resource)">编辑</button>
                <button v-if="app.canOperate" class="chip shrink-0 bg-red-500/10 text-red-300 ring-1 ring-red-500/30" @click="removeResource(resource)">删除</button>
              </div>
              <p class="mt-1 flex items-center gap-1.5 text-[11px] text-slate-500">
                <span class="truncate">网关：{{ gatewayLabel(resource) }}</span>
                <template v-if="gatewayAgent(resource)">
                  <span class="chip shrink-0 bg-violet-500/10 text-violet-300 ring-1 ring-violet-500/30">{{ agentRoleLabel(gatewayAgent(resource)) }}</span>
                  <span v-if="!gatewayAgent(resource)!.enabled" class="shrink-0 text-amber-400">已停用</span>
                  <span v-else-if="gatewayAgent(resource)!.status === 'offline'" class="shrink-0 text-amber-400">离线</span>
                </template>
                <span v-if="resource.description" class="truncate text-slate-600">· {{ resource.description }}</span>
              </p>
            </div>
            <p v-if="!resources.length" class="py-5 text-center text-xs text-slate-500">暂无资源</p>
          </div>
          <div class="mt-4 rounded-xl border border-ink-700 bg-ink-900/50 p-4">
            <div class="mb-3 flex items-center justify-between">
              <p class="text-xs font-semibold text-slate-300">{{ editingResourceId ? '编辑资源' : '新建资源' }}</p>
              <button v-if="editingResourceId" class="text-xs text-cyan-300" @click="resetResourceForm">取消编辑</button>
            </div>
            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div class="sm:col-span-2"><label class="label">资源名称</label><input v-model="resourceForm.name" class="input" placeholder="如：数据库 PostgreSQL" /></div>
              <div class="sm:col-span-2">
                <label class="label">网关节点（资源所在节点）</label>
                <select v-model="resourceForm.gateway_node_id" class="input">
                  <option value="">选择节点</option>
                  <optgroup v-if="agentNodes.length" label="节点（Agent）">
                    <option v-for="a in agentNodes" :key="a.id" :value="a.id">[{{ agentStatusText(a) }}] {{ a.name }}（{{ a.address }}）</option>
                  </optgroup>
                  <optgroup v-if="clientNodes.length" label="客户端设备">
                    <option v-for="a in clientNodes" :key="a.id" :value="a.id">[{{ agentStatusText(a) }}] {{ a.name }}（{{ a.address }}）</option>
                  </optgroup>
                </select>
                <p v-if="!networkNodes.length" class="mt-1 text-[11px] text-amber-400">该网络暂无节点，接入节点后再配置资源。</p>
              </div>
              <div>
                <label class="label">目标 CIDR（留空 = 节点地址/32）</label>
                <input v-model="resourceForm.target" class="input font-mono" placeholder="10.0.0.0/24" />
                <p v-if="defaultTargetPreview" class="mt-1 text-[11px] text-slate-600">将使用 <span class="font-mono text-slate-400">{{ defaultTargetPreview }}</span></p>
              </div>
              <div><label class="label">端口（可选）</label><input v-model.number="resourceForm.port" type="number" min="0" max="65535" class="input" /></div>
              <div>
                <label class="label">协议</label>
                <select v-model="resourceForm.protocol" class="input">
                  <option value="any">任意</option>
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                </select>
              </div>
              <div><label class="label">描述（可选）</label><input v-model="resourceForm.description" class="input" /></div>
            </div>
            <div class="mt-3 flex justify-end"><button class="btn-secondary" :disabled="!app.canOperate || savingResource" @click="saveResource">{{ savingResource ? '保存中…' : editingResourceId ? '保存修改' : '创建资源' }}</button></div>
          </div>
        </template>
      </div>

      <!-- 策略 -->
      <div class="panel min-h-0 overflow-y-auto p-5">
        <div class="mb-3 flex items-center justify-between">
          <div>
            <h2 class="text-sm font-semibold text-white">策略（{{ policies.length }}）</h2>
            <p class="mt-0.5 text-xs text-slate-500">定义哪些源节点可以访问哪些资源；源留空表示网络内全部节点。</p>
          </div>
        </div>
        <p v-if="!selectedNetworkId" class="py-6 text-center text-xs text-slate-500">请先选择网络</p>
        <p v-else-if="loading && !policies.length" class="py-6 text-center text-xs text-slate-500">加载中…</p>
        <template v-else>
          <div class="space-y-2">
            <div v-for="policy in policies" :key="policy.id" class="rounded-xl bg-ink-800/60 p-3.5 ring-1 ring-ink-600">
              <div class="flex flex-wrap items-center gap-2.5">
                <p class="min-w-0 flex-1 truncate text-sm font-medium text-slate-200">{{ policy.name }}</p>
                <span class="chip shrink-0 ring-1" :class="policy.enabled ? 'bg-emerald-500/10 text-emerald-300 ring-emerald-500/30' : 'bg-slate-500/10 text-slate-400 ring-slate-500/30'">{{ policy.enabled ? '已启用' : '已停用' }}</span>
                <button class="relative h-5 w-9 shrink-0 rounded-full transition" :class="policy.enabled ? 'bg-emerald-500' : 'bg-ink-600'" :disabled="!app.canOperate || togglingPolicyId === policy.id" :title="policy.enabled ? '点击停用' : '点击启用'" :aria-label="policy.enabled ? '停用策略' : '启用策略'" @click="togglePolicy(policy)">
                  <span class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all" :class="policy.enabled ? 'left-[18px]' : 'left-0.5'"></span>
                </button>
                <button v-if="app.canOperate" class="chip shrink-0 bg-slate-500/10 text-slate-300 ring-1 ring-slate-500/30" @click="editPolicy(policy)">编辑</button>
                <button v-if="app.canOperate" class="chip shrink-0 bg-red-500/10 text-red-300 ring-1 ring-red-500/30" @click="removePolicy(policy)">删除</button>
              </div>
              <p class="mt-1.5 text-[11px] text-slate-500">
                源：{{ policy.source_label ? `标签 ${policy.source_label}` + (labelMatchCount(policy.source_label) ? `（当前匹配 ${labelMatchCount(policy.source_label)} 个节点）` : '（当前无节点匹配）') : policy.source_node_ids.length ? policy.source_node_ids.map((id) => mesh.agentById(id)?.name || id).join('、') : '全部节点' }}
              </p>
              <p class="mt-1 text-[11px] text-slate-500">资源：</p>
              <div class="mt-1 flex flex-wrap gap-1.5">
                <span v-for="id in policy.resource_ids" :key="id" class="chip bg-ink-900/70 font-mono text-[11px] text-cyan-300 ring-1 ring-ink-600">
                  {{ resourceName(id) }}
                  <span v-if="resourceDetail(id)" class="ml-1 text-slate-500">{{ resourceChip(resourceDetail(id)!) }}</span>
                </span>
                <span v-if="!policy.resource_ids.length" class="text-[11px] text-amber-400">未选择资源（策略不生效）</span>
              </div>
              <p v-if="policy.enabled && policyUnlinkedCount(policy)" class="mt-1.5 rounded-lg bg-amber-500/10 px-2.5 py-1.5 text-[11px] leading-relaxed text-amber-300 ring-1 ring-amber-500/20">
                {{ policyUnlinkedCount(policy) }} 对「源 ↔ 网关」在当前拓扑中未互联，对应路由不会下发{{ selectedNetwork?.topology === 'custom' ? '；请在 系统设置 → 自定义 Peer 中添加配对' : '；Hub-Spoke 拓扑请确保一方为 Hub' }}
              </p>
            </div>
            <p v-if="!policies.length" class="py-5 text-center text-xs text-slate-500">暂无策略</p>
          </div>
          <div class="mt-4 rounded-xl border border-ink-700 bg-ink-900/50 p-4">
            <div class="mb-3 flex items-center justify-between">
              <p class="text-xs font-semibold text-slate-300">{{ editingPolicyId ? '编辑策略' : '新建策略' }}</p>
              <button v-if="editingPolicyId" class="text-xs text-cyan-300" @click="resetPolicyForm">取消编辑</button>
            </div>
            <div class="grid grid-cols-1 gap-3">
              <div><label class="label">策略名称</label><input v-model="policyForm.name" class="input" placeholder="如：运维组访问数据库" /></div>
              <div>
                <label class="label">源标签选择器（key=value，可选）</label>
                <input v-model="policyForm.source_label" class="input font-mono" placeholder="如：team=ops（与节点选择二选一）" @input="onSourceLabelInput" />
                <p class="mt-1 text-[11px] text-slate-600">标签与节点二选一；留空则作用于网络内全部节点。<span v-if="policyForm.source_label.trim()" class="text-cyan-300">当前匹配 {{ labelMatchCount(policyForm.source_label) }} 个节点（发布时以节点实际标签为准）</span></p>
              </div>
              <div>
                <label class="label">源节点（可多选，填写标签后禁用）</label>
                <div class="flex max-h-28 flex-wrap gap-1.5 overflow-y-auto rounded-lg bg-ink-950/50 p-2 ring-1 ring-ink-600" :class="{ 'opacity-50': policyForm.source_label.trim() }">
                  <button
                    v-for="a in networkNodes"
                    :key="a.id"
                    class="chip transition"
                    :class="policyForm.source_node_ids.includes(a.id) ? 'bg-cyan-500/15 text-cyan-300 ring-1 ring-cyan-500/40' : 'bg-ink-800 text-slate-400 ring-1 ring-ink-600 hover:text-slate-200'"
                    :disabled="Boolean(policyForm.source_label.trim())"
                    @click="toggleSourceNode(a.id)"
                  >
                    {{ a.name }}
                  </button>
                  <span v-if="!networkNodes.length" class="text-[11px] text-slate-500">该网络暂无节点（仍可保存「全部节点」策略）</span>
                </div>
              </div>
              <div>
                <label class="label">允许访问的资源（可多选）</label>
                <div class="flex max-h-28 flex-wrap gap-1.5 overflow-y-auto rounded-lg bg-ink-950/50 p-2 ring-1 ring-ink-600">
                  <button
                    v-for="resource in resources"
                    :key="resource.id"
                    class="chip transition"
                    :class="policyForm.resource_ids.includes(resource.id) ? 'bg-emerald-500/15 text-emerald-300 ring-1 ring-emerald-500/40' : 'bg-ink-800 text-slate-400 ring-1 ring-ink-600 hover:text-slate-200'"
                    @click="toggleResource(resource.id)"
                  >
                    {{ resource.name }}
                  </button>
                  <span v-if="!resources.length" class="text-[11px] text-amber-400">请先在左侧创建资源</span>
                </div>
              </div>
              <label class="flex items-center gap-2 text-xs text-slate-400"><input v-model="policyForm.enabled" type="checkbox" class="accent-emerald-500" />启用策略</label>
            </div>
            <div class="mt-3 flex justify-end"><button class="btn-secondary" :disabled="!app.canOperate || savingPolicy" @click="savePolicy">{{ savingPolicy ? '保存中…' : editingPolicyId ? '保存修改' : '创建策略' }}</button></div>
          </div>
        </template>
      </div>
    </div>

    <!-- 出口网关（Egress） -->
    <div v-if="selectedNetworkId" class="panel shrink-0 p-5">
      <h2 class="text-sm font-semibold text-white">出口网关（Egress）</h2>
      <p class="mt-0.5 text-xs text-slate-500">指定一个节点作为对外出口，其他节点到该节点的 AllowedIPs 会加入下面的 CIDR（如 0.0.0.0/0 表示全部流量经出口网关转发）。保存后需「保存并发布」生效。</p>
      <div v-if="egressNodeId && egressCIDRsText.split(',').some((item) => item.trim() === '0.0.0.0/0')" class="mt-3 rounded-lg bg-amber-500/10 px-3 py-2 text-[11px] leading-relaxed text-amber-300 ring-1 ring-amber-500/30">
        已配置默认路由 0.0.0.0/0：所有未命中更具体路由的流量都会经出口网关转发。WireGuard 按最长前缀匹配，访问策略中的明细 CIDR 仍优先于默认路由。
      </div>
      <div class="mt-4 flex flex-wrap items-end gap-3">
        <div>
          <label class="label">出口网关节点</label>
          <select v-model="egressNodeId" class="input !w-64" @change="egressDirty = true">
            <option value="">不启用出口网关</option>
            <optgroup v-if="agentNodes.length" label="节点（Agent）">
              <option v-for="a in agentNodes" :key="a.id" :value="a.id">[{{ agentStatusText(a) }}] {{ a.name }}（{{ a.address }}）</option>
            </optgroup>
            <optgroup v-if="clientNodes.length" label="客户端设备">
              <option v-for="a in clientNodes" :key="a.id" :value="a.id">[{{ agentStatusText(a) }}] {{ a.name }}（{{ a.address }}）</option>
            </optgroup>
          </select>
        </div>
        <div class="min-w-64 flex-1"><label class="label">转发 CIDR（逗号分隔）</label><input v-model="egressCIDRsText" class="input font-mono" placeholder="0.0.0.0/0" @input="egressDirty = true" /></div>
        <button v-if="egressDirty" class="btn-ghost !py-1.5 text-xs" title="还原为最近一次保存的出口配置" @click="restoreEgress">还原已保存值</button>
        <button class="btn-secondary" :disabled="!app.canOperate || savingEgress" @click="saveEgress">{{ savingEgress ? '保存中…' : '保存出口配置' }}</button>
      </div>
    </div>
  </div>
</template>
