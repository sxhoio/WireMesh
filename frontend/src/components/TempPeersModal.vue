<script setup lang="ts">
import { useMeshStore } from '../stores/mesh'
import { fmtBytes, fmtHandshake, shortEndpoint, shortKey } from '../utils/format'

const emit = defineEmits<{ (e: 'close'): void }>()
const mesh = useMeshStore()

function srcLabel(sourceIfaceId: string) {
  const found = mesh.ifaceWithAgent(sourceIfaceId)
  return found ? `${found.agent.name}/${found.iface.name}` : sourceIfaceId
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-ink-950/70 p-4 backdrop-blur-sm" @click.self="emit('close')">
    <div class="panel flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden">
      <div class="flex items-center justify-between border-b border-ink-700 px-6 py-4">
        <div>
          <h2 class="text-base font-semibold text-white">临时对等端</h2>
          <p class="mt-0.5 text-xs text-slate-500">从节点心跳上报的 WireGuard 观测数据中实时推导出的临时对等端（尚未注册为受管节点的设备）</p>
        </div>
        <button class="rounded-lg p-1.5 text-slate-500 hover:bg-ink-800 hover:text-slate-300" @click="emit('close')">
          <svg viewBox="0 0 24 24" fill="none" class="h-5 w-5" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>
      <!-- 各列宽度约束：超出省略（IPv6 端点压缩中间），hover 显示完整值 -->
      <div class="min-h-0 flex-1 overflow-x-auto p-6">
        <table class="w-full min-w-[640px] table-fixed text-left text-sm">
          <thead><tr class="border-b border-ink-700 text-xs text-slate-500">
            <th class="w-36 py-2.5 pr-3 font-medium">公钥</th>
            <th class="w-40 py-2.5 pr-3 font-medium">公网端点</th>
            <th class="w-32 py-2.5 pr-3 font-medium">位置</th>
            <th class="w-44 py-2.5 pr-3 font-medium">允许的地址范围</th>
            <th class="w-32 py-2.5 pr-3 font-medium">来源接口</th>
            <th class="w-32 py-2.5 pr-3 font-medium">流量</th>
            <th class="py-2.5 font-medium">握手</th>
          </tr></thead>
          <tbody>
            <tr v-for="peer in mesh.scopedTempPeers" :key="peer.id" class="border-b border-ink-800 last:border-0">
              <td class="truncate py-3 pr-3 font-mono text-xs text-slate-400" :title="peer.publicKey">{{ shortKey(peer.publicKey) }}</td>
              <td class="truncate py-3 pr-3 font-mono text-xs text-slate-300" :title="peer.endpoint">{{ shortEndpoint(peer.endpoint) }}</td>
              <td class="truncate py-3 pr-3 text-xs text-slate-400" :title="peer.geo ? `${peer.geo.city}, ${peer.geo.country}` : ''">{{ peer.geo ? `${peer.geo.city}, ${peer.geo.country}` : '未知' }}</td>
              <td class="break-all py-3 pr-3 font-mono text-xs leading-5 text-slate-400" :title="peer.allowedIPs">{{ peer.allowedIPs || '—' }}</td>
              <td class="truncate py-3 pr-3 text-xs text-slate-400" :title="srcLabel(peer.sourceIfaceId)">{{ srcLabel(peer.sourceIfaceId) }}</td>
              <td class="truncate py-3 pr-3 font-mono text-xs text-slate-400" title="自接口启用以来累计流量">↓{{ fmtBytes(peer.rxMB * 1024 ** 2) }} ↑{{ fmtBytes(peer.txMB * 1024 ** 2) }}</td>
              <td class="truncate py-3 text-xs text-slate-400">{{ fmtHandshake(peer.lastHandshakeSecAgo) }}</td>
            </tr>
            <tr v-if="!mesh.scopedTempPeers.length"><td colspan="7" class="py-12 text-center"><p class="text-sm text-slate-400">没有临时对等端数据</p><p class="mt-1 text-xs text-slate-600">当有非受管设备连接任一 WireGuard 接口时，将自动出现在这里。</p></td></tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>
