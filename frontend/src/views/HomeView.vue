<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import WorldMap, { type MapLink } from '../components/WorldMap.vue'
import TempPeersModal from '../components/TempPeersModal.vue'
import { useMeshStore } from '../stores/mesh'
import type { Agent, PeerState } from '../types'
import { stateMeta } from '../types'
import { ago, fmtHandshake, fmtMbps, shortKey } from '../utils/format'

const router = useRouter()
const mesh = useMeshStore()

const selectedAgent = ref<Agent | null>(null)
const selectedLink = ref<MapLink | null>(null)
const showTempPeers = ref(false)
const refreshing = ref(false)

const stats = computed(() => mesh.stats)
const lastUpdatedText = computed(() => (mesh.lastUpdated ? ago(mesh.lastUpdated) : '—'))
const emptySetup = computed(() => !mesh.projects.length && !mesh.networks.length && !mesh.agents.length)

const filterOptions: { value: 'all' | PeerState; label: string }[] = [
  { value: 'all', label: '全部状态' },
  { value: 'ok', label: '正常（绿）' },
  { value: 'degraded', label: '波动（黄）' },
  { value: 'down', label: '异常（红）' },
  { value: 'unknown', label: '未知（灰）' },
]

function agentEndpoint(a: Agent) {
  return a.publicIP || '—'
}

function locationSourceLabel(source: string) {
  if (source === 'manual') return '手动位置'
  if (source === 'agent') return '客户端自动定位'
  if (source === 'geoip') return 'GeoIP 自动定位'
  return '等待自动定位'
}

function onAgentClick(a: Agent) {
  selectedLink.value = null
  selectedAgent.value = a
}

function onLinkClick(l: MapLink) {
  selectedAgent.value = null
  selectedLink.value = l
}

function onMapBlankClick() {
  selectedAgent.value = null
  selectedLink.value = null
}

function linkEndLabel(ifaceId: string) {
  const f = mesh.ifaceWithAgent(ifaceId)
  return f ? `${f.agent.name}/${f.iface.name}` : ifaceId
}

function linkStateHint(l: MapLink) {
  switch (l.displayState) {
    case 'ok': return '握手正常'
    case 'degraded': return l.failReason || '握手波动或单侧数据'
    case 'down': return '握手超时，链路异常'
    default: return '状态未知'
  }
}

async function manualRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await mesh.refresh()
  } finally {
    refreshing.value = false
  }
}

const visibleLinks = computed(() => mesh.scopedLinksFiltered)

const unknownTempPeers = computed(() => mesh.scopedTempPeers.filter((t) => !t.geo))
</script>

<template>
  <div class="flex h-full flex-col gap-5">
    <!-- 统计卡片 -->
    <div class="grid shrink-0 grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-5">
      <div class="panel px-4 py-3.5">
        <p class="text-xs text-slate-500">节点总数</p>
        <p class="mt-1 whitespace-nowrap text-xl font-bold text-white">{{ stats.agentTotal }}
          <span class="ml-1.5 text-xs font-normal"><span class="text-emerald-400">{{ stats.agentOnline }} 在线</span> · <span class="text-slate-500">{{ stats.agentTotal - stats.agentOnline }} 离线</span></span>
        </p>
      </div>
      <div class="panel px-4 py-3.5">
        <p class="text-xs text-slate-500">WireGuard 接口</p>
        <p class="mt-1 text-xl font-bold text-cyan-300">{{ stats.ifaceCount }}</p>
      </div>
      <div class="panel px-4 py-3.5">
        <p class="text-xs text-slate-500">对等连接</p>
        <p class="mt-1 whitespace-nowrap text-xl font-bold text-white">{{ stats.linkOk + stats.linkBad + stats.linkUnknown }}
          <span class="ml-1.5 text-xs font-normal"><span class="text-emerald-400">{{ stats.linkOk }}</span> / <span class="text-red-400">{{ stats.linkBad }}</span> / <span class="text-slate-500">{{ stats.linkUnknown }}</span></span>
        </p>
      </div>
      <div class="panel px-4 py-3.5">
        <p class="text-xs text-slate-500">实时速率（全网）</p>
        <p class="mt-1 whitespace-nowrap font-mono text-xl font-bold text-white"><span class="text-cyan-300">↓{{ fmtMbps(stats.rx) }}</span> <span class="text-violet-300">↑{{ fmtMbps(stats.tx) }}</span></p>
        <p class="mt-0.5 text-[11px] text-slate-600">Mbps · 按两次心跳差值计算</p>
      </div>
      <button class="panel px-4 py-3.5 text-left transition hover:border-amber-500/40" @click="showTempPeers = true">
        <p class="text-xs text-slate-500">临时对等端</p>
        <p class="mt-1 text-xl font-bold" :class="stats.tempCount ? 'text-amber-400' : 'text-white'">{{ stats.tempCount }}</p>
      </button>
    </div>

    <!-- 告警摘要 -->
    <div v-if="stats.linkDown" class="flex shrink-0 items-center gap-3 rounded-xl bg-red-500/10 px-4 py-2.5 ring-1 ring-red-500/30">
      <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4 shrink-0 text-red-400" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" /></svg>
      <p class="text-xs text-red-300">{{ stats.linkDown }} 条链路异常<span v-if="stats.agentTotal - stats.agentOnline"> · {{ stats.agentTotal - stats.agentOnline }} 个节点离线</span></p>
      <button class="ml-auto shrink-0 rounded-lg px-3 py-1 text-xs font-medium text-red-200 ring-1 ring-red-500/40 transition hover:bg-red-500/10" @click="router.push({ name: 'alerts' })">前往告警中心</button>
    </div>

    <!-- 空状态引导 -->
    <div v-else-if="emptySetup" class="panel shrink-0 p-6 text-center">
      <p class="text-sm text-slate-300">欢迎使用 WireMesh，当前还没有任何节点。</p>
      <p class="mt-1 text-xs text-slate-500">先在「系统设置 → 项目与网络」创建网络，再到「客户端接入」或「节点列表 → 接入节点」部署节点。</p>
      <div class="mt-4 flex justify-center gap-2.5">
        <button class="btn-primary" @click="router.push({ name: 'clients' })">接入客户端设备</button>
        <button class="btn-secondary" @click="router.push({ name: 'nodes' })">接入节点</button>
      </div>
    </div>

    <!-- 主体：地图占 3，右侧图例筛选 + 链路状态占 1 -->
    <div class="grid grid-cols-1 gap-5 lg:min-h-0 lg:flex-1 lg:grid-cols-4">
      <!-- 地图 -->
      <div class="panel relative h-[420px] overflow-hidden sm:h-[520px] lg:col-span-3 lg:h-auto lg:min-h-[520px]">
        <WorldMap
          :agents="mesh.scopedAgents"
          :links="visibleLinks"
          :temp-peers="mesh.scopedTempPeers"
          @agent-click="onAgentClick"
          @link-click="onLinkClick"
          @map-blank-click="onMapBlankClick"
        />

        <!-- 数据更新指示 -->
        <div class="pointer-events-none absolute bottom-2 left-4 z-10 flex items-center gap-2 text-[11px] text-slate-500">
          <span class="rounded-md bg-white/85 px-2 py-1 shadow-sm ring-1 ring-slate-200 backdrop-blur">最后更新 {{ lastUpdatedText }}<span v-if="mesh.autoRefresh"> · 每 30s 自动刷新</span></span>
        </div>
        <button
          class="absolute right-4 top-14 z-10 flex items-center gap-1.5 rounded-lg bg-white/90 px-3 py-1.5 text-xs text-slate-600 shadow-lg ring-1 ring-slate-200 backdrop-blur transition hover:text-slate-900"
          :disabled="refreshing"
          @click="manualRefresh"
        >
          <svg viewBox="0 0 24 24" fill="none" class="h-3.5 w-3.5" :class="{ 'animate-spin': refreshing }" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" /></svg>
          {{ refreshing ? '刷新中…' : '刷新数据' }}
        </button>

        <!-- Agent 详情浮层 -->
        <transition name="fade">
          <div v-if="selectedAgent" class="absolute right-4 top-4 z-10 w-80 rounded-xl border border-slate-700/80 bg-slate-950/95 p-4 text-slate-100 shadow-2xl shadow-black/40 ring-1 ring-ink-600 backdrop-blur">
            <div class="flex items-start justify-between">
              <div>
                <p class="font-semibold text-white">{{ selectedAgent.name }}</p>
                <p class="text-xs text-slate-500">{{ selectedAgent.city || '未知位置' }}<span v-if="selectedAgent.locationSource === 'manual'" class="ml-1 text-cyan-500">· 手动位置</span> · {{ selectedAgent.hostname }}</p>
              </div>
              <button class="text-slate-500 hover:text-slate-300" @click="selectedAgent = null">
                <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
              </button>
            </div>
            <dl class="mt-3 space-y-1.5 text-xs">
              <div class="flex justify-between"><dt class="text-slate-500">状态</dt><dd :class="selectedAgent.status === 'online' ? 'text-emerald-400' : 'text-slate-500'">{{ selectedAgent.status === 'online' ? '在线' : '离线' }}{{ selectedAgent.enabled ? '' : ' · 已停用' }}</dd></div>
              <div class="flex justify-between gap-4"><dt class="text-slate-500">定位方式</dt><dd :class="selectedAgent.locationSource === 'manual' ? 'text-cyan-300' : selectedAgent.locationSource ? 'text-emerald-300' : 'text-amber-300'">{{ locationSourceLabel(selectedAgent.locationSource) }}</dd></div>
              <div class="flex justify-between"><dt class="text-slate-500">公网端点</dt><dd class="font-mono text-slate-300">{{ agentEndpoint(selectedAgent) }}</dd></div>
              <div v-if="Number.isFinite(selectedAgent.lat) && Number.isFinite(selectedAgent.lng)" class="flex justify-between"><dt class="text-slate-500">地理坐标</dt><dd class="font-mono text-slate-300">{{ selectedAgent.lat.toFixed(4) }}, {{ selectedAgent.lng.toFixed(4) }}</dd></div>
              <div class="flex justify-between"><dt class="text-slate-500">流量</dt><dd class="text-slate-300">↓{{ fmtMbps(selectedAgent.rxMbps) }} ↑{{ fmtMbps(selectedAgent.txMbps) }} Mbps</dd></div>
              <div class="flex justify-between"><dt class="text-slate-500">最后上报</dt><dd class="text-slate-300">{{ ago(selectedAgent.lastSeen) }}</dd></div>
              <div class="flex justify-between"><dt class="text-slate-500">版本</dt><dd class="text-slate-300">{{ selectedAgent.version }}</dd></div>
            </dl>
            <div class="mt-3 border-t border-ink-700 pt-3">
              <p class="mb-2 text-[11px] font-medium text-slate-500">{{ selectedAgent.interfaces.length }} 个接口</p>
              <div class="space-y-2">
                <div v-for="i in selectedAgent.interfaces" :key="i.id" class="rounded-lg bg-ink-800/70 px-3 py-2 ring-1 ring-ink-600">
                  <div class="flex items-center justify-between">
                    <span class="font-mono text-xs font-semibold text-cyan-300">{{ i.name }}</span>
                    <span class="chip bg-violet-500/10 text-violet-300 ring-1 ring-violet-500/30">{{ mesh.networkById(i.networkId)?.name }}</span>
                  </div>
                  <p class="mt-1 font-mono text-[11px] text-slate-400">{{ i.tunnelIP }} · :{{ i.listenPort }} · MTU {{ i.mtu }}</p>
                  <p class="font-mono text-[11px] text-slate-600">{{ shortKey(i.publicKey) }}</p>
                </div>
              </div>
            </div>
          </div>
        </transition>

        <!-- 链路详情浮层 -->
        <transition name="fade">
          <div
            v-if="selectedLink"
            class="absolute right-4 top-4 z-10 w-80 rounded-xl border border-slate-700/80 bg-slate-950/95 p-4 text-slate-100 shadow-2xl shadow-black/40 backdrop-blur"
          >
            <div class="flex items-start justify-between">
              <p class="font-semibold text-white">链路详情</p>
              <div class="flex items-center gap-2">
                <span class="chip" :class="selectedLink.displayState === 'ok' ? 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/30' : selectedLink.displayState === 'degraded' ? 'bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/30' : 'bg-red-500/10 text-red-400 ring-1 ring-red-500/30'">
                  {{ stateMeta[selectedLink.displayState].label }}
                </span>
                <button class="rounded-lg p-1 text-slate-500 transition hover:bg-ink-800 hover:text-slate-200" title="关闭" aria-label="关闭链路详情" @click="selectedLink = null">
                  <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
                </button>
              </div>
            </div>
            <dl class="mt-3 space-y-1.5 text-xs">
              <div class="flex justify-between gap-3 rounded-lg bg-white/[0.04] px-3 py-2"><dt class="text-slate-300">A 端</dt><dd class="truncate font-mono font-medium text-cyan-200">{{ linkEndLabel(selectedLink.a) }}</dd></div>
              <div class="flex justify-between gap-3 rounded-lg bg-white/[0.04] px-3 py-2"><dt class="text-slate-300">B 端</dt><dd class="truncate font-mono font-medium text-cyan-200">{{ linkEndLabel(selectedLink.b) }}</dd></div>
              <div class="flex justify-between gap-3 rounded-lg bg-white/[0.04] px-3 py-2"><dt class="text-slate-300">最后握手</dt><dd class="font-medium text-slate-100">{{ fmtHandshake(selectedLink.lastHandshakeSecAgo) }}</dd></div>
              <div class="flex justify-between gap-3 rounded-lg bg-white/[0.04] px-3 py-2"><dt class="text-slate-300">链路状态</dt><dd class="font-medium text-slate-100">{{ linkStateHint(selectedLink) }}</dd></div>
            </dl>
            <div v-if="selectedLink.displayState === 'down'" class="mt-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs leading-relaxed text-red-200 ring-1 ring-red-500/30">
              链路异常：{{ selectedLink.failReason || '握手超时，请检查两端节点的连通性' }}
            </div>
          </div>
        </transition>
      </div>

      <!-- 右侧栏：图例筛选在上方，链路状态在下方；未知位置并入链路状态 -->
      <div class="flex flex-col gap-5 lg:min-h-0 lg:h-full">
        <div class="panel shrink-0 p-4">
          <p class="mb-3 text-sm font-semibold text-white">图例筛选</p>
          <div class="space-y-1.5 text-[11px] text-slate-400">
            <div class="flex items-center gap-2"><span class="h-0.5 w-4 rounded bg-emerald-400"></span>正常：最近握手在阈值内</div>
            <div class="flex items-center gap-2"><span class="h-0.5 w-4 rounded bg-amber-400"></span>波动：握手超过阈值一半或单侧数据</div>
            <div class="flex items-center gap-2"><span class="h-0.5 w-4 rounded bg-red-400"></span>异常：握手超过阈值</div>
            <div class="flex items-center gap-2"><span class="h-0 w-4 border-t border-dashed border-slate-500"></span>未知：节点离线/从未握手</div>
            <div class="flex items-center gap-2 pt-1"><span class="h-2 w-2 rounded-full bg-emerald-400"></span>受管节点（实心）</div>
            <div class="flex items-center gap-2"><span class="h-2 w-2 rounded-full border border-amber-400"></span>临时对等端（空心）</div>
          </div>
          <div class="mt-3 space-y-2.5 border-t border-ink-700 pt-3">
            <select v-model="mesh.linkFilter" class="input !py-1.5 !text-xs">
              <option v-for="o in filterOptions" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
            <label class="flex items-center justify-between text-[11px] text-slate-400">
              仅看异常
              <button class="relative h-5 w-9 rounded-full transition" :class="mesh.onlyErrors ? 'bg-red-500' : 'bg-ink-600'" @click="mesh.onlyErrors = !mesh.onlyErrors">
                <span class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all" :class="mesh.onlyErrors ? 'left-[18px]' : 'left-0.5'"></span>
              </button>
            </label>
          </div>
        </div>

        <div class="panel flex min-h-[360px] flex-col p-4 lg:min-h-0 lg:flex-1">
          <div class="mb-3 flex items-center justify-between gap-3">
            <h3 class="text-sm font-semibold text-white">链路状态</h3>
            <span class="shrink-0 text-[11px] text-slate-500">{{ visibleLinks.length }} 条链路 · {{ unknownTempPeers.length }} 未知位置</span>
          </div>
          <div class="min-h-0 max-h-[540px] flex-1 space-y-2 overflow-y-auto p-2 lg:max-h-none">
            <button v-for="l in visibleLinks" :key="l.id" class="flex w-full items-center gap-2.5 rounded-lg bg-ink-800/60 px-3 py-2.5 text-left ring-1 ring-ink-700 transition hover:ring-ink-600" @click="onLinkClick(l)">
              <span class="h-2 w-2 shrink-0 rounded-full" :class="{ 'animate-pulse': l.displayState === 'down' }" :style="{ background: stateMeta[l.displayState].color }"></span>
              <div class="min-w-0 flex-1">
                <p class="break-all text-xs font-medium leading-snug text-slate-200">{{ linkEndLabel(l.a) }} ↔ {{ linkEndLabel(l.b) }}</p>
                <p class="mt-0.5 text-[11px] leading-snug text-slate-500">
                  {{ linkStateHint(l) }} · 握手 {{ fmtHandshake(l.lastHandshakeSecAgo) }}
                </p>
              </div>
              <span class="chip shrink-0 self-start" :class="l.displayState === 'ok' ? 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/30' : l.displayState === 'degraded' ? 'bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/30' : l.displayState === 'down' ? 'bg-red-500/10 text-red-400 ring-1 ring-red-500/30' : 'bg-slate-500/10 text-slate-500 ring-1 ring-slate-500/30'">
                {{ stateMeta[l.displayState].label }}
              </span>
            </button>
            <p v-if="!visibleLinks.length" class="py-6 text-center text-xs text-slate-600">当前筛选条件下没有链路</p>

            <div class="mt-3 border-t border-ink-700 pt-3">
              <p class="flex items-center justify-between text-xs font-semibold text-slate-200">
                未知位置
                <span class="chip bg-slate-500/10 text-slate-400 ring-1 ring-slate-500/30">{{ unknownTempPeers.length }}</span>
              </p>
              <p class="mt-1 text-[11px] leading-relaxed text-slate-500">私网 IP、无公网端点或 GeoIP 解析失败的对等端；有公网端点的已按 IP 标注在地图上</p>
              <div class="mt-3 space-y-2">
                <div v-for="t in unknownTempPeers" :key="t.id" class="flex items-center gap-2 rounded-lg bg-ink-800/60 px-3 py-2.5 ring-1 ring-ink-700">
                  <span class="h-2 w-2 shrink-0 rounded-full border border-amber-400/70"></span>
                  <span class="min-w-0 flex-1 truncate font-mono text-xs text-slate-400">{{ t.endpoint || shortKey(t.publicKey) }}</span>
                  <span class="shrink-0 text-[11px] text-slate-600">{{ fmtHandshake(t.lastHandshakeSecAgo) }}</span>
                </div>
                <p v-if="!unknownTempPeers.length" class="py-4 text-center text-xs text-slate-600">没有未知位置的对等端</p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <TempPeersModal v-if="showTempPeers" @close="showTempPeers = false" />
  </div>
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}
</style>
