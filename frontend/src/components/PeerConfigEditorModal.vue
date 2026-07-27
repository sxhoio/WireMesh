<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, type ApiPeerConfigFile } from '../api'
import type { Agent } from '../types'
import { useMeshStore } from '../stores/mesh'

const props = defineProps<{ agent: Agent }>()
const emit = defineEmits<{ close: [] }>()
const mesh = useMeshStore()

const loading = ref(false)
const saving = ref(false)
const error = ref('')
const hasPending = ref(false)
const files = ref<ApiPeerConfigFile[]>([])
const selectedInterface = ref('')
const draft = ref('')

const currentFile = computed(() => files.value.find((file) => file.interface === selectedInterface.value))
const interfaceOptions = computed(() => {
  const names = new Set<string>()
  files.value.forEach((file) => { if (file.interface) names.add(file.interface) })
  props.agent.interfaces.forEach((iface) => { if (iface.name) names.add(iface.name) })
  const selector = props.agent.interfaceSelector?.trim()
  if (selector && selector !== 'auto' && selector !== '*') {
    selector.split(',').map((item) => item.trim()).filter(Boolean).forEach((name) => names.add(name))
  }
  if (!names.size) names.add('wg0')
  return [...names]
})

function fileLabel(file?: ApiPeerConfigFile) {
  if (!file) return '未上传配置快照'
  const stamp = file.updated_at ? new Date(file.updated_at).toLocaleString('zh-CN') : '未知时间'
  return `${file.path || '/etc/wireguard/' + file.interface + '.conf'} · ${stamp}`
}

function selectInterface(name: string) {
  selectedInterface.value = name
  draft.value = files.value.find((file) => file.interface === name)?.content || ''
}

function clientValidatePeerConfig(content: string) {
  const normalized = content.toLowerCase()
  if (normalized.includes('[interface]') || normalized.includes('privatekey')) return '这里只能编辑 Peer 配置，不能包含 [Interface] 或 PrivateKey'
  const text = content.trim()
  if (!text) return ''
  if (!/^\s*(#.*|;.*|\[Peer\])/m.test(text)) return 'Peer 配置需要以 [Peer] 段落开始'
  return ''
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const result = await api.nodePeerConfig(props.agent.id)
    hasPending.value = result.has_pending
    const source = result.has_pending && result.pending_files?.length ? result.pending_files : result.files
    files.value = source || []
    const initial = files.value[0]?.interface || props.agent.interfaces[0]?.name || interfaceOptions.value[0] || 'wg0'
    selectInterface(initial)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载 Peer 配置失败'
  } finally {
    loading.value = false
  }
}

async function save() {
  const validation = clientValidatePeerConfig(draft.value)
  if (validation) {
    error.value = validation
    return
  }
  if (!selectedInterface.value.trim()) {
    error.value = '请选择或填写接口名'
    return
  }
  saving.value = true
  error.value = ''
  try {
    const result = await api.updateNodePeerConfig(props.agent.id, { interface: selectedInterface.value.trim(), content: draft.value })
    mesh.notice = result.message || (result.offline ? 'Peer 配置已保存，客户端上线后下发' : 'Peer 配置已保存并下发')
    await mesh.refresh()
    emit('close')
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '保存 Peer 配置失败'
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="fixed inset-0 z-[80] flex items-center justify-center bg-black/65 p-4" @click.self="emit('close')">
    <div class="panel flex max-h-[88vh] w-full max-w-4xl flex-col overflow-hidden">
      <div class="flex flex-wrap items-start justify-between gap-3 border-b border-ink-700 px-6 py-5">
        <div>
          <h3 class="text-base font-semibold text-white">{{ agent.name }} · 编辑 Peer</h3>
          <p class="mt-1 text-xs text-slate-500">展示 Agent 上传的实际 Peer 配置快照；保存后会立即下发到客户端并重启对应 WireGuard 接口。</p>
        </div>
        <button class="text-slate-500 hover:text-white" @click="emit('close')">✕</button>
      </div>

      <div class="min-h-0 flex-1 overflow-auto p-5">
        <p v-if="error" class="mb-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300">{{ error }}</p>
        <p v-if="hasPending" class="mb-3 rounded-lg bg-amber-500/10 px-3 py-2 text-xs text-amber-300 ring-1 ring-amber-500/30">当前存在待下发 Peer 配置，编辑器优先显示待下发内容。</p>
        <p v-if="loading" class="py-12 text-center text-sm text-slate-500">加载中…</p>

        <template v-else>
          <div class="grid gap-3 md:grid-cols-[12rem_1fr]">
            <div class="space-y-2">
              <label class="label">接口</label>
              <button
                v-for="name in interfaceOptions"
                :key="name"
                class="flex w-full items-center justify-between border border-ink-700 px-3 py-2 text-left font-mono text-xs transition hover:bg-ink-800"
                :class="selectedInterface === name ? 'bg-cyan-500/10 text-cyan-300' : 'bg-ink-900 text-slate-400'"
                @click="selectInterface(name)"
              >
                <span>{{ name }}</span>
                <span v-if="files.some((file) => file.interface === name)" class="text-[10px] text-emerald-300">snapshot</span>
              </button>
              <input v-model="selectedInterface" class="input font-mono text-xs" placeholder="wg0" @change="selectInterface(selectedInterface)" />
            </div>

            <div class="min-w-0">
              <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
                <p class="truncate text-xs text-slate-500">{{ fileLabel(currentFile) }}</p>
                <button class="btn-secondary !py-1.5 text-xs" :disabled="loading" @click="load">重新加载</button>
              </div>
              <textarea
                v-model="draft"
                spellcheck="false"
                class="h-[52vh] w-full resize-none border-y border-ink-700 bg-[#05070a] px-3 py-2 font-mono text-xs leading-5 text-slate-300 outline-none placeholder:text-slate-700 focus:border-cyan-500/50"
                placeholder="[Peer]&#10;PublicKey = ...&#10;AllowedIPs = 10.88.88.2/32&#10;Endpoint = example.com:51820&#10;PersistentKeepalive = 25"
              ></textarea>
              <p class="mt-2 text-[11px] leading-relaxed text-slate-500">为避免覆盖接口私钥，这里只允许保存 [Peer] 段落；[Interface]、PrivateKey 等接口配置请继续使用“编辑接口设置”。</p>
            </div>
          </div>
        </template>
      </div>

      <div class="flex items-center justify-end gap-2 border-t border-ink-700 px-6 py-4">
        <button class="btn-secondary" @click="emit('close')">取消</button>
        <button v-if="!loading" class="btn-primary" :disabled="saving" @click="save">{{ saving ? '保存中…' : '保存并下发' }}</button>
      </div>
    </div>
  </div>
</template>
