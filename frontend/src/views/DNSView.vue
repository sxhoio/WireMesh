<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api, type ApiDNSRecord } from '../api'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import { useClipboard } from '../composables/useClipboard'

const app = useAppStore()
const mesh = useMeshStore()

const selectedNetworkId = ref('')
const records = ref<ApiDNSRecord[]>([])
const loading = ref(false)
const error = ref('')
const saving = ref(false)
const form = reactive({ name: '', address: '', description: '' })
const { copied, copyText } = useClipboard(false, 1400)

const networkNodes = computed(() => mesh.agents.filter((agent) => agent.networkId === selectedNetworkId.value))

async function load() {
  if (!selectedNetworkId.value) return
  loading.value = true
  error.value = ''
  try {
    records.value = await api.dnsRecords(selectedNetworkId.value)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载 DNS 记录失败'
  } finally {
    loading.value = false
  }
}

watch(selectedNetworkId, () => {
  form.name = ''
  form.address = ''
  form.description = ''
  void load()
})

async function save() {
  if (!selectedNetworkId.value || saving.value || !app.canOperate) return
  if (!form.name.trim() || !form.address.trim()) {
    error.value = '请输入记录名称和 IP 地址'
    return
  }
  saving.value = true
  error.value = ''
  try {
    await api.createDNSRecord(selectedNetworkId.value, {
      name: form.name.trim().toLowerCase(),
      address: form.address.trim(),
      description: form.description.trim(),
    })
    form.name = ''
    form.address = ''
    form.description = ''
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '创建 DNS 记录失败'
  } finally {
    saving.value = false
  }
}

async function remove(record: ApiDNSRecord) {
  try {
    await api.deleteDNSRecord(selectedNetworkId.value, record.id)
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '删除记录失败'
  }
}

async function copyHosts() {
  const lines = [...automaticRows.value, ...records.value]
    .map((row) => `${row.address}  ${row.name}`)
    .join('\n')
  await copyText(lines, true)
}

const automaticRows = computed(() => networkNodes.value.map((agent) => ({ name: agent.hostname || agent.name, address: agent.address, description: '节点自动映射' })))
</script>

<template>
  <div class="flex h-full flex-col gap-5">
    <div class="flex flex-wrap items-center gap-3">
      <div>
        <label class="label">网络</label>
        <select v-model="selectedNetworkId" class="input !w-64">
          <option value="">选择网络</option>
          <option v-for="n in mesh.networks" :key="n.id" :value="n.id">{{ n.name }}（{{ n.cidr }}）</option>
        </select>
      </div>
      <p class="mb-2 text-xs text-slate-500">私有 DNS 映射：节点主机名自动解析到隧道 IP，手动记录用于额外的服务名；可复制 /etc/hosts 格式导入节点。</p>
    </div>

    <p v-if="error" class="rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ error }}</p>

    <div class="panel min-h-0 flex-1 overflow-y-auto p-5">
      <div class="mb-3 flex items-center justify-between">
        <h2 class="text-sm font-semibold text-white">DNS 记录</h2>
        <button v-if="selectedNetworkId" class="btn-ghost !py-1.5 text-xs" @click="copyHosts">{{ copied ? '已复制 ✓' : '复制 /etc/hosts 格式' }}</button>
      </div>
      <p v-if="!selectedNetworkId" class="py-6 text-center text-xs text-slate-500">请先选择网络</p>
      <template v-else>
        <div class="space-y-1.5">
          <div class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] gap-3 rounded-lg bg-ink-900/60 px-3 py-2 text-[11px] uppercase tracking-wide text-slate-600">
            <span>名称</span><span>地址</span><span></span>
          </div>
          <div v-for="row in automaticRows" :key="'auto-' + row.name" class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-3 rounded-lg bg-ink-800/40 px-3 py-2.5 ring-1 ring-ink-700">
            <span class="truncate font-mono text-xs text-slate-300">{{ row.name }}</span>
            <span class="truncate font-mono text-xs text-cyan-300">{{ row.address }}</span>
            <span class="chip bg-slate-500/10 text-slate-500 ring-1 ring-slate-600">自动</span>
          </div>
          <div v-for="record in records" :key="record.id" class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-3 rounded-lg bg-ink-800/60 px-3 py-2.5 ring-1 ring-ink-600">
            <span class="truncate font-mono text-xs text-slate-200">{{ record.name }}</span>
            <span class="truncate font-mono text-xs text-emerald-300">{{ record.address }}</span>
            <button v-if="app.canOperate" class="chip bg-red-500/10 text-red-300 ring-1 ring-red-500/30" @click="remove(record)">删除</button>
          </div>
          <p v-if="!automaticRows.length && !records.length" class="py-6 text-center text-xs text-slate-500">暂无记录</p>
        </div>

        <div class="mt-4 flex flex-wrap items-end gap-3 border-t border-ink-700 pt-4">
          <div><label class="label">名称</label><input v-model="form.name" class="input font-mono" placeholder="db.internal" /></div>
          <div><label class="label">IP 地址</label><input v-model="form.address" class="input font-mono" placeholder="10.0.0.5" /></div>
          <div class="flex-1"><label class="label">描述（可选）</label><input v-model="form.description" class="input" /></div>
          <button class="btn-primary" :disabled="!app.canOperate || saving || !form.name.trim() || !form.address.trim()" @click="save">{{ saving ? '创建中…' : '添加记录' }}</button>
        </div>
      </template>
    </div>
  </div>
</template>
