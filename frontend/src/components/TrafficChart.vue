<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, type ApiTrafficPoint, type ApiTrafficRange } from '../api'
import { fmtMbps } from '../utils/format'

const props = defineProps<{ nodeId: string; interfaceName: string; range: ApiTrafficRange }>()
const points = ref<ApiTrafficPoint[]>([])
const loading = ref(false)
const error = ref('')
let requestID = 0

async function load() {
  const current = ++requestID
  loading.value = true
  error.value = ''
  try {
    const result = await api.traffic(props.nodeId, props.interfaceName, props.range)
    if (current === requestID) points.value = result
  } catch (reason) {
    if (current === requestID) {
      points.value = []
      error.value = reason instanceof Error ? reason.message : '流量数据加载失败'
    }
  } finally {
    if (current === requestID) loading.value = false
  }
}
watch(() => [props.nodeId, props.interfaceName, props.range], load, { immediate: true })

const latest = computed(() => points.value.at(-1))
const maxRate = computed(() => Math.max(0.01, ...points.value.flatMap((point) => [point.rx_mbps, point.tx_mbps])))
function polyline(field: 'rx_mbps' | 'tx_mbps') {
  if (points.value.length < 2) return ''
  return points.value.map((point, index) => {
    const x = points.value.length === 1 ? 0 : index / (points.value.length - 1) * 100
    const y = 38 - Math.min(38, Math.max(0, point[field]) / maxRate.value * 34)
    return `${x.toFixed(2)},${y.toFixed(2)}`
  }).join(' ')
}
</script>

<template>
  <div class="rounded-xl bg-ink-950/40 p-3 ring-1 ring-ink-700">
    <div v-if="loading" class="flex h-32 items-center justify-center text-xs text-slate-500">正在加载真实流量数据…</div>
    <div v-else-if="error" class="flex h-32 items-center justify-center text-center text-xs text-red-400">{{ error }}</div>
    <div v-else-if="points.length < 2" class="flex h-32 items-center justify-center text-center">
      <div><p class="text-xs text-slate-400">暂无足够的历史流量数据</p><p class="mt-1 text-[11px] text-slate-600">至少需要 Agent 完成两次心跳上报后才能计算真实速率。</p></div>
    </div>
    <div v-else>
      <div class="mb-2 flex flex-wrap items-center justify-between gap-2 text-[11px]">
        <span class="text-slate-500">{{ interfaceName }} · {{ points.length }} 个采样点</span>
        <span class="flex gap-3 font-mono"><span class="text-cyan-300">下载 {{ fmtMbps(latest?.rx_mbps ?? 0) }} Mbps</span><span class="text-violet-300">上传 {{ fmtMbps(latest?.tx_mbps ?? 0) }} Mbps</span></span>
      </div>
      <svg viewBox="0 0 100 40" preserveAspectRatio="none" class="h-32 w-full overflow-visible" role="img" aria-label="真实流量时序曲线">
        <line v-for="y in [4, 12.5, 21, 29.5, 38]" :key="y" x1="0" :y1="y" x2="100" :y2="y" stroke="currentColor" stroke-width="0.2" class="text-slate-700" />
        <polyline :points="polyline('rx_mbps')" fill="none" stroke="#22d3ee" stroke-width="0.8" vector-effect="non-scaling-stroke" />
        <polyline :points="polyline('tx_mbps')" fill="none" stroke="#a78bfa" stroke-width="0.8" vector-effect="non-scaling-stroke" />
      </svg>
      <div class="mt-1 flex justify-between text-[10px] text-slate-600"><span>{{ new Date(points[0].recorded_at).toLocaleString('zh-CN') }}</span><span>峰值 {{ fmtMbps(maxRate) }} Mbps</span><span>{{ new Date(points.at(-1)!.recorded_at).toLocaleString('zh-CN') }}</span></div>
    </div>
  </div>
</template>
