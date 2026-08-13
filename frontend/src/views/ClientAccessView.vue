<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import QRCode from 'qrcode'
import { api, type ApiClientConfig } from '../api'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import { useClipboard } from '../composables/useClipboard'
import { requestConfirm } from '../utils/confirm'

const app = useAppStore()
const mesh = useMeshStore()

const selectedNetworkId = ref('')
const selectedNodeId = ref('')
const config = ref<ApiClientConfig | null>(null)
const loading = ref(false)
const error = ref('')
const qrDataUrl = ref('')
const creating = ref(false)
const newClientName = ref('')
const { copied, copyText } = useClipboard(false, 1400)

const nodesOfNetwork = computed(() => mesh.agents.filter((agent) => agent.networkId === selectedNetworkId.value))

watch(selectedNetworkId, () => {
  selectedNodeId.value = ''
  config.value = null
  qrDataUrl.value = ''
  const first = nodesOfNetwork.value[0]
  if (first) selectedNodeId.value = first.id
})

async function loadConfig() {
  if (!selectedNodeId.value) return
  loading.value = true
  error.value = ''
  try {
    const result = await api.nodeClientConfig(selectedNodeId.value)
    config.value = result
    qrDataUrl.value = await QRCode.toDataURL(result.content, { width: 480, margin: 1, errorCorrectionLevel: 'M' })
  } catch (reason) {
    config.value = null
    qrDataUrl.value = ''
    error.value = reason instanceof Error ? reason.message : '配置导出失败'
  } finally {
    loading.value = false
  }
}

watch(selectedNodeId, loadConfig)

function downloadConfig() {
  if (!config.value) return
  const blob = new Blob([config.value.content], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = (config.value.name || 'wiremesh-client') + '.conf'
  anchor.click()
  URL.revokeObjectURL(url)
}

function downloadQR() {
  if (!qrDataUrl.value || !config.value) return
  const anchor = document.createElement('a')
  anchor.href = qrDataUrl.value
  anchor.download = (config.value.name || 'wiremesh-client') + '-qr.png'
  anchor.click()
}

async function copyConfig() {
  if (!config.value) return
  await copyText(config.value.content, true)
}

async function createClient() {
  const name = newClientName.value.trim()
  if (!name || !selectedNetworkId.value || creating.value) return
  creating.value = true
  error.value = ''
  try {
    const node = await api.createNode({ network_id: selectedNetworkId.value, name, labels: { 'wiremesh.client': 'true' } })
    mesh.selectedNetworkId = selectedNetworkId.value
    try {
      const result = await api.publish(selectedNetworkId.value)
      await mesh.waitForDeliveryResult(result)
      mesh.notice = '客户端已创建并发布配置'
    } catch {
      mesh.notice = '客户端已创建，发布配置失败，请在设置中手动发布'
    }
    await mesh.refreshNodeTelemetry(true)
    newClientName.value = ''
    selectedNodeId.value = node.id
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
  if (await mesh.removeAgent(id, app.username)) {
    selectedNodeId.value = ''
    config.value = null
    qrDataUrl.value = ''
  }
}
</script>

<template>
  <div class="flex h-full flex-col gap-5">
    <div class="flex flex-wrap items-end gap-3">
      <div>
        <label class="label">网络</label>
        <select v-model="selectedNetworkId" class="input !w-64">
          <option value="">选择网络</option>
          <option v-for="n in mesh.networks" :key="n.id" :value="n.id">{{ n.name }}（{{ n.cidr }}）</option>
        </select>
      </div>
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

    <div class="grid min-h-0 flex-1 grid-cols-1 gap-5 lg:grid-cols-[16rem_1fr]">
      <!-- 设备列表 -->
      <div class="panel min-h-0 overflow-y-auto p-3">
        <p class="mb-2 px-2 text-xs font-semibold text-slate-400">设备（{{ nodesOfNetwork.length }}）</p>
        <p v-if="!selectedNetworkId" class="px-2 py-6 text-center text-xs text-slate-600">请先选择网络</p>
        <p v-else-if="!nodesOfNetwork.length" class="px-2 py-6 text-center text-xs text-slate-600">该网络暂无节点；先创建客户端或接入 Agent 节点</p>
        <button
          v-for="a in nodesOfNetwork"
          :key="a.id"
          class="flex w-full items-center gap-2.5 rounded-lg px-3 py-2.5 text-left transition ring-1"
          :class="selectedNodeId === a.id ? 'bg-emerald-500/10 ring-emerald-500/40' : 'ring-transparent hover:bg-ink-800'"
          @click="selectedNodeId = a.id"
        >
          <span class="h-2 w-2 shrink-0 rounded-full" :class="a.status === 'online' ? 'bg-emerald-400' : a.labels.includes('wiremesh.client=true') ? 'bg-cyan-400' : 'bg-slate-600'"></span>
          <span class="min-w-0 flex-1 truncate text-sm text-slate-200">{{ a.name }}</span>
          <span v-if="a.labels.includes('wiremesh.client=true')" class="chip shrink-0 bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">客户端</span>
        </button>
      </div>

      <!-- 配置 + QR -->
      <div class="panel flex min-h-0 flex-col p-5">
        <p v-if="error" class="mb-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ error }}</p>
        <div v-if="!selectedNodeId" class="flex flex-1 items-center justify-center text-xs text-slate-500">选择左侧设备查看接入配置</div>
        <div v-else-if="loading" class="flex flex-1 items-center justify-center text-xs text-slate-500">正在生成配置…</div>
        <div v-else-if="config" class="grid min-h-0 flex-1 gap-5 lg:grid-cols-[minmax(0,1fr)_18rem]">
          <div class="flex min-h-0 flex-col">
            <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
              <p class="text-sm font-semibold text-white">{{ config.name }} · 隧道地址 {{ config.address }}</p>
              <div class="flex flex-wrap gap-2">
                <button class="btn-ghost !py-1.5 text-xs" @click="copyConfig">{{ copied ? '已复制 ✓' : '复制配置' }}</button>
                <button class="btn-ghost !py-1.5 text-xs" @click="downloadConfig">下载 .conf</button>
                <button v-if="app.isAdmin" class="btn-ghost !py-1.5 text-xs text-red-300" @click="removeClient">删除设备</button>
              </div>
            </div>
            <pre class="min-h-0 flex-1 overflow-auto rounded-xl bg-ink-950 p-4 font-mono text-xs leading-5 text-emerald-200/90 ring-1 ring-ink-600">{{ config.content }}</pre>
          </div>
          <div class="flex flex-col items-center gap-3">
            <p class="text-xs text-slate-500">使用 WireGuard 客户端扫码导入</p>
            <img v-if="qrDataUrl" :src="qrDataUrl" alt="WireGuard 配置二维码" class="w-64 max-w-full rounded-xl ring-1 ring-ink-600" />
            <button v-if="qrDataUrl" class="btn-ghost !py-1.5 text-xs" @click="downloadQR">下载二维码图片</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
