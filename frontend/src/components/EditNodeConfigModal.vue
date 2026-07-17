<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { Agent } from '../types'
import { useMeshStore } from '../stores/mesh'

const props = defineProps<{ agent: Agent }>()
const emit = defineEmits<{ close: [] }>()
const mesh = useMeshStore()
const saving = ref(false)
const validationError = ref('')
const form = reactive({
  name: props.agent.name,
  address: props.agent.address,
  endpoint: props.agent.publicIP,
  listenPort: props.agent.listenPort || 51820,
  mtu: props.agent.mtu || 1420,
  interfaceSelector: props.agent.interfaceSelector || 'auto',
  enabled: props.agent.enabled,
  role: (props.agent.labels.find((value) => value.startsWith('wiremesh.role='))?.split('=')[1] || 'mesh') as 'mesh' | 'hub' | 'spoke',
  manualLocation: props.agent.locationSource === 'manual',
  locationName: props.agent.locationSource === 'manual' ? props.agent.city : '',
  latitude: props.agent.locationSource === 'manual' && Number.isFinite(props.agent.lat) ? String(props.agent.lat) : '',
  longitude: props.agent.locationSource === 'manual' && Number.isFinite(props.agent.lng) ? String(props.agent.lng) : '',
})
const network = computed(() => mesh.networkById(props.agent.networkId))

async function save() {
  validationError.value = ''
  const latitude = Number(form.latitude)
  const longitude = Number(form.longitude)
  if (form.manualLocation && (form.latitude.trim() === '' || !Number.isFinite(latitude) || latitude < -90 || latitude > 90)) {
    validationError.value = '纬度必须是 -90 到 90 之间的有效数字'
    return
  }
  if (form.manualLocation && (form.longitude.trim() === '' || !Number.isFinite(longitude) || longitude < -180 || longitude > 180)) {
    validationError.value = '经度必须是 -180 到 180 之间的有效数字'
    return
  }
  saving.value = true
  const labels = Object.fromEntries(props.agent.labels.map((value) => {
    const index = value.indexOf('='); return index >= 0 ? [value.slice(0, index), value.slice(index + 1)] : [value, 'true']
  }))
  labels['wiremesh.role'] = form.role
  const ok = await mesh.updateNodeConfig(props.agent.id, {
    name: form.name.trim(), address: form.address.trim(), endpoint: form.endpoint.trim(),
    listen_port: Number(form.listenPort), mtu: Number(form.mtu), enabled: form.enabled,
    interface_selector: form.interfaceSelector.trim() || 'auto', labels,
    location_source: form.manualLocation ? 'manual' : '',
    location_name: form.manualLocation ? form.locationName.trim() : '',
    latitude: form.manualLocation ? latitude : 0,
    longitude: form.manualLocation ? longitude : 0,
  })
  saving.value = false
  if (ok) emit('close')
}
</script>

<template>
  <div class="fixed inset-0 z-[80] flex items-center justify-center bg-black/65 p-4" @click.self="emit('close')">
    <form class="panel flex max-h-[calc(100vh-2rem)] w-full max-w-3xl flex-col overflow-hidden" @submit.prevent="save">
      <div class="flex shrink-0 items-start justify-between border-b border-ink-700 px-6 py-5">
        <div><h3 class="text-base font-semibold text-white">编辑节点配置</h3><p class="mt-1 text-xs text-slate-500">WireGuard 参数发布后下发到 Agent；手动地理位置保存后立即用于地图展示。</p></div>
        <button type="button" class="text-slate-500 hover:text-white" @click="emit('close')">✕</button>
      </div>
      <div class="grid min-h-0 flex-1 gap-4 overflow-y-auto p-6 sm:grid-cols-2">
        <label class="space-y-1.5"><span class="text-xs text-slate-400">节点名称</span><input v-model="form.name" required class="input w-full" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">所属网络</span><input :value="network?.name + ' · ' + network?.cidr" disabled class="input w-full opacity-60" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">WireGuard 内网 IP</span><input v-model="form.address" required class="input w-full font-mono" placeholder="10.0.0.2" /><span class="block text-[11px] text-slate-600">必须位于 {{ network?.cidr }} 中且不能与其他节点重复</span></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">公网 Endpoint</span><input v-model="form.endpoint" class="input w-full font-mono" placeholder="host.example.com:51820" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">监听端口</span><input v-model.number="form.listenPort" type="number" min="1" max="65535" required class="input w-full" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">MTU</span><input v-model.number="form.mtu" type="number" min="576" max="9000" required class="input w-full" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">采集接口选择器</span><input v-model="form.interfaceSelector" class="input w-full font-mono" placeholder="auto 或 wg0,wg1" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">拓扑角色</span><select v-model="form.role" class="input w-full"><option value="mesh">Mesh</option><option value="hub">Hub</option><option value="spoke">Spoke</option></select></label>

        <section class="space-y-4 rounded-xl border border-cyan-500/20 bg-cyan-500/[0.04] p-4 sm:col-span-2">
          <label class="flex cursor-pointer items-center justify-between gap-4">
            <span><span class="block text-sm font-medium text-slate-200">手动设置地理位置</span><span class="mt-1 block text-[11px] leading-relaxed text-slate-500">当 Agent 或 GeoIP 无法取得有效位置时启用。保存后节点会立即出现在地图上。</span></span>
            <input v-model="form.manualLocation" type="checkbox" class="h-4 w-4 shrink-0 accent-cyan-500" />
          </label>
          <div v-if="form.manualLocation" class="grid gap-4 border-t border-ink-700/80 pt-4 sm:grid-cols-3">
            <label class="space-y-1.5 sm:col-span-3"><span class="text-xs text-slate-400">位置名称</span><input v-model="form.locationName" class="input w-full" placeholder="例如：中国 上海" /><span class="block text-[11px] text-slate-600">用于节点详情和地图位置说明，不影响坐标计算。</span></label>
            <label class="space-y-1.5"><span class="text-xs text-slate-400">纬度 Latitude</span><input v-model.trim="form.latitude" required inputmode="decimal" class="input w-full font-mono" placeholder="31.2304" /></label>
            <label class="space-y-1.5"><span class="text-xs text-slate-400">经度 Longitude</span><input v-model.trim="form.longitude" required inputmode="decimal" class="input w-full font-mono" placeholder="121.4737" /></label>
            <div class="flex items-end"><p class="pb-2 text-[11px] leading-relaxed text-slate-500">请输入 WGS84 坐标。中国大陆底图显示时会自动执行坐标转换。</p></div>
          </div>
        </section>

        <label class="flex items-center justify-between rounded-xl border border-ink-700 bg-ink-900/60 px-4 py-3 sm:col-span-2"><span><span class="block text-sm text-slate-300">启用节点</span><span class="text-xs text-slate-600">停用后不再参与后续网络配置发布</span></span><input v-model="form.enabled" type="checkbox" class="h-4 w-4 accent-emerald-500" /></label>
        <p v-if="validationError" class="rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30 sm:col-span-2">{{ validationError }}</p>
      </div>
      <div class="flex shrink-0 justify-end gap-3 border-t border-ink-700 px-6 py-4"><button type="button" class="btn-secondary" @click="emit('close')">取消</button><button class="btn-primary" :disabled="saving">{{ saving ? '保存中…' : '保存配置' }}</button></div>
    </form>
  </div>
</template>
