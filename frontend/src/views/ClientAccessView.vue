<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import QRCode from 'qrcode'
import { api, apiBase, type ApiClientConfig } from '../api'
import EditNodeConfigModal from '../components/EditNodeConfigModal.vue'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import type { Agent, Topology } from '../types'
import { useClipboard } from '../composables/useClipboard'
import { requestConfirm } from '../utils/confirm'

const app = useAppStore()
const mesh = useMeshStore()
const router = useRouter()

const selectedNetworkId = ref('')
const selectedNodeId = ref('')
const config = ref<ApiClientConfig | null>(null)
const loading = ref(false)
const error = ref('')
const qrDataUrl = ref('')
const creating = ref(false)
const refreshing = ref(false)
const newClientName = ref('')
const flashId = ref('')
const editingAgent = ref<Agent | null>(null)
const { copied, copyText } = useClipboard(false, 1400)

const selectedNetwork = computed(() => (selectedNetworkId.value ? mesh.networkById(selectedNetworkId.value) : undefined))
const nodesOfNetwork = computed(() => mesh.agents.filter((agent) => agent.networkId === selectedNetworkId.value))
const clientDevices = computed(() => nodesOfNetwork.value.filter((agent) => agent.labels.includes('wiremesh.client=true')))
const serverNodes = computed(() => nodesOfNetwork.value.filter((agent) => !agent.labels.includes('wiremesh.client=true')))
const selectedAgent = computed(() => (selectedNodeId.value ? mesh.agentById(selectedNodeId.value) : undefined))
const serverUrl = computed(() => (apiBase || location.origin).replace(/\/$/, ''))
const isInsecure = computed(() => serverUrl.value.toLowerCase().startsWith('http://'))

function topologyLabel(topology: Topology) {
  if (topology === 'full-mesh') return '全互联（Full Mesh）'
  if (topology === 'hub-spoke') return '中心辐射（Hub-Spoke）'
  return '自定义（Custom）'
}

watch(selectedNetworkId, () => {
  selectedNodeId.value = ''
  config.value = null
  qrDataUrl.value = ''
  // 优先自动选中客户端设备，其次才是 Agent 节点
  const first = clientDevices.value[0] || nodesOfNetwork.value[0]
  if (first) selectedNodeId.value = first.id
})

/** 把后端英文错误映射为可操作的中文提示 */
function friendlyConfigError(message: string) {
  if (message.includes('no published configuration')) return '该网络尚未发布配置，请到「系统设置 → 网络」发布后再导出'
  if (message.includes('not included in published configuration')) return '该设备未包含在已发布的配置中，请重新发布网络配置后再导出'
  if (message.includes('node not found')) return '设备不存在或已被删除，请刷新列表'
  if (message.includes('network not found')) return '网络不存在或已被删除'
  return message
}

let configRequestID = 0

async function loadConfig() {
  if (!selectedNodeId.value) return
  const current = ++configRequestID
  const guard = () => current === configRequestID
  loading.value = true
  error.value = ''
  try {
    const result = await api.nodeClientConfig(selectedNodeId.value)
    if (!guard()) return
    config.value = result
    qrDataUrl.value = await QRCode.toDataURL(result.content, { width: 480, margin: 1, errorCorrectionLevel: 'M' })
  } catch (reason) {
    if (!guard()) return
    config.value = null
    qrDataUrl.value = ''
    error.value = friendlyConfigError(reason instanceof Error ? reason.message : '配置导出失败')
  } finally {
    if (guard()) loading.value = false
  }
}

watch(selectedNodeId, loadConfig)

/** 清理文件名中的非法字符，避免下载文件名被截断 */
function safeFileName(name: string) {
  return name.replace(/[\\/:*?"<>|]/g, '-').trim() || 'wiremesh-client'
}

function downloadConfig() {
  if (!config.value) return
  const blob = new Blob([config.value.content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = safeFileName(config.value.name) + '.conf'
  anchor.click()
  URL.revokeObjectURL(url)
}

function downloadQR() {
  if (!qrDataUrl.value || !config.value) return
  const anchor = document.createElement('a')
  anchor.href = qrDataUrl.value
  anchor.download = safeFileName(config.value.name) + '-qr.png'
  anchor.click()
}

async function copyConfig() {
  if (!config.value) return
  await copyText(config.value.content, true)
}

async function createClient() {
  const name = newClientName.value.trim()
  if (!name || !selectedNetworkId.value || creating.value) return
  const duplicate = nodesOfNetwork.value.find((agent) => agent.name === name)
  if (duplicate) {
    const confirmed = await requestConfirm({
      title: '同名设备提醒',
      message: `该网络中已存在名为“${name}”的设备，仍要创建吗？`,
      confirmText: '仍要创建',
      variant: 'warning',
    })
    if (!confirmed) return
  }
  creating.value = true
  error.value = ''
  try {
    const network = selectedNetwork.value
    const labels: Record<string, string> = { 'wiremesh.client': 'true' }
    // hub-spoke 拓扑下客户端以 Spoke 角色接入，与 Hub 建立直连
    if (network?.topology === 'hub-spoke') labels['wiremesh.role'] = 'spoke'
    const node = await api.createNode({ network_id: selectedNetworkId.value, name, labels })
    mesh.selectedNetworkId = selectedNetworkId.value
    try {
      const result = await api.publish(selectedNetworkId.value)
      await mesh.waitForDeliveryResult(result)
      mesh.notice = '客户端已创建并发布配置'
    } catch {
      mesh.notice = '客户端已创建，发布配置失败，请在系统设置中手动发布'
    }
    await mesh.refreshNodeTelemetry(true)
    newClientName.value = ''
    selectedNodeId.value = node.id
    flashId.value = node.id
    window.setTimeout(() => { if (flashId.value === node.id) flashId.value = '' }, 2400)
    await nextTick()
    document.getElementById('device-' + node.id)?.scrollIntoView({ block: 'nearest' })
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '创建客户端失败'
  } finally {
    creating.value = false
  }
}

async function removeClient() {
  const id = selectedNodeId.value
  if (!id || !app.isAdmin) return
  const agent = mesh.agentById(id)
  const confirmed = await requestConfirm({
    title: '删除客户端设备',
    message: `确定删除设备“${agent?.name || id}”吗？\n相关 Peer、命令和配置下发记录将一并清理，此操作无法恢复。`,
    confirmText: '删除设备',
    variant: 'danger',
  })
  if (!confirmed) return
  const list = nodesOfNetwork.value
  const index = list.findIndex((item) => item.id === id)
  if (await mesh.removeAgent(id, app.username)) {
    config.value = null
    qrDataUrl.value = ''
    // 自动选中相邻设备，保持右侧面板可用
    const remaining = list.filter((item) => item.id !== id)
    const next = remaining[Math.min(index, remaining.length - 1)]
    selectedNodeId.value = next ? next.id : ''
  }
}

async function refreshDevices() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await mesh.refreshNodeTelemetry(true)
  } finally {
    refreshing.value = false
  }
}

function onEditClose() {
  editingAgent.value = null
  // 编辑可能修改了名称/地址，重新拉取导出配置
  void loadConfig()
}
</script>

<template>
  <div class="flex h-full flex-col gap-5">
    <!-- 工具栏：网络选择 + 拓扑类型 + 创建表单 -->
    <div class="flex flex-wrap items-end gap-3">
      <div>
        <label class="label">网络</label>
        <select v-model="selectedNetworkId" class="input !w-64">
          <option value="">选择网络</option>
          <option v-for="n in mesh.networks" :key="n.id" :value="n.id">{{ n.name }}（{{ n.cidr }}）</option>
        </select>
      </div>
      <span v-if="selectedNetwork" class="chip mb-2 bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">{{ topologyLabel(selectedNetwork.topology) }}</span>
      <div class="flex items-end gap-2">
        <div>
          <label class="label">新客户端名称</label>
          <input v-model="newClientName" class="input w-56" placeholder="如：我的手机" @keyup.enter="createClient" />
        </div>
        <button class="btn-primary" :disabled="!app.canOperate || !selectedNetworkId || !newClientName.trim() || creating" @click="createClient">
          {{ creating ? '创建中…' : '创建客户端' }}
        </button>
      </div>
      <p class="mb-2 text-xs text-slate-500">客户端设备（手机 / 桌面）不运行 Agent，通过导入下方配置文件或扫描二维码接入网络。</p>
    </div>

    <!-- 拓扑语义提示 -->
    <div v-if="selectedNetwork && selectedNetwork.topology === 'custom'" class="flex items-center gap-3 rounded-xl bg-amber-500/10 px-4 py-2.5 ring-1 ring-amber-500/30">
      <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4 shrink-0 text-amber-400" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" /></svg>
      <p class="text-xs leading-relaxed text-amber-300">当前网络为自定义拓扑：新客户端默认不与任何设备建立连接，创建后请到「系统设置 → 网络 → 自定义 Peer」添加配对关系，并重新发布配置。</p>
    </div>
    <div v-else-if="selectedNetwork && selectedNetwork.topology === 'hub-spoke'" class="flex items-center gap-3 rounded-xl bg-cyan-500/10 px-4 py-2.5 ring-1 ring-cyan-500/30">
      <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4 shrink-0 text-cyan-400" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M11.25 11.25l.041-.02a.75.75 0 011.063.852l-.708 2.836a.75.75 0 001.063.853l.041-.021M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9-3.75h.008v.008H12V8.25z" /></svg>
      <p class="text-xs leading-relaxed text-cyan-300">当前网络为中心辐射（Hub-Spoke）拓扑：客户端将以 Spoke 角色接入，仅与 Hub 直连；如需访问其他 Spoke，请为 Hub 开启中继模式。</p>
    </div>

    <!-- 空状态：没有网络 -->
    <div v-if="!mesh.networks.length" class="panel p-8 text-center">
      <p class="text-sm text-slate-300">还没有可用的网络</p>
      <p class="mx-auto mt-1 max-w-xl text-xs leading-relaxed text-slate-500">客户端设备需要加入网络后才能导出接入配置。请先在「系统设置 → 项目与网络」中创建项目与网络。</p>
      <div class="mt-4 flex justify-center gap-2.5">
        <button class="btn-primary" @click="router.push({ name: 'settings' })">前往系统设置</button>
        <button class="btn-secondary" @click="router.push({ name: 'nodes' })">接入节点</button>
      </div>
    </div>

    <div v-else class="grid min-h-0 flex-1 grid-cols-1 gap-5 lg:grid-cols-[16rem_1fr]">
      <!-- 设备列表：客户端与 Agent 节点分组 -->
      <div class="panel flex min-h-0 flex-col p-3">
        <div class="mb-2 flex items-center justify-between px-2">
          <p class="text-xs font-semibold text-slate-400">客户端 <span class="text-cyan-300">{{ clientDevices.length }}</span> · 节点 <span class="text-slate-300">{{ serverNodes.length }}</span></p>
          <button class="rounded-md p-1 text-slate-500 transition hover:bg-ink-800 hover:text-slate-300" :disabled="refreshing" title="刷新设备状态" @click="refreshDevices">
            <svg viewBox="0 0 24 24" fill="none" class="h-3.5 w-3.5" :class="{ 'animate-spin': refreshing }" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" /></svg>
          </button>
        </div>
        <div class="min-h-0 flex-1 overflow-y-auto">
          <p v-if="!selectedNetworkId" class="px-2 py-6 text-center text-xs text-slate-600">请先选择网络</p>
          <p v-else-if="!nodesOfNetwork.length" class="px-2 py-6 text-center text-xs text-slate-600">该网络暂无设备；创建客户端或接入 Agent 节点后会自动出现在这里</p>
          <template v-else>
            <p v-if="clientDevices.length" class="px-2 pb-1 pt-1 text-[11px] font-medium text-slate-600">客户端设备</p>
            <button
              v-for="a in clientDevices"
              :id="'device-' + a.id"
              :key="a.id"
              class="flex w-full items-center gap-2.5 rounded-lg px-3 py-2.5 text-left transition ring-1"
              :class="selectedNodeId === a.id ? 'bg-emerald-500/10 ring-emerald-500/40' : flashId === a.id ? 'bg-cyan-500/10 ring-cyan-500/40' : 'ring-transparent hover:bg-ink-800'"
              @click="selectedNodeId = a.id"
            >
              <span class="h-2 w-2 shrink-0 rounded-full" :class="!a.enabled ? 'bg-slate-600' : 'bg-cyan-400'"></span>
              <span class="min-w-0 flex-1 truncate text-sm text-slate-200">{{ a.name }}</span>
              <span v-if="!a.enabled" class="chip shrink-0 bg-slate-500/10 text-slate-500 ring-1 ring-slate-500/30">停用</span>
              <span v-else class="chip shrink-0 bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">客户端</span>
            </button>
            <p v-if="serverNodes.length" class="px-2 pb-1 pt-2 text-[11px] font-medium text-slate-600">节点（Agent）</p>
            <button
              v-for="a in serverNodes"
              :id="'device-' + a.id"
              :key="a.id"
              class="flex w-full items-center gap-2.5 rounded-lg px-3 py-2.5 text-left transition ring-1"
              :class="selectedNodeId === a.id ? 'bg-emerald-500/10 ring-emerald-500/40' : 'ring-transparent hover:bg-ink-800'"
              @click="selectedNodeId = a.id"
            >
              <span class="h-2 w-2 shrink-0 rounded-full" :class="!a.enabled ? 'bg-slate-600' : a.status === 'online' ? 'bg-emerald-400' : 'bg-slate-500'"></span>
              <span class="min-w-0 flex-1 truncate text-sm text-slate-200">{{ a.name }}</span>
              <span v-if="!a.enabled" class="chip shrink-0 bg-slate-500/10 text-slate-500 ring-1 ring-slate-500/30">停用</span>
              <span v-else class="chip shrink-0 bg-violet-500/10 text-violet-300 ring-1 ring-violet-500/30">节点</span>
            </button>
          </template>
        </div>
      </div>

      <!-- 配置 + QR -->
      <div class="panel flex min-h-0 flex-col p-5">
        <p v-if="error" class="mb-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs leading-relaxed text-red-300 ring-1 ring-red-500/30">{{ error }}</p>
        <div v-if="!selectedNodeId" class="flex flex-1 items-center justify-center text-xs text-slate-500">选择左侧设备查看接入配置</div>
        <div v-else-if="loading" class="flex flex-1 items-center justify-center text-xs text-slate-500">正在生成配置…</div>
        <div v-else-if="config" class="grid min-h-0 flex-1 gap-5 lg:grid-cols-[minmax(0,1fr)_18rem]">
          <div class="flex min-h-0 flex-col">
            <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
              <p class="flex items-center gap-2 text-sm font-semibold text-white">
                {{ config.name }}
                <span v-if="selectedAgent?.labels.includes('wiremesh.client=true')" class="chip bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">客户端</span>
                <span v-else class="chip bg-violet-500/10 text-violet-300 ring-1 ring-violet-500/30">节点</span>
                <span class="text-xs font-normal text-slate-500">隧道地址 {{ config.address }} · {{ selectedNetwork?.name }}</span>
              </p>
              <div class="flex flex-wrap gap-2">
                <button v-if="app.canOperate" class="btn-ghost !py-1.5 text-xs" @click="editingAgent = selectedAgent || null">编辑</button>
                <button class="btn-ghost !py-1.5 text-xs" @click="copyConfig">{{ copied ? '已复制 ✓' : '复制配置' }}</button>
                <button class="btn-ghost !py-1.5 text-xs" @click="downloadConfig">下载 .conf</button>
                <button v-if="app.isAdmin" class="btn-ghost !py-1.5 text-xs text-red-300" @click="removeClient">删除设备</button>
              </div>
            </div>
            <p v-if="isInsecure" class="mb-2 rounded-lg bg-amber-500/10 px-3 py-2 text-[11px] leading-relaxed text-amber-300 ring-1 ring-amber-500/30">
              当前服务为 HTTP 明文传输，复制或下载的配置包含私钥，可能在网络中泄露；生产环境请使用 HTTPS。
            </p>
            <pre class="min-h-0 flex-1 overflow-auto rounded-xl bg-ink-950 p-4 font-mono text-xs leading-5 text-emerald-200/90 ring-1 ring-ink-600">{{ config.content }}</pre>
            <p class="mt-2 flex items-center gap-1.5 text-[11px] text-slate-600">
              <svg viewBox="0 0 24 24" fill="none" class="h-3.5 w-3.5 shrink-0 text-amber-500" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M16.5 10.5V6.75a4.5 4.5 0 10-9 0v3.75m-.75 11.25h10.5a2.25 2.25 0 002.25-2.25v-6.75a2.25 2.25 0 00-2.25-2.25H6.75a2.25 2.25 0 00-2.25 2.25v6.75a2.25 2.25 0 002.25 2.25z" /></svg>
              配置文件包含该设备的 WireGuard 私钥，请妥善保管，勿通过不安全的渠道分享。
            </p>
          </div>
          <div class="flex flex-col items-center gap-3">
            <p class="text-xs text-slate-500">使用 WireGuard 客户端扫码导入</p>
            <img v-if="qrDataUrl" :src="qrDataUrl" alt="WireGuard 配置二维码" class="w-64 max-w-full rounded-xl ring-1 ring-ink-600" />
            <div v-else class="flex h-64 w-64 max-w-full items-center justify-center rounded-xl text-xs text-slate-600 ring-1 ring-ink-600">二维码生成中…</div>
            <button v-if="qrDataUrl" class="btn-ghost !py-1.5 text-xs" @click="downloadQR">下载二维码图片</button>
            <p class="text-center text-[11px] leading-relaxed text-slate-600">二维码与配置文件内容一致，均包含私钥。</p>
          </div>
        </div>
      </div>
    </div>

    <EditNodeConfigModal v-if="editingAgent" :agent="editingAgent" @close="onEditClose" />
  </div>
</template>
