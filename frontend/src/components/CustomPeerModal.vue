<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'

const props = defineProps<{ networkId: string }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const app = useAppStore()
const mesh = useMeshStore()

const network = mesh.networkById(props.networkId)!
const keyword = ref('')
const selected = ref<Set<string>>(new Set())

// 初始选中：当前 customPairs 中出现的节点
network.customPairs.forEach(([a, b]) => {
  selected.value.add(a)
  selected.value.add(b)
})

interface Row {
  nodeId: string
  agentName: string
  hostname: string
  online: boolean
  tunnelIP: string
  networkName: string
  cross: boolean // 跨网络不可选
}

const rows = computed<Row[]>(() => {
  return mesh.agents
    .filter((agent) => agent.projectId === network.projectId)
    .map((agent) => ({
      nodeId: agent.id,
      agentName: agent.name,
      hostname: agent.hostname,
      online: agent.status === 'online' && agent.enabled,
      tunnelIP: agent.address,
      networkName: mesh.networkById(agent.networkId)?.name ?? agent.networkId,
      cross: agent.networkId !== network.id,
    }))
})

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return rows.value
  return rows.value.filter((r) => `${r.agentName} ${r.hostname} ${r.tunnelIP} ${r.networkName}`.toLowerCase().includes(kw))
})

const selectable = computed(() => rows.value.filter((r) => !r.cross))

function toggle(id: string) {
  const r = rows.value.find((x) => x.nodeId === id)
  if (!r || r.cross) return
  if (selected.value.has(id)) selected.value.delete(id)
  else selected.value.add(id)
  selected.value = new Set(selected.value)
}

function selectAll() {
  selected.value = new Set(selectable.value.map((r) => r.nodeId))
}

function clear() {
  selected.value = new Set()
}

async function save() {
  // Agent 注册时已经分配节点身份和隧道地址，无需等到本机出现 WireGuard 接口即可配置 Peer。
  const ids = selectable.value.filter((r) => selected.value.has(r.nodeId)).map((r) => r.nodeId)
  const pairs: [string, string][] = []
  for (let i = 0; i < ids.length; i++) {
    for (let j = i + 1; j < ids.length; j++) pairs.push([ids[i], ids[j]])
  }
  await mesh.setCustomPairs(props.networkId, pairs, app.username)
  if (!mesh.error) emit('close')
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-ink-950/70 p-4 backdrop-blur-sm" @click.self="emit('close')">
    <div class="panel flex max-h-[85vh] w-full max-w-2xl flex-col overflow-hidden">
      <div class="border-b border-ink-700 px-6 py-4">
        <h2 class="text-base font-semibold text-white">Custom Peer 选择 · {{ network.name }}</h2>
        <p class="mt-0.5 text-xs text-slate-500">手动选择需要互联的节点；节点尚未安装或采集到 WireGuard 接口时也可以预先配置</p>
      </div>

      <div class="flex items-center gap-3 border-b border-ink-700 px-6 py-3">
        <div class="relative flex-1">
          <svg viewBox="0 0 24 24" fill="none" class="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-5.197-5.197m0 0A7.5 7.5 0 105.196 5.196a7.5 7.5 0 0010.607 10.607z" /></svg>
          <input v-model="keyword" class="input pl-9 !py-2" placeholder="搜索 Agent / 主机名 / 隧道 IP…" />
        </div>
        <button class="btn-ghost !px-3 !py-2 text-xs" @click="selectAll">全选</button>
        <button class="btn-ghost !px-3 !py-2 text-xs" @click="clear">清空</button>
      </div>

      <div class="min-h-0 flex-1 overflow-y-auto px-6 py-3">
        <div class="space-y-1.5">
          <label
            v-for="r in filtered"
            :key="r.nodeId"
            class="flex items-center gap-3 rounded-xl px-3.5 py-2.5 ring-1 transition"
            :class="[
              r.cross ? 'cursor-not-allowed bg-ink-900/40 opacity-45 ring-ink-700' : 'cursor-pointer hover:bg-ink-800',
              selected.has(r.nodeId) && !r.cross ? 'bg-emerald-500/10 ring-emerald-500/40' : 'ring-ink-700',
            ]"
          >
            <input type="checkbox" class="h-4 w-4 rounded border-ink-600 bg-ink-800 accent-emerald-500" :checked="selected.has(r.nodeId)" :disabled="r.cross" @change="toggle(r.nodeId)" />
            <span class="h-2 w-2 shrink-0 rounded-full" :class="r.online ? 'bg-emerald-400' : 'bg-slate-600'"></span>
            <div class="min-w-0 flex-1">
              <p class="text-sm text-slate-200">{{ r.agentName }} <span class="text-slate-500">/</span> <span class="font-mono text-xs">{{ r.hostname }}</span></p>
              <p class="font-mono text-[11px] text-slate-500">分配地址 {{ r.tunnelIP || '等待分配' }}</p>
            </div>
            <span class="chip" :class="r.cross ? 'bg-slate-500/10 text-slate-500 ring-1 ring-slate-600' : 'bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30'">
              {{ r.networkName }}{{ r.cross ? '（跨网络）' : '' }}
            </span>
          </label>
        </div>
      </div>

      <div class="flex items-center justify-between border-t border-ink-700 px-6 py-4">
        <p class="text-xs text-slate-500">已选 <span class="font-semibold text-emerald-300">{{ selected.size }}</span> 个节点</p>
        <div class="flex gap-2.5">
          <button class="btn-ghost" @click="emit('close')">取消</button>
          <button class="btn-primary" @click="save">保存为待发布变更</button>
        </div>
      </div>
    </div>
  </div>
</template>
