<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, type ApiPeerConfigFile } from '../api'
import type { Agent, WGInterface } from '../types'
import { useMeshStore } from '../stores/mesh'
import { useClipboard } from '../composables/useClipboard'
import { shortKey } from '../utils/format'
import {
  buildPeerBlock,
  joinEndpoint,
  parsePeerBlock,
  peerExists,
  randomWireGuardKey,
  splitEndpoint,
  splitPeerBlocks,
  upsertPeerBlock,
  validateAllowedIPs,
  validateEndpoint,
  validatePeerConfigDraft,
  validWireGuardInterfaceName,
  validWireGuardKey,
} from '../utils/wireguard'
import BaseModal from './BaseModal.vue'

type EditMode = 'manual' | 'form'
type PeerSource = 'managed' | 'manual'

const props = withDefaults(defineProps<{ agent: Agent; initialMode?: EditMode; initialInterface?: string }>(), {
  initialMode: 'manual',
  initialInterface: '',
})
const emit = defineEmits<{ close: [] }>()
const mesh = useMeshStore()

const loading = ref(true)
const saving = ref(false)
const error = ref('')
const hasPending = ref(false)
const files = ref<ApiPeerConfigFile[]>([])
const selectedInterface = ref('')
const draft = ref('')
const mode = ref<EditMode>(props.initialMode)

const pickerOpen = ref(false)
const peerSearch = ref('')
const expandedPeerIndex = ref<number | null>(null)
const editingPeerIndex = ref<number | null>(null)
const editingOriginalPublicKey = ref('')
const addingPeer = ref(props.initialMode === 'form')
const { copied: copiedRemoteConfig, copyText: copyTextToClipboard, clearCopied: clearRemoteCopied } = useClipboard(false)

const form = reactive({
  source: 'managed' as PeerSource,
  remoteNodeId: '',
  remoteName: '',
  remoteInterface: '',
  remotePublicKey: '',
  remoteAllowedIPs: '',
  remoteEndpointHost: '',
  remoteEndpointPort: '',
  localAllowedIPs: '',
  localEndpointHost: '',
  localEndpointPort: '',
  presharedKey: '',
  keepalive: '25',
})

const preview = reactive({
  localContent: '',
  remoteContent: '',
  localInterface: '',
  remoteNodeId: '',
  remoteInterface: '',
  remoteName: '',
  localUpdatesExisting: false,
  remoteUpdatesExisting: false,
  remoteEnabled: false,
})

type PeerFormValues = Partial<typeof form>
type PeerConfigSaveResult = Awaited<ReturnType<typeof api.updateNodePeerConfig>>

const currentFile = computed(() => files.value.find((file) => file.interface === selectedInterface.value))
const currentLocalInterface = computed(() => props.agent.interfaces.find((iface) => iface.name === selectedInterface.value) || props.agent.interfaces[0])
const remoteAgent = computed(() => mesh.agents.find((agent) => agent.id === form.remoteNodeId))
const previewReady = computed(() => preview.localContent.trim() !== '')
const localPublicKey = computed(() => currentLocalInterface.value?.publicKey || props.agent.publicKey || '')
const peerRows = computed(() => splitPeerBlocks(draft.value).map((block, index) => ({ ...parsePeerBlock(block), index, raw: block })))
const peerFormVisible = computed(() => addingPeer.value || editingPeerIndex.value !== null)
const peerFormTitle = computed(() => editingPeerIndex.value !== null ? `配置 Peer ${editingPeerIndex.value + 1}` : '添加新 Peer')

const interfaceOptions = computed(() => {
  const names = new Set<string>()
  files.value.forEach((file) => { if (file.interface) names.add(file.interface) })
  props.agent.interfaces.forEach((iface) => { if (iface.name) names.add(iface.name) })
  const selector = props.agent.interfaceSelector?.trim()
  if (selector && selector !== 'auto' && selector !== '*') {
    selector.split(',').map((item) => item.trim()).filter(Boolean).forEach((name) => names.add(name))
  }
  if (!names.size) names.add('wg0')
  return [...names]
})

const managedPeerCandidates = computed(() => {
  const keyword = peerSearch.value.trim().toLowerCase()
  return mesh.agents
    .filter((agent) => agent.id !== props.agent.id)
    .filter((agent) => agent.networkId === props.agent.networkId || agent.interfaces.some((iface) => iface.networkId === props.agent.networkId))
    .filter((agent) => {
      if (!keyword) return true
      return `${agent.name} ${agent.hostname} ${agent.address} ${agent.publicIP} ${agent.interfaces.map((iface) => `${iface.name} ${iface.tunnelIP} ${iface.publicKey}`).join(' ')}`.toLowerCase().includes(keyword)
    })
    .sort((a, b) => Number(b.status === 'online') - Number(a.status === 'online') || a.name.localeCompare(b.name))
})

function clearPreview() {
  Object.assign(preview, {
    localContent: '',
    remoteContent: '',
    localInterface: '',
    remoteNodeId: '',
    remoteInterface: '',
    remoteName: '',
    localUpdatesExisting: false,
    remoteUpdatesExisting: false,
    remoteEnabled: false,
  })
  clearRemoteCopied()
}

function resetPeerForm(values: PeerFormValues, defaultKeepalive = true) {
  Object.assign(form, values)
  if (defaultKeepalive && !form.keepalive.trim()) form.keepalive = '25'
  applyLocalDefaults(false)
  clearPreview()
}

function activatePeerForm(adding: boolean, editingIndex: number | null, originalPublicKey = '', expandedIndex: number | null = null) {
  addingPeer.value = adding
  editingPeerIndex.value = editingIndex
  editingOriginalPublicKey.value = originalPublicKey
  expandedPeerIndex.value = expandedIndex
  pickerOpen.value = false
  mode.value = 'form'
}

function closePeerForm() {
  addingPeer.value = false
  editingPeerIndex.value = null
  editingOriginalPublicKey.value = ''
  pickerOpen.value = false
  clearPreview()
}

function fileLabel(file?: ApiPeerConfigFile) {
  if (!file) return '未上传配置快照'
  const stamp = file.updated_at ? new Date(file.updated_at).toLocaleString('zh-CN') : '未知时间'
  return `${file.path || '/etc/wireguard/' + file.interface + '.conf'} · ${stamp}`
}

function selectInterface(name: string) {
  selectedInterface.value = name
  draft.value = files.value.find((file) => file.interface === name)?.content || ''
  applyLocalDefaults(false)
  clearPreview()
}

function switchMode(next: EditMode) {
  mode.value = next
  if (next === 'form' && !peerFormVisible.value && !peerRows.value.length) startNewPeerForm()
}

function primaryInterface(agent: Agent, preferredNetworkId = props.agent.networkId) {
  return agent.interfaces.find((iface) => iface.networkId === preferredNetworkId) || agent.interfaces[0]
}

function interfaceTunnelPrefix(iface?: WGInterface, fallback = '') {
  const value = iface?.tunnelIP || fallback
  if (!value) return ''
  return value.includes('/') ? value : value + '/32'
}

function endpointDefaults(agent: Agent, iface?: WGInterface) {
  const parsed = splitEndpoint(agent.publicIP || '')
  return {
    host: parsed.host,
    port: parsed.port || String(iface?.listenPort || agent.listenPort || 51820),
  }
}

function applyLocalDefaults(force = false) {
  const iface = currentLocalInterface.value
  if (force || !form.localAllowedIPs.trim()) form.localAllowedIPs = interfaceTunnelPrefix(iface, props.agent.address)
  const endpoint = endpointDefaults(props.agent, iface)
  if (force || !form.localEndpointHost.trim()) form.localEndpointHost = endpoint.host
  if (force || !form.localEndpointPort.trim()) form.localEndpointPort = endpoint.port
}

function fillFromManagedAgent(agent: Agent, iface = primaryInterface(agent)) {
  const endpoint = endpointDefaults(agent, iface)
  resetPeerForm({
    source: 'managed',
    remoteNodeId: agent.id,
    remoteName: agent.name,
    remoteInterface: iface?.name || 'wg0',
    remotePublicKey: iface?.publicKey || agent.publicKey || '',
    remoteAllowedIPs: interfaceTunnelPrefix(iface, agent.address),
    remoteEndpointHost: endpoint.host,
    remoteEndpointPort: endpoint.port,
  })
  activatePeerForm(true, null)
}

function startManualPeer() {
  resetPeerForm({
    source: 'manual',
    remoteNodeId: '',
    remoteName: '',
    remoteInterface: 'wg0',
    remotePublicKey: '',
    remoteAllowedIPs: '',
    remoteEndpointHost: '',
    remoteEndpointPort: '',
    presharedKey: '',
  })
  pickerOpen.value = false
  mode.value = 'form'
}

function startNewPeerForm() {
  startManualPeer()
  activatePeerForm(true, null)
}

function loadPeerIntoForm(index: number) {
  const peer = peerRows.value[index]
  if (!peer) return
  resetPeerForm({
    source: 'manual',
    remoteNodeId: '',
    remoteName: peer.label,
    remoteInterface: 'wg0',
    remotePublicKey: peer.publicKey,
    remoteAllowedIPs: peer.allowedIPs,
    remoteEndpointHost: peer.endpointHost,
    remoteEndpointPort: peer.endpointPort,
    presharedKey: peer.presharedKey,
    keepalive: peer.keepalive || '',
  }, false)
  activatePeerForm(false, index, peer.publicKey, index)
}

function togglePeer(index: number) {
  if (expandedPeerIndex.value === index) {
    expandedPeerIndex.value = null
    return
  }
  expandedPeerIndex.value = index
}

function generatePresharedKey() {
  form.presharedKey = randomWireGuardKey()
  error.value = ''
  clearPreview()
}

async function copyRemoteConfig() {
  if (!preview.remoteContent.trim()) return
  if (!await copyTextToClipboard(preview.remoteContent, true)) {
    error.value = '复制失败，请手动选中对端配置复制'
  }
}

function validateForm() {
  if (!selectedInterface.value.trim()) return '请选择本端接口'
  if (!validWireGuardInterfaceName(selectedInterface.value)) return '本端接口名格式不正确'
  if (!form.remotePublicKey.trim()) return '对端公钥不能为空'
  if (!validWireGuardKey(form.remotePublicKey)) return '对端公钥格式不正确，应为 32 字节 WireGuard base64 公钥'
  if (form.presharedKey.trim() && !validWireGuardKey(form.presharedKey)) return '预共享密钥格式不正确，应为 32 字节 WireGuard base64 密钥'
  const remoteAllowedError = validateAllowedIPs('对端 AllowedIPs', form.remoteAllowedIPs)
  if (remoteAllowedError) return remoteAllowedError
  const remoteEndpointError = validateEndpoint('对端 Endpoint', form.remoteEndpointHost, form.remoteEndpointPort)
  if (remoteEndpointError) return remoteEndpointError
  if (form.keepalive.trim()) {
    const keepalive = Number(form.keepalive)
    if (!Number.isInteger(keepalive) || keepalive < 0 || keepalive > 65535) return 'KeepAlive 必须在 0-65535 之间'
  }
  if (!localPublicKey.value || !validWireGuardKey(localPublicKey.value)) return '本端接口没有可用公钥，请等待 Agent 上传 WireGuard 接口信息'
  const localAllowedError = validateAllowedIPs('本端 AllowedIPs', form.localAllowedIPs)
  if (localAllowedError) return localAllowedError
  const localEndpointError = validateEndpoint('本端 Endpoint', form.localEndpointHost, form.localEndpointPort)
  if (localEndpointError) return localEndpointError
  if (form.source === 'managed') {
    if (!form.remoteNodeId) return '请选择一个已存在的对端节点'
  }
  return ''
}

async function generatePreview() {
  error.value = ''
  clearPreview()
  applyLocalDefaults(true)
  const validation = validateForm()
  if (validation) {
    error.value = validation
    return false
  }
  const localIface = selectedInterface.value.trim()
  const remoteEndpoint = joinEndpoint(form.remoteEndpointHost, form.remoteEndpointPort)
  const localPeerBlock = buildPeerBlock(form.remotePublicKey, form.remoteAllowedIPs, remoteEndpoint, form.presharedKey, form.keepalive)
  const localMatchPublicKey = editingOriginalPublicKey.value || form.remotePublicKey
  preview.localContent = upsertPeerBlock(draft.value, localPeerBlock, localMatchPublicKey)
  preview.localInterface = localIface
  preview.localUpdatesExisting = peerExists(draft.value, localMatchPublicKey)

  const localEndpoint = joinEndpoint(form.localEndpointHost, form.localEndpointPort)
  preview.remoteContent = buildPeerBlock(localPublicKey.value, form.localAllowedIPs, localEndpoint, form.presharedKey, form.keepalive)
  preview.remoteNodeId = form.remoteNodeId
  preview.remoteInterface = form.remoteInterface.trim() || 'wg0'
  preview.remoteName = form.remoteName || remoteAgent.value?.name || '对端'
  preview.remoteUpdatesExisting = false
  preview.remoteEnabled = true
  return true
}

async function saveManual() {
  const validation = validatePeerConfigDraft(draft.value)
  if (validation) {
    error.value = validation
    return
  }
  if (!selectedInterface.value.trim()) {
    error.value = '请选择或填写接口名'
    return
  }
  await savePeerConfig(selectedInterface.value.trim(), draft.value, (result) => result.message || (result.offline ? 'Peer 配置已保存，客户端上线后下发' : 'Peer 配置已保存并下发'), '保存 Peer 配置失败')
}

async function saveGenerated() {
  if (!previewReady.value) {
    await generatePreview()
    return
  }
  await savePeerConfig(preview.localInterface, preview.localContent, (result) => (result.message || 'Peer 配置已保存并下发') + '；对端配置已生成，可复制后粘贴到对端', '保存生成的 Peer 配置失败')
}

async function savePeerConfig(interfaceName: string, content: string, successMessage: (result: PeerConfigSaveResult) => string, fallbackError: string) {
  saving.value = true
  error.value = ''
  try {
    const result = await api.updateNodePeerConfig(props.agent.id, { interface: interfaceName, content })
    mesh.notice = successMessage(result)
    await mesh.refresh()
    emit('close')
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : fallbackError
  } finally {
    saving.value = false
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await api.nodePeerConfig(props.agent.id)
    hasPending.value = result.has_pending
    const source = result.has_pending && result.pending_files?.length ? result.pending_files : result.files
    files.value = source || []
    const initial = props.initialInterface || files.value[0]?.interface || props.agent.interfaces[0]?.name || interfaceOptions.value[0] || 'wg0'
    selectInterface(initial)
    applyLocalDefaults(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载 Peer 配置失败'
  } finally {
    loading.value = false
  }
}

watch(
  () => props.agent.id,
  () => {
    clearPreview()
    void load()
  },
)

onMounted(load)
</script>

<template>
  <BaseModal panel-class="h-[86vh] max-h-[90vh] min-h-[36rem] max-w-6xl" body-class="p-5" @close="emit('close')">
    <template #header>
        <div>
          <h3 class="text-base font-semibold text-white">{{ agent.name }} · 编辑 Peer</h3>
          <p class="mt-1 text-xs text-slate-500">手动模式保留原始文本编辑；表单模式用单窗填写对端信息并生成两端 Peer 配置。</p>
        </div>
    </template>

    <template #toolbar>
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-ink-700 px-6 py-3">
        <div class="flex rounded-xl bg-ink-900 p-1 ring-1 ring-ink-700">
          <button class="rounded-lg px-3 py-1.5 text-xs font-medium transition" :class="mode === 'manual' ? 'bg-cyan-500/15 text-cyan-300' : 'text-slate-500 hover:text-slate-300'" @click="switchMode('manual')">手动编辑</button>
          <button class="rounded-lg px-3 py-1.5 text-xs font-medium transition" :class="mode === 'form' ? 'bg-cyan-500/15 text-cyan-300' : 'text-slate-500 hover:text-slate-300'" @click="switchMode('form')">表单填写</button>
        </div>
        <button v-if="mode === 'form'" class="btn-secondary !py-1.5 text-xs" @click="startNewPeerForm">+ 添加新 Peer</button>
      </div>
    </template>

    <template #default>
        <p v-if="error" class="mb-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300">{{ error }}</p>
        <p v-if="hasPending" class="mb-3 rounded-lg bg-amber-500/10 px-3 py-2 text-xs text-amber-300 ring-1 ring-amber-500/30">当前存在待下发 Peer 配置，编辑器优先显示待下发内容。</p>

        <div class="grid h-full min-h-[28rem] gap-4 lg:grid-cols-[13rem_1fr]">
          <template v-if="loading">
            <div class="space-y-2">
              <label class="label">接口</label>
              <div class="space-y-2">
                <div v-for="index in 4" :key="index" class="h-9 animate-pulse rounded-lg bg-ink-800/80 ring-1 ring-ink-700"></div>
              </div>
            </div>
            <div class="min-w-0">
              <div class="mb-2 flex items-center justify-between gap-2">
                <div class="h-4 w-72 max-w-full animate-pulse rounded bg-ink-800"></div>
                <div class="h-8 w-20 animate-pulse rounded-xl bg-ink-800"></div>
              </div>
              <div class="flex h-[52vh] min-h-[18rem] items-center justify-center border-y border-ink-700 bg-[#05070a]">
                <div class="text-center">
                  <div class="mx-auto h-8 w-8 animate-spin rounded-full border-2 border-cyan-400/20 border-t-cyan-300"></div>
                  <p class="mt-3 text-sm text-slate-500">正在加载 Peer 配置…</p>
                </div>
              </div>
            </div>
          </template>

          <template v-else>
            <div class="space-y-2">
              <label class="label">本端接口</label>
              <button
                v-for="name in interfaceOptions"
                :key="name"
                class="flex w-full items-center justify-between border border-ink-700 px-3 py-2 text-left font-mono text-xs transition hover:bg-ink-800"
                :class="selectedInterface === name ? 'bg-cyan-500/10 text-cyan-300' : 'bg-ink-900 text-slate-400'"
                @click="selectInterface(name)"
              >
                <span>{{ name }}</span>
                <span v-if="files.some((file) => file.interface === name)" class="text-[10px] text-emerald-300">snapshot</span>
              </button>
              <input v-model="selectedInterface" class="input font-mono text-xs" placeholder="wg0" @change="selectInterface(selectedInterface)" />
              <div v-if="mode === 'form'" class="rounded-xl bg-ink-900/70 p-3 text-[11px] leading-relaxed text-slate-500 ring-1 ring-ink-700">
                <p>本端公钥</p>
                <p class="mt-1 truncate font-mono text-slate-300" :title="localPublicKey">{{ shortKey(localPublicKey) || '等待 Agent 上传' }}</p>
                <p class="mt-2">接口私钥属于 [Interface]，不会在 Peer 快捷表单里编辑。</p>
              </div>
            </div>

            <div v-if="mode === 'manual'" class="min-w-0">
              <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
                <p class="truncate text-xs text-slate-500">{{ fileLabel(currentFile) }}</p>
                <button class="btn-secondary !py-1.5 text-xs" :disabled="loading" @click="load">重新加载</button>
              </div>
              <textarea
                v-model="draft"
                spellcheck="false"
                class="h-[52vh] w-full resize-none border-y border-ink-700 bg-[#05070a] px-3 py-2 font-mono text-xs leading-5 text-slate-300 outline-none placeholder:text-slate-700 focus:border-cyan-500/50"
                placeholder="[Peer]&#10;PublicKey = ...&#10;AllowedIPs = 10.88.88.2/32&#10;Endpoint = example.com:51820&#10;PersistentKeepalive = 25"
              ></textarea>
              <p class="mt-2 text-[11px] leading-relaxed text-slate-500">为避免覆盖接口私钥，这里只允许保存 [Peer] 段落；[Interface]、PrivateKey 等接口配置请继续使用“编辑接口设置”。</p>
            </div>

            <div v-else class="min-w-0 space-y-4" @input="clearPreview" @change="clearPreview">
              <section class="rounded-2xl border border-ink-700 bg-ink-900/70 p-4">
                <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <h4 class="text-sm font-semibold text-white">已有 Peer</h4>
                    <p class="mt-1 text-xs text-slate-500">点击 Peer 展开查看详情；需要修改时再进入编辑。</p>
                  </div>
                  <button class="btn-secondary !py-1.5 text-xs" @click="startNewPeerForm">添加新 Peer</button>
                </div>
                <div class="space-y-2">
                  <div v-for="peer in peerRows" :key="peer.index" class="overflow-hidden rounded-xl border border-ink-700 bg-ink-950/70">
                    <button class="flex w-full items-center justify-between gap-3 px-3 py-2 text-left transition hover:bg-ink-800" @click="togglePeer(peer.index)">
                      <span class="min-w-0">
                        <span class="block text-sm font-medium text-slate-200">Peer {{ peer.index + 1 }}</span>
                        <span class="block truncate font-mono text-[11px] text-slate-500">{{ peer.label }}</span>
                      </span>
                      <span class="flex shrink-0 items-center gap-2 text-[11px] text-slate-500">
                        {{ expandedPeerIndex === peer.index ? '收起' : '查看详情' }}
                        <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4 transition-transform" :class="{ 'rotate-90': expandedPeerIndex === peer.index }" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8.25 4.5l7.5 7.5-7.5 7.5" /></svg>
                      </span>
                    </button>
                    <div v-if="expandedPeerIndex === peer.index" class="border-t border-ink-700 px-3 py-3">
                      <div class="grid gap-2 text-[11px] sm:grid-cols-2">
                        <p class="truncate text-slate-500">PublicKey <span class="ml-1 font-mono text-slate-300" :title="peer.publicKey">{{ shortKey(peer.publicKey) }}</span></p>
                        <p class="truncate text-slate-500">AllowedIPs <span class="ml-1 font-mono text-slate-300">{{ peer.allowedIPs || '未填写' }}</span></p>
                        <p class="truncate text-slate-500">Endpoint <span class="ml-1 font-mono text-slate-300">{{ peer.endpoint || '未填写' }}</span></p>
                        <p class="truncate text-slate-500">KeepAlive <span class="ml-1 font-mono text-slate-300">{{ peer.keepalive || '未填写' }}</span></p>
                      </div>
                      <div class="mt-3 flex justify-end">
                        <button class="rounded-md px-2 py-1 text-[11px] text-cyan-300 transition hover:bg-cyan-500/10" @click="loadPeerIntoForm(peer.index)">编辑此 Peer</button>
                      </div>
                    </div>
                  </div>
                  <p v-if="!peerRows.length" class="rounded-xl border border-dashed border-ink-700 px-3 py-6 text-center text-xs text-slate-500">当前接口还没有 Peer，点击“添加新 Peer”开始配置。</p>
                </div>
              </section>

              <div
                v-if="peerFormVisible"
                class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 px-4 py-6 backdrop-blur-sm"
                @click.self="closePeerForm"
              >
                <section class="flex max-h-[90vh] w-full max-w-3xl flex-col overflow-hidden rounded-2xl border border-ink-700 bg-ink-950 shadow-2xl">
                  <div class="flex items-start justify-between gap-4 border-b border-ink-700 px-5 py-4">
                    <div>
                      <h4 class="text-sm font-semibold text-white">{{ peerFormTitle }}</h4>
                      <p class="mt-1 text-xs text-slate-500">填写写入本端的对端 Peer 信息，生成后会同时给出可复制到对端的 Peer 配置。</p>
                    </div>
                    <button class="rounded-lg px-2 py-1 text-slate-500 transition hover:bg-ink-800 hover:text-white" aria-label="关闭 Peer 编辑" @click="closePeerForm">✕</button>
                  </div>

                  <div class="min-h-0 flex-1 space-y-4 overflow-y-auto p-5">
                    <div class="flex flex-wrap items-center justify-between gap-3">
                      <span class="chip bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">{{ form.source === 'managed' ? '已有节点' : '手动 Peer' }}</span>
                      <button class="rounded-md px-2 py-1 text-[11px] text-cyan-300 transition hover:bg-cyan-500/10" @click="pickerOpen = !pickerOpen">从已有节点填充</button>
                    </div>

                    <div v-if="pickerOpen" class="rounded-xl border border-ink-700 bg-ink-900/80 p-3">
                      <input v-model="peerSearch" class="input mb-3 w-full" placeholder="搜索节点名称 / 主机名 / IP / 公钥…" />
                      <div class="max-h-52 space-y-2 overflow-auto pr-1">
                        <button
                          v-for="candidate in managedPeerCandidates"
                          :key="candidate.id"
                          class="flex w-full items-center justify-between gap-3 rounded-xl border border-ink-700 bg-ink-950/70 px-3 py-2 text-left transition hover:border-cyan-500/40 hover:bg-cyan-500/5"
                          @click="fillFromManagedAgent(candidate)"
                        >
                          <span class="min-w-0">
                            <span class="block truncate text-sm font-medium text-slate-200">{{ candidate.name }}</span>
                            <span class="block truncate text-[11px] text-slate-500">{{ candidate.hostname }} · {{ primaryInterface(candidate)?.name || 'wg0' }} · {{ primaryInterface(candidate)?.tunnelIP || candidate.address }}</span>
                          </span>
                          <span class="shrink-0 rounded-full px-2 py-0.5 text-[10px]" :class="candidate.status === 'online' ? 'bg-emerald-500/10 text-emerald-300 ring-1 ring-emerald-500/30' : 'bg-slate-500/10 text-slate-400 ring-1 ring-slate-500/30'">{{ candidate.status === 'online' ? '在线' : '离线' }}</span>
                        </button>
                        <p v-if="!managedPeerCandidates.length" class="py-6 text-center text-xs text-slate-500">当前网络没有可选节点</p>
                      </div>
                    </div>

                    <div class="grid gap-3 sm:grid-cols-2">
                      <label class="sm:col-span-2">
                        <span class="label">PublicKey · 对端公钥 *</span>
                        <input v-model.trim="form.remotePublicKey" class="input font-mono" placeholder="base64 WireGuard public key" />
                      </label>
                      <label class="sm:col-span-2">
                        <span class="label">AllowedIPs · 对端允许 IP *</span>
                        <input v-model="form.remoteAllowedIPs" class="input font-mono" placeholder="10.88.88.2/32, fd00::2/128" />
                      </label>
                      <label>
                        <span class="label">Endpoint 地址</span>
                        <input v-model.trim="form.remoteEndpointHost" class="input font-mono" placeholder="1.2.3.4 或 example.com" />
                      </label>
                      <label>
                        <span class="label">Endpoint 端口</span>
                        <input v-model="form.remoteEndpointPort" class="input font-mono" placeholder="51820" />
                      </label>
                      <label class="sm:col-span-2">
                        <span class="label">PresharedKey · 预共享密钥</span>
                        <div class="flex gap-2">
                          <input v-model.trim="form.presharedKey" class="input min-w-0 flex-1 font-mono" placeholder="可手动填写，留空则不生成" />
                          <button class="btn-secondary shrink-0 !py-2 text-xs" type="button" @click="generatePresharedKey">随机生成</button>
                        </div>
                      </label>
                      <label>
                        <span class="label">PersistentKeepalive · 保活间隔秒</span>
                        <input v-model="form.keepalive" class="input font-mono" placeholder="25；留空或 0 不生成" />
                      </label>
                    </div>

                    <div v-if="previewReady" class="grid gap-4 lg:grid-cols-2">
                      <section class="overflow-hidden rounded-xl border border-cyan-500/30 bg-[#05070a]">
                        <div class="border-b border-ink-700 px-3 py-2 text-xs text-cyan-300">将写入本端 · {{ agent.name }}/{{ preview.localInterface }}</div>
                        <pre class="max-h-64 overflow-auto whitespace-pre-wrap p-3 font-mono text-xs leading-5 text-slate-300">{{ preview.localContent }}</pre>
                      </section>
                      <section class="overflow-hidden rounded-xl border border-violet-500/30 bg-[#05070a]">
                        <div class="flex items-center justify-between gap-2 border-b border-ink-700 px-3 py-2 text-xs text-violet-300">
                          <span>对端可复制配置</span>
                          <button class="rounded-md px-2 py-1 text-[11px] text-violet-200 transition hover:bg-violet-500/10" @click="copyRemoteConfig">{{ copiedRemoteConfig ? '已复制 ✓' : '复制' }}</button>
                        </div>
                        <pre class="max-h-64 overflow-auto whitespace-pre-wrap p-3 font-mono text-xs leading-5 text-slate-300">{{ preview.remoteContent }}</pre>
                      </section>
                    </div>
                  </div>

                  <div class="flex flex-wrap items-center justify-between gap-3 border-t border-ink-700 px-5 py-4">
                    <p class="text-xs text-slate-500">
                      <span v-if="previewReady">{{ preview.localUpdatesExisting ? '本端将更新已有 Peer' : '本端将新增 Peer' }}；对端配置已生成，可复制后粘贴。</span>
                      <span v-else>填写完成后先生成 Peer，确认配置再保存下发。</span>
                    </p>
                    <div class="flex items-center gap-2">
                      <button class="btn-secondary" @click="closePeerForm">取消</button>
                      <button class="btn-secondary" :disabled="saving" @click="generatePreview">生成 Peer</button>
                      <button class="btn-primary" :disabled="saving || !previewReady" @click="saveGenerated">{{ saving ? '保存中…' : '保存并下发' }}</button>
                    </div>
                  </div>
                </section>
              </div>
            </div>
          </template>
        </div>
    </template>

    <template #footer>
        <button class="btn-secondary" @click="emit('close')">取消</button>
        <button v-if="mode === 'manual'" class="btn-primary" :disabled="loading || saving" @click="saveManual">{{ saving ? '保存中…' : '保存并下发' }}</button>
    </template>
  </BaseModal>
</template>
