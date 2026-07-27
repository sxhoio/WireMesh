<script setup lang="ts">
import { computed, ref } from 'vue'
import WorldMap, { type MapLink } from '../components/WorldMap.vue'
import TempPeersModal from '../components/TempPeersModal.vue'
import { useMeshStore } from '../stores/mesh'
import type { Agent, PeerState } from '../types'
import { stateMeta } from '../types'
import { ago, fmtHandshake, fmtMbps, shortKey } from '../utils/format'

const mesh = useMeshStore()

const selectedAgent = ref<Agent | null>(null)
const selectedLink = ref<MapLink | null>(null)
const showTempPeers = ref(false)

const stats = computed(() => mesh.stats)

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

function linkEndLabel(ifaceId: string) {
  const f = mesh.ifaceWithAgent(ifaceId)
  return f ? `${f.agent.name}/${f.iface.name}` : ifaceId
}

const visibleLinks = computed(() => {
  let ls = mesh.scopedLinks
  if (mesh.onlyErrors) ls = ls.filter((l) => l.displayState === 'degraded' || l.displayState === 'down')
  if (mesh.linkFilter !== 'all') ls = ls.filter((l) => l.displayState === mesh.linkFilter)
  return ls
})

const unknownTempPeers = computed(() => mesh.scopedTempPeers.filter((t) => !t.geo))
</script>

<template>
  <div class="flex h-full flex-col gap-5">
    <!-- 统计卡片 -->
    <div class="grid shrink-0 grid-cols-2 gap-4 lg:grid-cols-4">
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
      <button class="panel px-4 py-3.5 text-left transition hover:border-amber-500/40" @click="showTempPeers = true">
        <p class="text-xs text-slate-500">临时对等端</p>
        <p class="mt-1 text-xl font-bold" :class="stats.tempCount ? 'text-amber-400' : 'text-white'">{{ stats.tempCount }}</p>
      </button>
    </div>

    <!-- 主体：地图占 3，右侧图例筛选 + 链路状态占 1 -->
    <div class="grid grid-cols-1 gap-5 2xl:min-h-0 2xl:flex-1 2xl:grid-cols-4">
      <!-- 地图 -->
      <div class="panel relative h-[420px] overflow-hidden sm:h-[520px] 2xl:col-span-3 2xl:h-auto 2xl:min-h-[520px]">
        <WorldMap
          :agents="mesh.scopedAgents"
          :links="mesh.scopedLinks"
          :temp-peers="mesh.scopedTempPeers"
          :link-filter="mesh.linkFilter"
          :only-errors="mesh.onlyErrors"
          @agent-click="onAgentClick"
          @link-click="onLinkClick"
        />

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
                <button class="rounded-lg p-1 text-slate-500 transition hover:bg-ink-800 hover:text-slate-200" title="关闭" @click="selectedLink = null">
                  <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
                </button>
              </div>
            </div>
            <dl class="mt-3 space-y-1.5 text-xs">
              <div class="flex justify-between gap-3 rounded-lg bg-white/[0.04] px-3 py-2"><dt class="text-slate-300">A 端</dt><dd class="truncate font-mono font-medium text-cyan-200">{{ linkEndLabel(selectedLink.a) }}</dd></div>
              <div class="flex justify-between gap-3 rounded-lg bg-white/[0.04] px-3 py-2"><dt class="text-slate-300">B 端</dt><dd class="truncate font-mono font-medium text-cyan-200">{{ linkEndLabel(selectedLink.b) }}</dd></div>
              <div class="flex justify-between gap-3 rounded-lg bg-white/[0.04] px-3 py-2"><dt class="text-slate-300">最后握手</dt><dd class="font-medium text-slate-100">{{ fmtHandshake(selectedLink.lastHandshakeSecAgo) }}</dd></div>
              <div class="flex justify-between gap-3 rounded-lg bg-white/[0.04] px-3 py-2"><dt class="text-slate-300">延迟 / 丢包</dt><dd class="font-mono font-medium text-slate-100">{{ selectedLink.latencyMs }} ms / {{ selectedLink.lossPct }}%</dd></div>
              <div class="flex justify-between gap-3 rounded-lg bg-white/[0.04] px-3 py-2">
                <dt class="text-slate-300">流量</dt>
                <dd class="font-mono text-xs text-slate-100">↓{{ fmtMbps(selectedLink.rxMbps) }} ↑{{ fmtMbps(selectedLink.txMbps) }} Mbps</dd>
              </div>
            </dl>
            <div v-if="selectedLink.failReason" class="mt-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs leading-relaxed text-red-200 ring-1 ring-red-500/30">
              故障原因：{{ selectedLink.failReason }}
            </div>
          </div>
        </transition>
      </div>

      <!-- 右侧栏：图例筛选在上方，链路状态在下方；未知位置并入链路状态 -->
      <div class="flex flex-col gap-5 2xl:min-h-0 2xl:h-full">
        <div class="panel shrink-0 p-4">
          <p class="mb-3 text-sm font-semibold text-white">图例筛选</p>
          <div class="space-y-1.5 text-[11px] text-slate-400">
            <div class="flex items-center gap-2"><span class="h-0.5 w-4 rounded bg-emerald-400"></span>正常：3 分钟内握手且探测可达</div>
            <div class="flex items-center gap-2"><span class="h-0.5 w-4 rounded bg-amber-400"></span>波动：握手超时/探测波动/单侧数据</div>
            <div class="flex items-center gap-2"><span class="h-0.5 w-4 rounded bg-red-400"></span>异常：连续探测失败</div>
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

        <div class="panel flex min-h-[360px] flex-col p-4 2xl:min-h-0 2xl:flex-1">
          <div class="mb-3 flex items-center justify-between gap-3">
            <h3 class="text-sm font-semibold text-white">链路状态</h3>
            <span class="shrink-0 text-[11px] text-slate-500">{{ visibleLinks.length }} 条链路 · {{ unknownTempPeers.length }} 未知位置</span>
          </div>
          <div class="min-h-0 max-h-[540px] flex-1 space-y-2 overflow-y-auto p-2 2xl:max-h-none">
            <button v-for="l in visibleLinks" :key="l.id" class="flex w-full items-center gap-2.5 rounded-lg bg-ink-800/60 px-3 py-2.5 text-left ring-1 ring-ink-700 transition hover:ring-ink-600" @click="onLinkClick(l)">
              <span class="h-2 w-2 shrink-0 rounded-full" :class="{ 'animate-pulse': l.displayState === 'down' }" :style="{ background: stateMeta[l.displayState].color }"></span>
              <div class="min-w-0 flex-1">
                <p class="break-all text-xs font-medium leading-snug text-slate-200">{{ linkEndLabel(l.a) }} ↔ {{ linkEndLabel(l.b) }}</p>
                <p class="mt-0.5 text-[11px] leading-snug text-slate-500">
                  <template v-if="l.displayState === 'ok'">{{ l.latencyMs }}ms · 丢包 {{ l.lossPct }}% · 握手 {{ fmtHandshake(l.lastHandshakeSecAgo) }}</template>
                  <template v-else-if="l.displayState === 'degraded'">{{ l.failReason }}</template>
                  <template v-else-if="l.displayState === 'down'">连接异常 · 丢包 {{ l.lossPct }}%</template>
                  <template v-else>状态未知</template>
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
