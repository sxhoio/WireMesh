<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import type { Agent } from '../types'
import { useMeshStore } from '../stores/mesh'

const props = defineProps<{ agent: Agent }>()
const emit = defineEmits<{ close: [] }>()
const mesh = useMeshStore()
const saving = ref(false)
const form = reactive({
  name: props.agent.name,
  address: props.agent.address,
  endpoint: props.agent.publicIP,
  listenPort: props.agent.listenPort || 51820,
  mtu: props.agent.mtu || 1420,
  interfaceSelector: props.agent.interfaceSelector || 'auto',
  enabled: props.agent.enabled,
  role: (props.agent.labels.find((value) => value.startsWith('wiremesh.role='))?.split('=')[1] || 'mesh') as 'mesh' | 'hub' | 'spoke',
})
const network = computed(() => mesh.networkById(props.agent.networkId))

async function save() {
  saving.value = true
  const labels = Object.fromEntries(props.agent.labels.map((value) => {
    const index = value.indexOf('='); return index >= 0 ? [value.slice(0, index), value.slice(index + 1)] : [value, 'true']
  }))
  labels['wiremesh.role'] = form.role
  const ok = await mesh.updateNodeConfig(props.agent.id, {
    name: form.name.trim(), address: form.address.trim(), endpoint: form.endpoint.trim(),
    listen_port: Number(form.listenPort), mtu: Number(form.mtu), enabled: form.enabled,
    interface_selector: form.interfaceSelector.trim() || 'auto', labels,
  })
  saving.value = false
  if (ok) emit('close')
}
</script>

<template>
  <div class="fixed inset-0 z-[80] flex items-center justify-center bg-black/65 p-4" @click.self="emit('close')">
    <form class="panel w-full max-w-2xl overflow-hidden" @submit.prevent="save">
      <div class="flex items-start justify-between border-b border-ink-700 px-6 py-5">
        <div><h3 class="text-base font-semibold text-white">编辑节点 WireGuard 配置</h3><p class="mt-1 text-xs text-slate-500">保存后需要发布网络配置，Agent 才会收到新的内网 IP、端口和 MTU。</p></div>
        <button type="button" class="text-slate-500 hover:text-white" @click="emit('close')">✕</button>
      </div>
      <div class="grid gap-4 p-6 sm:grid-cols-2">
        <label class="space-y-1.5"><span class="text-xs text-slate-400">节点名称</span><input v-model="form.name" required class="input w-full" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">所属网络</span><input :value="network?.name + ' · ' + network?.cidr" disabled class="input w-full opacity-60" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">WireGuard 内网 IP</span><input v-model="form.address" required class="input w-full font-mono" placeholder="10.0.0.2" /><span class="block text-[11px] text-slate-600">必须位于 {{ network?.cidr }} 中且不能与其他节点重复</span></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">公网 Endpoint</span><input v-model="form.endpoint" class="input w-full font-mono" placeholder="host.example.com:51820" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">监听端口</span><input v-model.number="form.listenPort" type="number" min="1" max="65535" required class="input w-full" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">MTU</span><input v-model.number="form.mtu" type="number" min="576" max="9000" required class="input w-full" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">采集接口选择器</span><input v-model="form.interfaceSelector" class="input w-full font-mono" placeholder="auto 或 wg0,wg1" /></label>
        <label class="space-y-1.5"><span class="text-xs text-slate-400">拓扑角色</span><select v-model="form.role" class="input w-full"><option value="mesh">Mesh</option><option value="hub">Hub</option><option value="spoke">Spoke</option></select></label>
        <label class="sm:col-span-2 flex items-center justify-between rounded-xl border border-ink-700 bg-ink-900/60 px-4 py-3"><span><span class="block text-sm text-slate-300">启用节点</span><span class="text-xs text-slate-600">停用后不再参与后续网络配置发布</span></span><input v-model="form.enabled" type="checkbox" class="h-4 w-4 accent-emerald-500" /></label>
      </div>
      <div class="flex justify-end gap-3 border-t border-ink-700 px-6 py-4"><button type="button" class="btn-secondary" @click="emit('close')">取消</button><button class="btn-primary" :disabled="saving">{{ saving ? '保存中…' : '保存配置' }}</button></div>
    </form>
  </div>
</template>
