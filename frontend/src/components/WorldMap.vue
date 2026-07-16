<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import * as echarts from 'echarts'
import worldJson from '../assets/map/world.json'
import type { Agent, PeerLink, PeerState, TempPeer } from '../types'

export interface MapLink extends PeerLink {
  displayState: PeerState
}

const props = defineProps<{
  agents: Agent[]
  links: MapLink[]
  tempPeers: TempPeer[]
  linkFilter: 'all' | PeerState
  onlyErrors: boolean
}>()

const emit = defineEmits<{
  (e: 'agent-click', agent: Agent): void
  (e: 'link-click', link: MapLink): void
}>()

const el = ref<HTMLDivElement>()
let chart: echarts.ECharts | null = null
let resizeObserver: ResizeObserver | null = null

/** 视口状态：任何数据刷新、筛选切换都不重置用户当前的缩放与位置 */
let zoomLevel = 1.15
let centerPos: [number, number] = [20, 25]
/** 用户是否手动调整过视口；自动适配只在未手动调整时生效 */
let userRoamed = false
/** 上次自动适配时的节点集合指纹，节点范围不变则不重复适配 */
let lastFitKey = ''

echarts.registerMap('world', worldJson as any)

interface Cluster {
  agents: Agent[]
  lng: number
  lat: number
}

/** 按地理距离聚合（阈值随缩放级别减小） */
function clusterAgents(list: Agent[]): { singles: Agent[]; clusters: Cluster[] } {
  const threshold = Math.max(2.5, 9 / zoomLevel)
  const used = new Set<string>()
  const clusters: Cluster[] = []
  const singles: Agent[] = []
  for (const a of list) {
    if (used.has(a.id)) continue
    const group = [a]
    used.add(a.id)
    for (const b of list) {
      if (used.has(b.id)) continue
      if (Math.abs(a.lng - b.lng) < threshold && Math.abs(a.lat - b.lat) < threshold) {
        group.push(b)
        used.add(b.id)
      }
    }
    if (group.length > 1) {
      clusters.push({
        agents: group,
        lng: group.reduce((s, x) => s + x.lng, 0) / group.length,
        lat: group.reduce((s, x) => s + x.lat, 0) / group.length,
      })
    } else {
      singles.push(a)
    }
  }
  return { singles, clusters }
}

function locatedAgents() {
  return props.agents.filter((agent) => Number.isFinite(agent.lng) && Number.isFinite(agent.lat))
}

const stateColor: Record<PeerState, string> = {
  ok: '#34d399',
  degraded: '#fbbf24',
  down: '#f87171',
  unknown: '#64748b',
}

/** 只构建 series 数据；geo 配置只在初始化时设置一次，避免刷新重置视口 */
function buildSeries(): echarts.SeriesOption[] {
  const agents = locatedAgents()
  const { singles, clusters } = clusterAgents(agents)
  const clusteredIds = new Set(clusters.flatMap((c) => c.agents.map((a) => a.id)))

  const ifacePoint = new Map<string, [number, number]>()
  for (const a of agents) {
    for (const i of a.interfaces) {
      if (clusteredIds.has(a.id)) {
        const c = clusters.find((x) => x.agents.some((g) => g.id === a.id))!
        ifacePoint.set(i.id, [c.lng, c.lat])
      } else {
        ifacePoint.set(i.id, [a.lng, a.lat])
      }
    }
  }

  let links = props.links
  if (props.onlyErrors) links = links.filter((l) => l.displayState === 'degraded' || l.displayState === 'down')
  if (props.linkFilter !== 'all') links = links.filter((l) => l.displayState === props.linkFilter)

  const lineData = links
    .filter((l) => ifacePoint.has(l.a) && ifacePoint.has(l.b))
    .map((l) => {
      const color = stateColor[l.displayState]
      return {
        linkId: l.id,
        coords: [ifacePoint.get(l.a)!, ifacePoint.get(l.b)!],
        lineStyle: {
          color,
          curveness: 0.18,
          width: l.displayState === 'down' ? 2.6 : 1.8,
          opacity: l.displayState === 'unknown' ? 0.55 : 0.9,
          type: l.displayState === 'unknown' ? ('dashed' as const) : ('solid' as const),
          shadowColor: color,
          shadowBlur: l.displayState === 'ok' ? 4 : 8,
        },
      }
    })

  const onlineSingles = singles.filter((a) => a.status === 'online' && a.enabled)
  const offlineSingles = singles.filter((a) => a.status === 'offline' || !a.enabled)

  // 标签重叠时自动隐藏，低缩放级别下只显示节点名
  const labelBase = {
    position: 'right' as const,
    fontSize: 10,
    textBorderColor: '#070b14',
    textBorderWidth: 2,
  }
  const noOverlap = { hideOverlap: true, moveOverlap: 'shiftY' as const }

  return [
    {
      type: 'lines',
      coordinateSystem: 'geo',
      zlevel: 2,
      data: lineData,
      // 关闭动画：数据变化时线段直接重建，不做位置补间移动
      animation: false,
      animationDuration: 0,
      animationDurationUpdate: 0,
      animationEasingUpdate: 'linear',
    } as any,
    {
      type: 'effectScatter',
      coordinateSystem: 'geo',
      zlevel: 3,
      rippleEffect: { brushType: 'stroke', scale: 3 },
      symbolSize: 10,
      itemStyle: { color: '#34d399', shadowColor: 'rgba(52,211,153,0.8)', shadowBlur: 8 },
      label: { ...labelBase, show: true, formatter: (p: any) => p.data.name, color: '#cbd5e1' },
      labelLayout: noOverlap,
      data: onlineSingles.map((a) => ({ ...a, value: [a.lng, a.lat], kind: 'agent' })),
    } as any,
    {
      type: 'scatter',
      coordinateSystem: 'geo',
      zlevel: 3,
      symbolSize: 10,
      itemStyle: { color: '#475569', borderColor: '#94a3b8', borderWidth: 1 },
      label: { ...labelBase, show: true, formatter: (p: any) => p.data.name, color: '#64748b' },
      labelLayout: noOverlap,
      data: offlineSingles.map((a) => ({ ...a, value: [a.lng, a.lat], kind: 'agent' })),
    } as any,
    {
      type: 'scatter',
      coordinateSystem: 'geo',
      zlevel: 4,
      symbolSize: (_v: number[], p: any) => 18 + Math.min(10, p.data.agents.length * 2),
      itemStyle: {
        color: 'rgba(52,211,153,0.18)',
        borderColor: '#34d399',
        borderWidth: 1.5,
        shadowColor: 'rgba(52,211,153,0.5)',
        shadowBlur: 10,
      },
      label: { show: true, formatter: (p: any) => String(p.data.agents.length), color: '#a7f3d0', fontSize: 12, fontWeight: 'bold' },
      data: clusters.map((c) => ({ value: [c.lng, c.lat], agents: c.agents, kind: 'cluster', name: `${c.agents.length} 个节点` })),
    } as any,
    {
      type: 'scatter',
      coordinateSystem: 'geo',
      zlevel: 3,
      symbolSize: 8,
      itemStyle: { color: 'transparent', borderColor: '#fbbf24', borderWidth: 1.6 },
      label: { ...labelBase, show: true, formatter: () => '临时 Peer', color: '#d97706', fontSize: 9 },
      labelLayout: noOverlap,
      data: props.tempPeers.filter((t) => t.geo && Number.isFinite(t.geo.lng) && Number.isFinite(t.geo.lat)).map((t) => ({ value: [t.geo!.lng, t.geo!.lat], kind: 'temp', name: 'temp' })),
    } as any,
  ]
}

/** 根据可见节点计算最佳视野；zoom 基于容器宽高比换算，确保铺满 */
function fitView(agents: Agent[]) {
  if (!agents.length) return
  const lngs = agents.map((a) => a.lng)
  const lats = agents.map((a) => a.lat)
  const minLng = Math.min(...lngs)
  const maxLng = Math.max(...lngs)
  const minLat = Math.min(...lats)
  const maxLat = Math.max(...lats)
  centerPos = [(minLng + maxLng) / 2, (minLat + maxLat) / 2]
  const spanLng = maxLng - minLng || 1
  const spanLat = maxLat - minLat || 1

  // 世界地图经度跨度约 360、纬度跨度约 170；容器宽高比决定哪个维度先顶满
  const rect = el.value?.getBoundingClientRect()
  const aspect = rect ? rect.width / Math.max(1, rect.height) : 16 / 9
  const worldAspect = 360 / 170
  // 留 12% 边距
  const margin = 0.88
  let z: number
  if (aspect >= worldAspect) {
    // 容器更宽：纬度先顶满
    z = (170 / Math.max(spanLat, 1)) * margin
  } else {
    // 容器更高：经度先顶满
    z = (360 / Math.max(spanLng, 1)) * margin
  }
  zoomLevel = +Math.min(8, Math.max(1.0, z)).toFixed(2)
}

function applyViewport() {
  if (!chart) return
  chart.setOption({ geo: { center: centerPos, zoom: zoomLevel } })
}

/** 数据/筛选变化时只更新 series，geo 视口保持不变 */
function render(fit = false) {
  if (!chart) return
  if (fit && !userRoamed) {
    const agents = locatedAgents()
    const key = agents.map((a) => a.id).sort().join(',')
    if (key !== lastFitKey) {
      lastFitKey = key
      fitView(agents)
      applyViewport()
    }
  }
  chart.setOption({ series: buildSeries() })
}

onMounted(() => {
  chart = echarts.init(el.value!)
  // 首次按节点范围自动适配
  const agents = locatedAgents()
  if (agents.length) {
    fitView(agents)
    lastFitKey = agents.map((a) => a.id).sort().join(',')
  }
  chart.setOption({
    backgroundColor: 'transparent',
    geo: {
      map: 'world',
      roam: true,
      zoom: zoomLevel,
      center: centerPos,
      scaleLimit: { min: 0.8, max: 12 },
      itemStyle: { areaColor: '#111c30', borderColor: '#2a3c5e', borderWidth: 0.6 },
      emphasis: { disabled: true },
      select: { disabled: true },
      silent: true,
    },
    series: buildSeries(),
  } as echarts.EChartsOption)

  chart.on('georoam', () => {
    userRoamed = true
    const opt = chart?.getOption() as any
    const z = opt?.geo?.[0]?.zoom
    const c = opt?.geo?.[0]?.center
    if (c) centerPos = c as [number, number]
    if (z && Math.abs(z - zoomLevel) > 0.35) {
      zoomLevel = z
      // 仅因聚合阈值变化重绘 series，视口不动
      render()
    }
  })
  chart.on('click', (params: any) => {
    if (params.seriesType === 'lines') {
      const l = props.links.find((x) => x.id === params.data.linkId)
      if (l) emit('link-click', l)
      return
    }
    const kind = params.data?.kind
    if (kind === 'agent') {
      emit('agent-click', params.data as Agent)
    } else if (kind === 'cluster') {
      // 点击聚合簇：平滑放大到簇中心
      zoomLevel = Math.min(6, zoomLevel * 2)
      centerPos = params.data.value as [number, number]
      chart?.setOption({ geo: { center: centerPos, zoom: zoomLevel } })
      render()
    }
  })
  resizeObserver = new ResizeObserver(() => chart?.resize())
  resizeObserver.observe(el.value!)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  chart?.dispose()
  chart = null
})

watch(
  () => [props.agents, props.links, props.tempPeers, props.linkFilter, props.onlyErrors],
  () => render(true),
  { deep: true },
)
</script>

<template>
  <div ref="el" class="h-full w-full"></div>
</template>
