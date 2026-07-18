<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { Agent } from '../types'
import { useMeshStore } from '../stores/mesh'

const props = defineProps<{ agent: Agent }>()
const emit = defineEmits<{ close: [] }>()
const mesh = useMeshStore()
const reportedInterface = props.agent.interfaces.find((iface) => iface.tunnelIP && iface.tunnelIP === props.agent.address)
  || (props.agent.interfaceSelector && props.agent.interfaceSelector !== 'auto'
    ? props.agent.interfaces.find((iface) => props.agent.interfaceSelector.split(',').map((value) => value.trim()).includes(iface.name))
    : undefined)
  || props.agent.interfaces.find((iface) => iface.up && iface.tunnelIP)
  || props.agent.interfaces.find((iface) => iface.tunnelIP)
  || props.agent.interfaces.find((iface) => iface.up)
  || props.agent.interfaces[0]
const saving = ref(false)
const validationError = ref('')
const form = reactive({
  name: props.agent.name,
  address: reportedInterface?.tunnelIP || props.agent.address,
  endpoint: props.agent.publicIP,
  listenPort: reportedInterface?.listenPort || props.agent.listenPort || 51820,
  mtu: reportedInterface?.mtu || props.agent.mtu || 1420,
  interfaceSelector: props.agent.interfaceSelector || 'auto',
  enabled: props.agent.enabled,
  role: (props.agent.labels.find((value) => value.startsWith('wiremesh.role='))?.split('=')[1] || 'mesh') as 'mesh' | 'hub' | 'spoke',
  manualLocation: props.agent.locationSource === 'manual',
  locationName: props.agent.city || '',
})
const network = computed(() => mesh.networkById(props.agent.networkId))
const hasCurrentLocation = computed(() => Number.isFinite(props.agent.lat) && Number.isFinite(props.agent.lng))
const currentLocationSource = computed(() => {
  if (props.agent.locationSource === 'manual') return '手动位置'
  if (props.agent.locationSource === 'agent') return '客户端自动定位'
  if (props.agent.locationSource === 'geoip') return 'GeoIP 自动定位'
  return '等待自动定位'
})

async function save() {
  validationError.value = ''
  if (form.manualLocation && form.locationName.trim() === '') {
    validationError.value = '请输入位置名称'
    return
  }
  saving.value = true
  const labels = Object.fromEntries(props.agent.labels.map((value) => {
    const index = value.indexOf('='); return index >= 0 ? [value.slice(0, index), value.slice(index + 1)] : [value, 'true']
  }))
  labels['wiremesh.role'] = form.role
  const ok = await mesh.updateNodeAndPublish(props.agent.id, {
    name: form.name.trim(), address: form.address.trim(), endpoint: form.endpoint.trim(),
    listen_port: Number(form.listenPort), mtu: Number(form.mtu), enabled: form.enabled,
    interface_selector: form.interfaceSelector.trim() || 'auto', labels,
    location_source: form.manualLocation ? 'manual' : '',
    location_name: form.manualLocation ? form.locationName.trim() : '',
  })
  saving.value = false
  if (ok) emit('close')
}
</script>

<template>
  <div class="fixed inset-0 z-[80] flex items-center justify-center bg-black/65 p-4" @click.self="emit('close')">
    <form class="panel flex max-h-[calc(100vh-2rem)] w-full max-w-3xl flex-col overflow-hidden" @submit.prevent="save">
      <div class="flex shrink-0 items-start justify-between border-b border-ink-700 px-6 py-5">
        <div><h3 class="text-base font-semibold text-white">编辑节点配置</h3><p class="mt-1 text-xs text-slate-500">保存后立即发布到所在网络，Agent 下一个探测周期自动更新；地理位置默认由客户端与服务器 GeoIP 自动维护。</p></div>
        <button type="button" class="text-slate-500 hover:text-white" @click="emit('close')">✕</button>
      </div>
      <div class="grid min-h-0 flex-1 gap-4 overflow-y-auto p-6 sm:grid-cols-2">
        <label class="space-y-1.5"><span class="text-xs text-slate-400">节点名称</span><input v-model="form.name" required class="input w-full" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">所属网络</span><input :value="network?.name + ' · ' + network?.cidr" disabled class="input w-full opacity-60" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">WireGuard 内网 IP</span><input v-model="form.address" required class="input w-full font-mono" placeholder="10.0.0.2" /><span class="block text-[11px] text-slate-600"><template v-if="reportedInterface?.tunnelIP">来自 Agent 最近上报的 {{ reportedInterface.name }}；</template>必须位于 {{ network?.cidr }} 中且不能与其他节点重复</span></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">公网 Endpoint</span><input v-model="form.endpoint" class="input w-full font-mono" placeholder="host.example.com:51820" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">监听端口</span><input v-model.number="form.listenPort" type="number" min="1" max="65535" required class="input w-full" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">MTU</span><input v-model.number="form.mtu" type="number" min="576" max="9000" required class="input w-full" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">采集接口选择器</span><input v-model="form.interfaceSelector" class="input w-full font-mono" placeholder="auto 或 wg0,wg1" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">拓扑角色</span><select v-model="form.role" class="input w-full"><option value="mesh">Mesh</option><option value="hub">Hub</option><option value="spoke">Spoke</option></select></label>

        <section class="space-y-4 rounded-xl border border-cyan-500/20 bg-cyan-500/[0.04] p-4 sm:col-span-2">
          <div class="flex flex-wrap items-center justify-between gap-3 rounded-lg bg-ink-900/70 px-3 py-2.5 ring-1 ring-ink-700/80">
            <div><p class="text-[11px] text-slate-500">当前定位状态</p><p class="mt-0.5 text-sm text-slate-200">{{ currentLocationSource }}<span v-if="props.agent.city" class="text-slate-500"> · {{ props.agent.city }}</span></p></div>
            <p v-if="hasCurrentLocation" class="font-mono text-xs text-slate-400">{{ props.agent.lat.toFixed(4) }}, {{ props.agent.lng.toFixed(4) }}</p>
            <p v-else class="text-xs text-amber-300">等待客户端上报公网 IP 或服务器 GeoIP 解析</p>
          </div>
          <label class="flex cursor-pointer items-center justify-between gap-4">
            <span><span class="block text-sm font-medium text-slate-200">使用手动位置覆盖自动定位</span><span class="mt-1 block text-[11px] leading-relaxed text-slate-500">通常无需开启。关闭手动位置后，Agent 后续心跳会自动恢复客户端/GeoIP 定位。</span></span>
            <input v-model="form.manualLocation" type="checkbox" class="h-4 w-4 shrink-0 accent-cyan-500" />
          </label>
          <div v-if="form.manualLocation" class="border-t border-ink-700/80 pt-4">
            <label class="space-y-1.5"><span class="text-xs text-slate-400">位置名称</span><input v-model="form.locationName" required class="input w-full" list="wiremesh-location-presets" placeholder="例如：上海、广州、东京、新加坡" /><span class="block text-[11px] leading-relaxed text-slate-600">只需填写位置名称，系统会自动匹配预设中心坐标；无法识别时优先沿用节点当前坐标，否则使用默认坐标。</span></label>
            <datalist id="wiremesh-location-presets"><option value="上海" /><option value="广州" /><option value="北京" /><option value="深圳" /><option value="成都" /><option value="重庆" /><option value="杭州" /><option value="香港" /><option value="台北" /><option value="东京" /><option value="新加坡" /><option value="法兰克福" /><option value="伦敦" /><option value="纽约" /><option value="悉尼" /></datalist>
          </div>
        </section>

        <label class="flex items-center justify-between rounded-xl border border-ink-700 bg-ink-900/60 px-4 py-3 sm:col-span-2"><span><span class="block text-sm text-slate-300">启用节点</span><span class="text-xs text-slate-600">停用后不再参与后续网络配置发布</span></span><input v-model="form.enabled" type="checkbox" class="h-4 w-4 accent-emerald-500" /></label>
        <p v-if="validationError" class="rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30 sm:col-span-2">{{ validationError }}</p>
      </div>
      <div class="flex shrink-0 justify-end gap-3 border-t border-ink-700 px-6 py-4"><button type="button" class="btn-secondary" @click="emit('close')">取消</button><button class="btn-primary" :disabled="saving">{{ saving ? '保存中…' : '保存配置' }}</button></div>
    </form>
  </div>
</template>
