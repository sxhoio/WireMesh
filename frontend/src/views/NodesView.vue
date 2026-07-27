<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import AddAgentDialog from '../components/AddAgentDialog.vue'
import EditNodeConfigModal from '../components/EditNodeConfigModal.vue'
import AgentLogsModal from '../components/AgentLogsModal.vue'
import PeerConfigEditorModal from '../components/PeerConfigEditorModal.vue'
import TrafficChart from '../components/TrafficChart.vue'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import type { Agent, WGInterface } from '../types'
import { stateMeta } from '../types'
import { ago, fmtHandshake, fmtMbps, shortKey } from '../utils/format'

const app = useAppStore()
const mesh = useMeshStore()

const showAdd = ref(false)
const keyword = ref('')
const statusFilter = ref<'all' | 'online' | 'offline' | 'disabled'>('all')
const networkFilter = ref('all')
const sortBy = ref<'name' | 'lastSeen' | 'rx'>('name')
const expanded = ref<Set<string>>(new Set())
const trafficRange = reactive<Record<string, '24h' | '7d' | '30d'>>({})

const menuFor = ref<string | null>(null)
const menuPos = ref<{ x: number; y: number }>({ x: 0, y: 0 })

function openMenu(e: MouseEvent, id: string) {
  if (menuFor.value === id) {
    menuFor.value = null
    return
  }
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
  menuPos.value = { x: rect.right, y: rect.bottom + 4 }
  menuFor.value = id
}

function closeMenu() {
  menuFor.value = null
}
const copiedKey = ref<string | null>(null)
const editingAgent = ref<Agent | null>(null)
const peerEditingAgent = ref<Agent | null>(null)
const logsAgent = ref<Agent | null>(null)
const refreshing = ref(false)

async function refreshAll() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await mesh.collectAll()
  } finally {
    refreshing.value = false
  }
}

const filtered = computed(() => {
  let list = mesh.scopedAgents.filter((a) => {
    if (statusFilter.value === 'online' && a.status !== 'online') return false
    if (statusFilter.value === 'offline' && a.status !== 'offline') return false
    if (statusFilter.value === 'disabled' && a.enabled) return false
    if (networkFilter.value !== 'all' && a.networkId !== networkFilter.value && !a.interfaces.some((i) => i.networkId === networkFilter.value)) return false
    const kw = keyword.value.trim().toLowerCase()
    if (kw && !`${a.name} ${a.hostname} ${a.city} ${a.publicIP}`.toLowerCase().includes(kw)) return false
    return true
  })
  list = [...list].sort((a, b) => {
    if (sortBy.value === 'name') return a.name.localeCompare(b.name)
    if (sortBy.value === 'lastSeen') return b.lastSeen - a.lastSeen
    return b.rxMbps + b.txMbps - (a.rxMbps + a.txMbps)
  })
  return list
})

function toggleExpand(id: string) {
  if (expanded.value.has(id)) expanded.value.delete(id)
  else {
    expanded.value.add(id)
    if (!trafficRange[id]) trafficRange[id] = '24h'
  }
  expanded.value = new Set(expanded.value)
}

function peersOf(iface?: WGInterface) {
  if (!iface) return []
  return mesh.scopedLinks
    .filter((l) => l.a === iface.id || l.b === iface.id)
    .map((l) => {
      const otherId = l.a === iface.id ? l.b : l.a
      const other = mesh.ifaceWithAgent(otherId)
      return { link: l, other, otherId }
    })
}

function peerErrorCount(a: Agent) {
  // 只统计真实异常（down）的链路；波动（degraded）不标记为异常。
  return mesh.links.filter(
    (l) => (a.interfaces.some((i) => i.id === l.a || i.id === l.b)) && l.state === 'down',
  ).length
}


async function copyText(text: string, key: string) {
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    /* ignore */
  }
  copiedKey.value = key
  setTimeout(() => (copiedKey.value = null), 1400)
}

async function confirmDelete(a: Agent) {
  if (!window.confirm(`确定删除节点“${a.name}”吗？相关 Peer、命令和配置下发记录将一并清理。`)) return
  await mesh.removeAgent(a.id, app.username)
}

</script>

<template>
  <div class="space-y-5">
    <!-- 工具栏 -->
    <div class="flex flex-wrap items-center gap-3">
      <div class="relative min-w-0 flex-1 sm:max-w-64">
        <svg viewBox="0 0 24 24" fill="none" class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-5.197-5.197m0 0A7.5 7.5 0 105.196 5.196a7.5 7.5 0 0010.607 10.607z" /></svg>
        <input v-model="keyword" class="input w-full pl-9" placeholder="搜索名称 / 主机名 / IP…" />
      </div>
      <select v-model="statusFilter" class="input !w-28 shrink-0">
        <option value="all">全部状态</option>
        <option value="online">在线</option>
        <option value="offline">离线</option>
        <option value="disabled">已停用</option>
      </select>
      <select v-model="networkFilter" class="input !w-36 shrink-0">
        <option value="all">全部网络</option>
        <option v-for="n in mesh.networks" :key="n.id" :value="n.id">{{ n.name }}</option>
      </select>
      <select v-model="sortBy" class="input !w-32 shrink-0">
        <option value="name">按名称</option>
        <option value="lastSeen">按最近上报</option>
        <option value="rx">按流量</option>
      </select>
      <div class="ml-auto flex items-center gap-2">
        <button v-if="app.canOperate" class="btn-secondary flex items-center gap-1.5" :disabled="refreshing" title="立即请求在线节点采集并上报最新状态" @click="refreshAll">
          <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4" :class="{ 'animate-spin': refreshing }" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" /></svg>
          {{ refreshing ? '刷新中…' : '刷新' }}
        </button>
        <button v-if="app.isAdmin" class="btn-primary" @click="showAdd = true">
          <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 4.5v15m7.5-7.5h-15" /></svg>
          接入节点
        </button>
      </div>
    </div>

    <!-- Agent 列表：table-fixed 固定列宽，所有行严格对齐 -->
    <div class="panel overflow-hidden">
      <table class="w-full table-fixed border-collapse text-left">
        <thead>
          <tr class="text-[11px] font-medium text-slate-500">
            <th class="w-10 px-2 py-3"></th>
            <th class="w-7 px-1 py-3"></th>
            <th class="w-36 px-2 py-3">节点</th>
            <th class="hidden w-24 px-2 py-3 md:table-cell">接口</th>
            <th class="hidden w-28 px-2 py-3 md:table-cell">隧道 IP</th>
            <th class="hidden w-28 px-2 py-3 2xl:table-cell">公网 Endpoint</th>
            <th class="hidden w-24 px-2 py-3 2xl:table-cell">速率 (Mbps)</th>
            <th class="hidden w-20 px-2 py-3 2xl:table-cell">Peer</th>
            <th class="hidden w-56 px-2 py-3 xl:table-cell">版本 · 上报</th>
            <th class="w-28 px-2 py-3 text-left">操作</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="a in filtered" :key="a.id">
            <tr class="border-t border-ink-700/70 transition hover:bg-ink-850/40">
              <td class="px-2 py-3.5 text-center">
                <button class="text-slate-500 transition hover:text-slate-300" @click="toggleExpand(a.id)">
                  <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4 transition-transform" :class="{ 'rotate-90': expanded.has(a.id) }" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8.25 4.5l7.5 7.5-7.5 7.5" /></svg>
                </button>
              </td>
              <td class="px-1 py-3.5">
                <span class="block h-2.5 w-2.5 rounded-full" :class="!a.enabled ? 'bg-slate-600' : a.status === 'online' ? 'bg-emerald-400 shadow-glow' : 'bg-slate-500'"></span>
              </td>
              <td class="px-2 py-3.5">
                <p class="truncate font-medium leading-snug text-white">{{ a.name }}</p>
                <p class="truncate text-xs text-slate-500">{{ a.hostname }}</p>
                <p v-if="a.collectionError" class="mt-0.5 truncate text-[11px] text-amber-400" :title="a.collectionError">WireGuard 采集异常</p>
                <p class="mt-0.5 truncate font-mono text-[11px] text-slate-600 md:hidden">{{ a.interfaces.map((i) => i.name).join(', ') || '待配置' }} · {{ a.interfaces.map((i) => i.tunnelIP).join(', ') || a.address }}</p>
              </td>
              <td class="hidden px-2 py-3.5 md:table-cell">
                <p class="flex items-center gap-1 whitespace-nowrap text-sm text-slate-300">
                  <span class="truncate font-mono text-cyan-300">{{ a.interfaces.map((i) => i.name).join(', ') || '待配置' }}</span>
                  <span v-if="a.interfaces.length > 1" class="chip shrink-0 bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">{{ a.interfaces.length }}</span>
                </p>
              </td>
              <td class="hidden px-2 py-3.5 md:table-cell">
                <p class="truncate font-mono text-xs text-slate-300">{{ a.interfaces.map((i) => i.tunnelIP).join(', ') || a.address }}</p>
              </td>
              <td class="hidden px-2 py-3.5 2xl:table-cell">
                <p class="truncate font-mono text-xs text-slate-300">{{ a.publicIP }}</p>
              </td>
              <td class="hidden whitespace-nowrap px-2 py-3.5 font-mono text-xs text-slate-300 2xl:table-cell">↓{{ fmtMbps(a.rxMbps) }} ↑{{ fmtMbps(a.txMbps) }}</td>
              <td class="hidden whitespace-nowrap px-2 py-3.5 text-xs text-slate-300 2xl:table-cell">
                {{ peersOf(a.interfaces[0]).length + a.interfaces.slice(1).reduce((n, i) => n + peersOf(i).length, 0) }}
                <span v-if="peerErrorCount(a)" class="ml-1 text-red-400">({{ peerErrorCount(a) }} 异常)</span>
              </td>
              <td class="hidden px-2 py-3.5 xl:table-cell">
                <p class="truncate text-xs leading-relaxed text-slate-500" :title="`${a.version} · ${a.osInfo}`">{{ a.version }} · {{ a.osInfo }}</p>
                <p class="truncate text-xs text-slate-500">最后上报 {{ ago(a.lastSeen) }}<span v-if="!a.enabled" class="ml-1.5 text-amber-400">已停用</span></p>
              </td>
              <td class="px-2 py-3.5">
                <div class="flex items-center justify-end gap-1.5">
                  <button
                    v-if="app.canOperate"
                    class="chip w-11 justify-center transition"
                    :class="a.enabled ? 'bg-slate-500/10 text-slate-400 ring-1 ring-slate-500/30 hover:bg-amber-500/10 hover:text-amber-400' : 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/30'"
                    @click="mesh.toggleAgentEnabled(a.id, app.username)"
                  >
                    {{ a.enabled ? '停用' : '启用' }}
                  </button>
                  <button class="rounded-lg p-2 text-slate-500 transition hover:bg-ink-800 hover:text-slate-200" @click="openMenu($event, a.id)">
                    <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M12 6.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 12.75a.75.75 0 110-1.5.75.75 0 010 1.5zM12 18.75a.75.75 0 110-1.5.75.75 0 010 1.5z" /></svg>
                  </button>
                </div>
              </td>
            </tr>

            <!-- 展开：接口详情 -->
            <tr v-if="expanded.has(a.id)" class="border-t border-ink-700/70 bg-ink-950/40">
              <td colspan="10" class="px-4 py-4 sm:px-5">
          <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <div v-if="!a.interfaces.length" class="rounded-xl bg-ink-900/70 p-4 ring-1 xl:col-span-2" :class="a.collectionError ? 'ring-amber-500/30' : 'ring-ink-600'">
              <p class="text-sm font-medium" :class="a.collectionError ? 'text-amber-300' : 'text-slate-300'">未采集到 WireGuard 接口</p>
              <p class="mt-1 text-xs leading-relaxed text-slate-500">
                {{ a.collectionError || 'Agent 心跳正常，但当前机器没有活动的 WireGuard 接口。请发布该节点的网络配置，或检查 --interfaces 选择器。' }}
              </p>
              <p v-if="a.interfaceSelector" class="mt-2 font-mono text-[11px] text-slate-600">接口选择器：{{ a.interfaceSelector }}</p>
            </div>
            <div v-for="iface in a.interfaces" :key="iface.id" class="rounded-xl bg-ink-900/70 p-4 ring-1 ring-ink-600">
              <div class="flex items-center justify-between">
                <div class="flex items-center gap-2">
                  <span class="font-mono text-sm font-semibold text-cyan-300">{{ iface.name }}</span>
                  <span class="chip bg-violet-500/10 text-violet-300 ring-1 ring-violet-500/30">{{ mesh.networkById(iface.networkId)?.name }}</span>
                  <span v-if="iface.role !== 'mesh'" class="chip bg-amber-500/10 text-amber-300 ring-1 ring-amber-500/30">{{ iface.role === 'hub' ? 'Hub' : 'Spoke' }}</span>
                </div>
                <button v-if="app.canOperate" class="chip bg-slate-500/10 text-slate-400 ring-1 ring-slate-500/30 transition hover:bg-ink-700" @click="editingAgent = a">编辑</button>
              </div>
              <div class="mt-2.5 grid grid-cols-2 gap-x-4 gap-y-1.5 text-xs">
                <p class="text-slate-500">监听端口 <span class="ml-1 font-mono text-slate-300">{{ iface.listenPort }}</span></p>
                <p class="text-slate-500">MTU <span class="ml-1 font-mono text-slate-300">{{ iface.mtu }}</span></p>
                <p class="text-slate-500">隧道地址 <span class="ml-1 font-mono text-slate-300">{{ iface.tunnelIP }}/32</span></p>
                <p class="truncate text-slate-500">公钥 <span class="ml-1 font-mono text-slate-400" :title="iface.publicKey">{{ shortKey(iface.publicKey) }}</span></p>
              </div>

              <!-- Peer 表 -->
              <div class="mt-3 border-t border-ink-700 pt-3">
                <p class="mb-2 text-[11px] font-medium text-slate-500">Peer（{{ peersOf(iface).length }}）</p>
                <div class="space-y-1.5">
                  <div v-for="p in peersOf(iface)" :key="p.link.id" class="flex items-center gap-2.5 text-xs">
                    <span class="h-1.5 w-1.5 shrink-0 rounded-full" :style="{ background: stateMeta[p.link.displayState].color }"></span>
                    <span class="min-w-0 flex-1 truncate text-slate-300">{{ p.other ? p.other.agent.name + '/' + p.other.iface.name : p.otherId }}</span>
                    <span class="font-mono text-[11px] text-slate-500">{{ p.other?.iface.tunnelIP }}</span>
                    <span class="w-20 text-right text-[11px] text-slate-500">{{ fmtHandshake(p.link.lastHandshakeSecAgo) }}</span>
                    <span class="w-24 text-right font-mono text-[11px] text-slate-500">↓{{ fmtMbps(p.link.rxMbps) }} ↑{{ fmtMbps(p.link.txMbps) }}</span>
                  </div>
                  <p v-if="!peersOf(iface).length" class="text-[11px] text-slate-600">暂无 Peer</p>
                </div>
              </div>

              <!-- 流量曲线 -->
              <div class="mt-3 border-t border-ink-700 pt-3">
                <div class="mb-1 flex items-center justify-between">
                  <p class="text-[11px] font-medium text-slate-500">流量曲线</p>
                  <div class="flex gap-1">
                    <button
                      v-for="r in ['24h', '7d', '30d'] as const"
                      :key="r"
                      class="rounded-md px-2 py-0.5 text-[10px] font-medium transition"
                      :class="(trafficRange[a.id] ?? '24h') === r ? 'bg-emerald-500/15 text-emerald-300' : 'text-slate-500 hover:text-slate-300'"
                      @click="trafficRange[a.id] = r"
                    >
                      {{ r === '24h' ? '24 小时' : r === '7d' ? '7 天' : '月度' }}
                    </button>
                  </div>
                </div>
                <TrafficChart :node-id="a.id" :interface-name="iface.name" :range="trafficRange[a.id] ?? '24h'" />
              </div>
            </div>
          </div>
              </td>
            </tr>
          </template>
          <tr v-if="!filtered.length">
            <td colspan="10" class="py-12 text-center text-sm text-slate-500">没有匹配的 Agent</td>
          </tr>
        </tbody>
      </table>
    </div>

    <AddAgentDialog v-if="showAdd" @close="showAdd = false" />
    <EditNodeConfigModal v-if="editingAgent" :agent="editingAgent" @close="editingAgent = null" />
    <PeerConfigEditorModal v-if="peerEditingAgent" :agent="peerEditingAgent" @close="peerEditingAgent = null" />
    <AgentLogsModal v-if="logsAgent" :agent="logsAgent" @close="logsAgent = null" />

    <!-- 更多操作菜单：Teleport 到 body，避免被行容器裁剪 -->
    <Teleport to="body">
      <div v-if="menuFor" class="fixed inset-0 z-50" @click="closeMenu" @contextmenu.prevent="closeMenu"></div>
      <div
        v-if="menuFor"
        class="fixed z-[60] w-52 rounded-xl border border-ink-600 bg-ink-850 py-1.5 shadow-2xl"
        :style="{ left: menuPos.x - 208 + 'px', top: menuPos.y + 'px' }"
      >
        <template v-for="a in filtered" :key="a.id">
          <template v-if="menuFor === a.id">
            <button v-if="app.canOperate" class="flex w-full items-center gap-2.5 px-4 py-2 text-left text-xs text-slate-300 hover:bg-ink-700" @click="peerEditingAgent = a; closeMenu()">编辑 Peer</button>
            <button v-if="app.canOperate" class="flex w-full items-center gap-2.5 px-4 py-2 text-left text-xs text-slate-300 hover:bg-ink-700" @click="mesh.collectNow(a.id); closeMenu()">立即采集状态</button>
            <button v-if="app.canOperate" class="flex w-full items-center gap-2.5 px-4 py-2 text-left text-xs text-slate-300 hover:bg-ink-700" @click="mesh.checkConnectivity(a.id); closeMenu()">连通性检测</button>
            <button v-if="app.canOperate" class="flex w-full items-center gap-2.5 px-4 py-2 text-left text-xs text-slate-300 hover:bg-ink-700" @click="editingAgent = a; closeMenu()">编辑接口设置</button>
            <button class="flex w-full items-center gap-2.5 px-4 py-2 text-left text-xs text-slate-300 hover:bg-ink-700" @click="copyText(a.interfaces.map((i) => i.tunnelIP).filter(Boolean).join(', ') || a.address, 'ip-' + a.id)">{{ copiedKey === 'ip-' + a.id ? '已复制 ✓' : '复制隧道 IP' }}</button>
            <button class="flex w-full items-center gap-2.5 px-4 py-2 text-left text-xs text-slate-300 hover:bg-ink-700" @click="copyText(a.publicIP, 'ep-' + a.id)">{{ copiedKey === 'ep-' + a.id ? '已复制 ✓' : '复制 Endpoint' }}</button>
            <button class="flex w-full items-center gap-2.5 px-4 py-2 text-left text-xs text-slate-300 hover:bg-ink-700" @click="copyText(a.interfaces[0]?.publicKey || a.publicKey, 'pk-' + a.id)">{{ copiedKey === 'pk-' + a.id ? '已复制 ✓' : '复制 Public Key' }}</button>
            <button class="flex w-full items-center gap-2.5 px-4 py-2 text-left text-xs text-slate-300 hover:bg-ink-700" @click="logsAgent = a; closeMenu()">查看日志</button>
            <div class="my-1 border-t border-ink-700"></div>
            <button v-if="app.isAdmin" class="flex w-full items-center gap-2.5 px-4 py-2 text-left text-xs text-red-400 hover:bg-ink-700" @click="confirmDelete(a); closeMenu()">删除 Agent</button>
          </template>
        </template>
      </div>
    </Teleport>
  </div>
</template>
