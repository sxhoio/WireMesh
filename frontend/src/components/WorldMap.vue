<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Map as OlMap, View, Feature } from 'ol'
import TileLayer from 'ol/layer/Tile'
import VectorLayer from 'ol/layer/Vector'
import XYZ from 'ol/source/XYZ'
import VectorSource from 'ol/source/Vector'
import { Point, LineString } from 'ol/geom'
import { Style, Circle as CircleStyle, Fill, Stroke, Text } from 'ol/style'
import { fromLonLat, toLonLat } from 'ol/proj'
import { defaults as defaultControls, ScaleLine } from 'ol/control'
import { boundingExtent } from 'ol/extent'
import countryRegionData from '../assets/country-regions.json'
import 'ol/ol.css'
import type { Agent, PeerLink, PeerState, TempPeer } from '../types'

export interface MapLink extends PeerLink {
  displayState: PeerState
}

const props = defineProps<{
  agents: Agent[]
  links: MapLink[]
  tempPeers: TempPeer[]
}>()

const locatedAgents = () => props.agents.filter((agent) => Number.isFinite(agent.lng) && Number.isFinite(agent.lat))
const locatedTempPeers = () => props.tempPeers.filter((peer) => peer.geo && Number.isFinite(peer.geo.lng) && Number.isFinite(peer.geo.lat))

type CountryRegion = { code: string; name: string; bounds: [number, number, number, number]; center: [number, number] }
const countryRegions = countryRegionData as CountryRegion[]
const selectedCountryName = ref('')

const emit = defineEmits<{
  (e: 'agent-click', agent: Agent): void
  (e: 'link-click', link: MapLink): void
  (e: 'map-blank-click'): void
}>()

const stateColor: Record<PeerState, string> = {
  ok: '#34d399',
  degraded: '#fbbf24',
  down: '#f87171',
  unknown: '#64748b',
}

const earthRadius = 6378245
const gcjEccentricity = 0.006693421622965943

function outsideChina(lng: number, lat: number) {
  return lng < 72.004 || lng > 137.8347 || lat < 0.8293 || lat > 55.8271
}

function transformLatitude(lng: number, lat: number) {
  let value = -100 + 2 * lng + 3 * lat + 0.2 * lat * lat + 0.1 * lng * lat + 0.2 * Math.sqrt(Math.abs(lng))
  value += ((20 * Math.sin(6 * lng * Math.PI) + 20 * Math.sin(2 * lng * Math.PI)) * 2) / 3
  value += ((20 * Math.sin(lat * Math.PI) + 40 * Math.sin((lat / 3) * Math.PI)) * 2) / 3
  value += ((160 * Math.sin((lat / 12) * Math.PI) + 320 * Math.sin((lat * Math.PI) / 30)) * 2) / 3
  return value
}

function transformLongitude(lng: number, lat: number) {
  let value = 300 + lng + 2 * lat + 0.1 * lng * lng + 0.1 * lng * lat + 0.1 * Math.sqrt(Math.abs(lng))
  value += ((20 * Math.sin(6 * lng * Math.PI) + 20 * Math.sin(2 * lng * Math.PI)) * 2) / 3
  value += ((20 * Math.sin(lng * Math.PI) + 40 * Math.sin((lng / 3) * Math.PI)) * 2) / 3
  value += ((150 * Math.sin((lng / 12) * Math.PI) + 300 * Math.sin((lng / 30) * Math.PI)) * 2) / 3
  return value
}

/** 中文底图在中国大陆使用 GCJ-02；其他地区保持 WGS84。 */
function mapCoordinate([lng, lat]: [number, number]): [number, number] {
  if (outsideChina(lng, lat)) return [lng, lat]
  let deltaLat = transformLatitude(lng - 105, lat - 35)
  let deltaLng = transformLongitude(lng - 105, lat - 35)
  const radLat = (lat / 180) * Math.PI
  let magic = Math.sin(radLat)
  magic = 1 - gcjEccentricity * magic * magic
  const sqrtMagic = Math.sqrt(magic)
  deltaLat = (deltaLat * 180) / (((earthRadius * (1 - gcjEccentricity)) / (magic * sqrtMagic)) * Math.PI)
  deltaLng = (deltaLng * 180) / ((earthRadius / sqrtMagic) * Math.cos(radLat) * Math.PI)
  return [lng + deltaLng, lat + deltaLat]
}

function toMapProjection(coordinate: [number, number]) {
  return fromLonLat(mapCoordinate(coordinate))
}

const el = ref<HTMLDivElement>()
let map: OlMap | null = null
let linkSource: VectorSource | null = null
let markerSource: VectorSource | null = null

/** 两点间生成贝塞尔曲线（弧线）投影坐标序列 */
function curveCoords(a: [number, number], b: [number, number]): number[][] {
  const pa = toMapProjection(a)
  const pb = toMapProjection(b)
  const dx = pb[0] - pa[0]
  const dy = pb[1] - pa[1]
  const dist = Math.hypot(dx, dy)
  if (dist < 1) return [pa, pb]
  // 垂直于连线方向的控制点偏移量，弧度随距离增大但封顶
  const offset = Math.min(dist * 0.18, 2_200_000)
  const mx = (pa[0] + pb[0]) / 2
  const my = (pa[1] + pb[1]) / 2
  // 垂直方向单位向量（取让弧线朝向上方/世界中心一侧）
  let nx = -dy / dist
  let ny = dx / dist
  if (ny < 0) {
    nx = -nx
    ny = -ny
  }
  const cx = mx + nx * offset
  const cy = my + ny * offset
  // 二次贝塞尔采样
  const pts: number[][] = []
  const N = 48
  for (let i = 0; i <= N; i++) {
    const t = i / N
    const mt = 1 - t
    pts.push([mt * mt * pa[0] + 2 * mt * t * cx + t * t * pb[0], mt * mt * pa[1] + 2 * mt * t * cy + t * t * pb[1]])
  }
  return pts
}

/** 重建全部要素：链路曲线、节点、临时 Peer（瞬间重建，无补间动画） */
function render() {
  if (!map || !linkSource || !markerSource) return
  linkSource.clear()
  markerSource.clear()

  // 不再聚合：每个节点始终单独显示，接口直接映射到所属 Agent 坐标
  const ifacePoint = new Map<string, [number, number]>()
  for (const a of locatedAgents()) {
    for (const i of a.interfaces) ifacePoint.set(i.id, [a.lng, a.lat])
  }

  const links = props.links

  const linkFeatures: Feature[] = []
  for (const l of links) {
    if (!ifacePoint.has(l.a) || !ifacePoint.has(l.b)) continue
    const geom = new LineString(curveCoords(ifacePoint.get(l.a)!, ifacePoint.get(l.b)!))
    const f = new Feature({ geometry: geom, linkRef: l })
    const color = stateColor[l.displayState]
    f.setStyle(
      new Style({
        stroke: new Stroke({
          color,
          width: l.displayState === 'down' ? 3 : 2.2,
          lineDash: l.displayState === 'unknown' ? [7, 7] : undefined,
          lineCap: 'round',
          lineJoin: 'round',
        }),
      }),
    )
    linkFeatures.push(f)
  }

  // 临时对等端 → 所属受管节点：琥珀色虚线连接
  for (const t of locatedTempPeers()) {
    if (!t.geo) continue
    const sourcePoint = ifacePoint.get(t.sourceIfaceId)
    if (!sourcePoint) continue
    const geom = new LineString(curveCoords([t.geo.lng, t.geo.lat], sourcePoint))
    const f = new Feature({ geometry: geom })
    f.setStyle(
      new Style({
        stroke: new Stroke({
          color: 'rgba(217,119,6,0.6)',
          width: 1.6,
          lineDash: [5, 6],
          lineCap: 'round',
          lineJoin: 'round',
        }),
      }),
    )
    linkFeatures.push(f)
  }
  linkSource.addFeatures(linkFeatures)

  const markerFeatures: Feature[] = []
  for (const a of locatedAgents()) {
    const online = a.status === 'online' && a.enabled
    const color = online ? '#059669' : '#94a3b8'
    const f = new Feature({ geometry: new Point(toMapProjection([a.lng, a.lat])), agentRef: a })
    f.setStyle(
      new Style({
        image: new CircleStyle({
          radius: 6,
          fill: new Fill({ color }),
          stroke: new Stroke({ color: '#ffffff', width: 2.2 }),
        }),
        text: new Text({
          text: a.name,
          offsetX: 14,
          textAlign: 'left',
          font: '600 11px sans-serif',
          fill: new Fill({ color: online ? '#1e293b' : '#94a3b8' }),
          stroke: new Stroke({ color: 'rgba(255,255,255,0.92)', width: 3.5 }),
          overflow: false,
        }),
      }),
    )
    markerFeatures.push(f)
  }

  for (const t of locatedTempPeers()) {
    if (!t.geo) continue
    const f = new Feature({ geometry: new Point(toMapProjection([t.geo.lng, t.geo.lat])) })
    f.setStyle(
      new Style({
        image: new CircleStyle({
          radius: 4.5,
          fill: new Fill({ color: 'rgba(245,158,11,0.15)' }),
          stroke: new Stroke({ color: '#d97706', width: 1.8 }),
        }),
      }),
    )
    markerFeatures.push(f)
  }
  markerSource.addFeatures(markerFeatures)
}

function countryAtCoordinate(lng: number, lat: number) {
  const candidates = countryRegions.filter((country) => {
    const [minLng, minLat, maxLng, maxLat] = country.bounds
    return lng >= minLng && lng <= maxLng && lat >= minLat && lat <= maxLat
  })
  return candidates.sort((left, right) => {
    const score = (country: CountryRegion) => {
      const [minLng, minLat, maxLng, maxLat] = country.bounds
      const width = Math.max(1, maxLng - minLng)
      const height = Math.max(1, maxLat - minLat)
      return ((lng - country.center[0]) / width) ** 2 + ((lat - country.center[1]) / height) ** 2
    }
    return score(left) - score(right)
  })[0]
}

/** 中国地理中心（经度 104°E、纬度 35°N），作为默认地图中心 */
const chinaCenter: [number, number] = [104, 35]
const chinaRegion = countryRegions.find((country) => country.code === 'CN')

function showGlobalView() {
  if (!map) return
  selectedCountryName.value = ''
  map.getView().animate({ center: toMapProjection(chinaCenter), zoom: 2, duration: 450 })
}

/** 初始视觉：聚焦中国全境（未找到中国条目时回退到 zoom 3.8 的中国中心）。 */
function showChinaView() {
  if (!map) return
  if (chinaRegion) {
    const [minLng, minLat, maxLng, maxLat] = chinaRegion.bounds
    const extent = boundingExtent([
      toMapProjection([minLng, minLat]),
      toMapProjection([minLng, maxLat]),
      toMapProjection([maxLng, minLat]),
      toMapProjection([maxLng, maxLat]),
    ])
    selectedCountryName.value = chinaRegion.name || chinaRegion.code
    map.getView().fit(extent, { padding: [42, 42, 42, 42], duration: 0, maxZoom: 6 })
  } else {
    selectedCountryName.value = ''
    map.getView().setZoom(3.8)
  }
}

function zoomToCountry(lng: number, lat: number) {
  const country = countryAtCoordinate(lng, lat)
  if (!map || !country) return false
  const [minLng, minLat, maxLng, maxLat] = country.bounds
  const extent = boundingExtent([
    toMapProjection([minLng, minLat]),
    toMapProjection([minLng, maxLat]),
    toMapProjection([maxLng, minLat]),
    toMapProjection([maxLng, maxLat]),
  ])
  selectedCountryName.value = country.name || country.code
  map.getView().fit(extent, { padding: [54, 54, 54, 54], duration: 500, maxZoom: 6 })
  return true
}

onMounted(() => {
  linkSource = new VectorSource()
  markerSource = new VectorSource()
  map = new OlMap({
    target: el.value!,
    controls: defaultControls({ zoom: false, rotate: false, attribution: false }).extend([new ScaleLine({ units: 'metric' })]),
    layers: [
      new TileLayer({
        source: new XYZ({
          // 使用中文标注底图，并保持节点坐标与底图坐标系一致。
          url: 'https://webrd0{1-4}.is.autonavi.com/appmaptile?style=7&x={x}&y={y}&z={z}&lang=zh_cn&size=1&scale=1',
          attributions: '© 高德地图',
          maxZoom: 19,
        }),
      }),
      new VectorLayer({ source: linkSource, zIndex: 2 }),
      new VectorLayer({ source: markerSource, zIndex: 3 }),
    ],
    view: new View({
      center: toMapProjection(chinaCenter),
      zoom: 2,
      minZoom: 2,
      maxZoom: 12,
    }),
  })
  // 默认以中国为初始视觉：挂载后立即聚焦中国全境。
  showChinaView()

  // 矢量要素是地理定位的，平移/缩放时由 OpenLayers 自动跟随地图，无需重建。
  // 只在数据变化（下方 watch）或首次挂载时 render；在 change:resolution /
  // moveend 里 clear+重建会导致移动过程中要素短暂消失。

  map.on('singleclick', (evt) => {
    let hit = false
    map!.forEachFeatureAtPixel(evt.pixel, (f) => {
      const agent = f.get('agentRef') as Agent | undefined
      const link = f.get('linkRef') as MapLink | undefined
      if (agent) {
        emit('agent-click', agent)
      } else if (link) {
        emit('link-click', link)
      }
      hit = true
      return true
    }, { hitTolerance: 6 })
    if (!hit) {
      emit('map-blank-click')
      const [lng, lat] = toLonLat(evt.coordinate)
      zoomToCountry(lng, lat)
    }
    return hit
  })

  render()
})

onBeforeUnmount(() => {
  map?.setTarget(undefined)
  map = null
})

// store 采用 immutable 重新赋值（整数组替换），浅比较引用即可感知数据变化；
// deep watch 会对大数组的每个嵌套字段做递归依赖追踪，开销显著且无必要。
watch(
  () => [props.agents, props.links, props.tempPeers],
  () => render(),
)
</script>

<template>
  <div class="relative h-full w-full">
    <div ref="el" class="ol-map-wrap h-full w-full"></div>
    <div class="absolute left-4 top-4 z-10 flex items-center gap-2 rounded-lg bg-white/90 px-3 py-2 text-xs text-slate-600 shadow-lg ring-1 ring-slate-200 backdrop-blur">
      <span>{{ selectedCountryName ? '已定位：' + selectedCountryName : '全球视图 · 点击国家自动放大' }}</span>
      <button v-if="selectedCountryName" class="font-medium text-cyan-700 hover:text-cyan-900" @click="showGlobalView">返回全球</button>
    </div>
    <div v-if="!locatedAgents().length" class="pointer-events-none absolute inset-0 flex items-center justify-center bg-white/45 p-6 text-center backdrop-blur-[1px]">
      <div class="rounded-xl bg-white/90 px-5 py-4 text-sm text-slate-500 shadow-lg ring-1 ring-slate-200">
        暂无带有效地理坐标的节点；正在等待客户端上报公网 IP 或服务器 GeoIP 自动解析。
      </div>
    </div>
  </div>
</template>

<style scoped>
.ol-map-wrap {
  background: #e8eef4;
}
.ol-map-wrap :deep(.ol-viewport) {
  background: #e8eef4;
}
</style>
