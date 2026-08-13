<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { api, type ApiDNSRecord } from '../api'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import type { Agent } from '../types'
import { useClipboard } from '../composables/useClipboard'
import { requestConfirm } from '../utils/confirm'
import { fmtDateTime } from '../utils/format'

const app = useAppStore()
const mesh = useMeshStore()
const router = useRouter()

const selectedNetworkId = ref('')
const records = ref<ApiDNSRecord[]>([])
const loading = ref(false)
const error = ref('')
const notice = ref('')
const saving = ref(false)
const editingRecordId = ref<string | null>(null)
const keyword = ref('')
const form = reactive({ name: '', address: '', description: '' })
const nameError = ref('')
const addressError = ref('')
const { copied, copyText } = useClipboard(false, 1400)

const networkNodes = computed(() => mesh.agents.filter((agent) => agent.networkId === selectedNetworkId.value))
const selectedNetwork = computed(() => (selectedNetworkId.value ? mesh.networkById(selectedNetworkId.value) : undefined))

type AutoRow = { name: string; address: string; agent: Agent; conflict: boolean }

const hostnamePattern = /^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$/i

/** 节点自动映射：同名主机名去重并标注冲突，hosts 复制时以注释说明 */
const automaticRows = computed<AutoRow[]>(() => {
  const byName = new Map<string, Agent[]>()
  for (const agent of networkNodes.value) {
    const base = (agent.hostname || agent.name).toLowerCase()
    const list = byName.get(base) || []
    list.push(agent)
    byName.set(base, list)
  }
  const rows: AutoRow[] = []
  for (const [base, agents] of byName) {
    const conflict = agents.length > 1
    for (const agent of agents) {
      rows.push({ name: base, address: agent.address, agent, conflict })
    }
  }
  return rows.sort((a, b) => a.name.localeCompare(b.name) || a.agent.name.localeCompare(b.agent.name))
})

const keywordLower = computed(() => keyword.value.trim().toLowerCase())
const filteredRecords = computed(() => {
  if (!keywordLower.value) return records.value
  return records.value.filter((record) =>
    `${record.name} ${record.address} ${record.description || ''}`.toLowerCase().includes(keywordLower.value))
})
const filteredAutoRows = computed(() => {
  if (!keywordLower.value) return automaticRows.value
  return automaticRows.value.filter((row) =>
    `${row.name} ${row.address} ${row.agent.name}`.toLowerCase().includes(keywordLower.value))
})

function validateName() {
  const name = form.name.trim().toLowerCase()
  if (!name) return '请输入记录名称'
  if (name.length > 253) return '名称不能超过 253 个字符'
  if (!hostnamePattern.test(name)) return '名称只能包含小写字母、数字、点与连字符'
  return ''
}

function validateAddress() {
  const address = form.address.trim()
  if (!address) return '请输入 IP 地址'
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(address)) {
    const ok = address.split('.').every((part) => Number(part) >= 0 && Number(part) <= 255)
    return ok ? '' : 'IPv4 每段必须在 0-255 之间'
  }
  if (address.includes(':')) {
    const cleaned = address.replace(/\[|\]/g, '')
    return /^[0-9a-fA-F:]+$/.test(cleaned) && cleaned.split(':').length >= 3 ? '' : 'IPv6 地址格式不正确'
  }
  return '请输入合法的 IPv4 或 IPv6 地址'
}

async function load(silent = false) {
  if (!selectedNetworkId.value) return
  if (!silent) loading.value = true
  if (!silent) error.value = ''
  try {
    records.value = await api.dnsRecords(selectedNetworkId.value)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载 DNS 记录失败'
  } finally {
    if (!silent) loading.value = false
  }
}

watch(selectedNetworkId, () => {
  resetForm()
  notice.value = ''
  void load()
})

function resetForm() {
  editingRecordId.value = null
  form.name = ''
  form.address = ''
  form.description = ''
  nameError.value = ''
  addressError.value = ''
}

function editRecord(record: ApiDNSRecord) {
  editingRecordId.value = record.id
  form.name = record.name
  form.address = record.address
  form.description = record.description || ''
  nameError.value = ''
  addressError.value = ''
}

async function save() {
  if (!selectedNetworkId.value || saving.value || !app.canOperate) return
  nameError.value = validateName()
  addressError.value = validateAddress()
  if (nameError.value || addressError.value) return
  const isEdit = Boolean(editingRecordId.value)
  saving.value = true
  error.value = ''
  const payload = {
    name: form.name.trim().toLowerCase(),
    address: form.address.trim(),
    description: form.description.trim(),
  }
  try {
    if (isEdit) await api.updateDNSRecord(selectedNetworkId.value, editingRecordId.value!, payload)
    else await api.createDNSRecord(selectedNetworkId.value, payload)
    resetForm()
    notice.value = isEdit ? '记录已更新' : '记录已添加'
    await load(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '保存 DNS 记录失败'
  } finally {
    saving.value = false
  }
}

async function remove(record: ApiDNSRecord) {
  const confirmed = await requestConfirm({
    title: '删除 DNS 记录',
    message: `确定删除记录“${record.name}”吗？此操作无法恢复。`,
    confirmText: '删除记录',
    variant: 'danger',
  })
  if (!confirmed) return
  try {
    await api.deleteDNSRecord(selectedNetworkId.value, record.id)
    if (editingRecordId.value === record.id) resetForm()
    notice.value = '记录已删除'
    await load(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '删除记录失败'
  }
}

/** 生成 /etc/hosts 片段：注释头 + 自动映射（冲突以注释说明）+ 手动记录 */
function hostsContent() {
  const network = selectedNetwork.value
  const header = [
    `# WireMesh 网络 ${network?.name || ''} · 生成于 ${new Date().toLocaleString('zh-CN')}`,
    `# 自动映射 ${automaticRows.value.length} 条 · 手动记录 ${records.value.length} 条`,
  ]
  const autoLines = automaticRows.value.map((row) =>
    `${row.address}  ${row.name}${row.conflict ? `  # 冲突：${row.agent.name} 与其它节点共用主机名` : ''}`)
  const manualLines = records.value.map((record) => `${record.address}  ${record.name}${record.description ? `  # ${record.description}` : ''}`)
  return [...header, ...autoLines, ...manualLines].join('\n')
}

async function copyHosts() {
  await copyText(hostsContent(), true)
}

function downloadHosts() {
  const blob = new Blob([hostsContent() + '\n'], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `${selectedNetwork.value?.name || 'wiremesh'}-hosts.txt`
  anchor.click()
  URL.revokeObjectURL(url)
}

async function copyRow(text: string) {
  await copyText(text, true)
}

let refreshTimer: number | undefined
onMounted(() => {
  refreshTimer = window.setInterval(() => {
    if (document.hidden || editingRecordId.value || saving.value) return
    void load(true)
  }, 30000)
})
onUnmounted(() => window.clearInterval(refreshTimer))
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
      <p class="mb-2 max-w-2xl text-xs leading-relaxed text-slate-500">私有 DNS 映射表：节点主机名自动对应隧道 IP，手动记录用于额外的服务名。WireGuard 本身不解析 DNS——请把生成的 /etc/hosts 片段导入节点，或在节点侧配置 DNS 服务。</p>
    </div>

    <!-- 上游 DNS 设置 -->
    <div v-if="selectedNetwork" class="flex flex-wrap items-center gap-3 rounded-xl bg-ink-900/60 px-4 py-2.5 ring-1 ring-ink-700">
      <p class="text-xs text-slate-400">网络上游 DNS：<span class="font-mono text-cyan-300">{{ selectedNetwork.dns || '未配置' }}</span></p>
      <p class="text-[11px] text-slate-600">用于客户端配置的 [Interface] DNS 字段，在本页不生效</p>
      <button class="ml-auto rounded-md px-2 py-1 text-[11px] text-cyan-300 transition hover:bg-cyan-500/10" @click="router.push({ name: 'settings' })">去「系统设置 → 网络」修改</button>
    </div>

    <p v-if="notice" class="rounded-lg bg-emerald-500/10 px-3 py-2 text-xs text-emerald-300 ring-1 ring-emerald-500/30">{{ notice }}</p>
    <p v-if="error" class="rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ error }}</p>

    <!-- 空状态：没有网络 -->
    <div v-if="!mesh.networks.length" class="panel p-8 text-center">
      <p class="text-sm text-slate-300">还没有可用的网络</p>
      <p class="mx-auto mt-1 max-w-xl text-xs leading-relaxed text-slate-500">DNS 记录归属于具体网络。请先在「系统设置 → 项目与网络」中创建项目与网络。</p>
      <div class="mt-4 flex justify-center gap-2.5">
        <button class="btn-primary" @click="router.push({ name: 'settings' })">前往系统设置</button>
        <button class="btn-secondary" @click="router.push({ name: 'nodes' })">接入节点</button>
      </div>
    </div>

    <div v-else class="panel flex min-h-0 flex-1 flex-col p-5">
      <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 class="text-sm font-semibold text-white">DNS 记录 <span class="ml-1 text-xs font-normal text-slate-500">自动 {{ automaticRows.length }} · 手动 {{ records.length }}</span></h2>
          <p class="mt-0.5 text-xs text-slate-500">自动记录来自节点主机名，无需维护；手动记录随网络保存。</p>
        </div>
        <div class="flex items-center gap-2">
          <div class="relative">
            <svg viewBox="0 0 24 24" fill="none" class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-slate-500" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-5.197-5.197m0 0A7.5 7.5 0 105.196 5.196a7.5 7.5 0 0010.607 10.607z" /></svg>
            <input v-model="keyword" class="input !w-48 !py-1.5 !pl-8 !text-xs" placeholder="搜索名称 / IP / 描述…" />
          </div>
          <button v-if="selectedNetworkId" class="btn-ghost !py-1.5 text-xs" :disabled="!automaticRows.length && !records.length" @click="copyHosts">{{ copied ? '已复制 ✓' : '复制 /etc/hosts 格式' }}</button>
          <button v-if="selectedNetworkId" class="btn-ghost !py-1.5 text-xs" :disabled="!automaticRows.length && !records.length" @click="downloadHosts">下载 hosts 文件</button>
        </div>
      </div>

      <p v-if="!selectedNetworkId" class="py-6 text-center text-xs text-slate-500">请先选择网络</p>
      <p v-else-if="loading && !records.length" class="py-6 text-center text-xs text-slate-500">加载中…</p>
      <template v-else>
        <div class="min-h-0 flex-1 space-y-1.5 overflow-y-auto pr-1">
          <div class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto] gap-3 rounded-lg bg-ink-900/60 px-3 py-2 text-[11px] uppercase tracking-wide text-slate-600">
            <span>名称</span><span>地址</span><span>描述 / 来源</span><span></span>
          </div>

          <!-- 自动映射 -->
          <div v-for="row in filteredAutoRows" :key="'auto-' + row.agent.id + '-' + row.name" class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-3 rounded-lg bg-ink-800/40 px-3 py-2.5 ring-1 ring-ink-700">
            <span class="flex min-w-0 items-center gap-2">
              <span class="h-2 w-2 shrink-0 rounded-full" :class="!row.agent.enabled ? 'bg-slate-600' : row.agent.status === 'online' ? 'bg-emerald-400' : 'bg-slate-500'"></span>
              <span class="truncate font-mono text-xs text-slate-300">{{ row.name }}</span>
            </span>
            <span class="truncate font-mono text-xs text-cyan-300">{{ row.address }}</span>
            <span class="flex min-w-0 items-center gap-1.5">
              <span class="truncate text-xs text-slate-500">{{ row.agent.name }}</span>
              <span v-if="row.conflict" class="chip shrink-0 bg-amber-500/10 text-amber-300 ring-1 ring-amber-500/30" title="多个节点共用同一主机名，hosts 中已加注释说明">冲突</span>
            </span>
            <div class="flex items-center gap-1.5">
              <span class="chip shrink-0 bg-slate-500/10 text-slate-500 ring-1 ring-slate-600">自动</span>
              <button class="rounded-md p-1 text-slate-500 transition hover:bg-ink-800 hover:text-slate-300" title="复制单条 hosts 记录" @click="copyRow(`${row.address}  ${row.name}`)">⧉</button>
            </div>
          </div>

          <!-- 手动记录 -->
          <div v-for="record in filteredRecords" :key="record.id" class="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto] items-center gap-3 rounded-lg bg-ink-800/60 px-3 py-2.5 ring-1 ring-ink-600">
            <span class="truncate font-mono text-xs text-slate-200">{{ record.name }}</span>
            <span class="truncate font-mono text-xs text-emerald-300">{{ record.address }}</span>
            <span class="truncate text-xs text-slate-500" :title="`创建于 ${fmtDateTime(Date.parse(record.created_at))}`">{{ record.description || '—' }}</span>
            <div class="flex items-center gap-1.5">
              <button v-if="app.canOperate" class="chip shrink-0 bg-slate-500/10 text-slate-300 ring-1 ring-slate-500/30" @click="editRecord(record)">编辑</button>
              <button class="rounded-md p-1 text-slate-500 transition hover:bg-ink-800 hover:text-slate-300" title="复制单条 hosts 记录" @click="copyRow(`${record.address}  ${record.name}`)">⧉</button>
              <button v-if="app.canOperate" class="chip shrink-0 bg-red-500/10 text-red-300 ring-1 ring-red-500/30" @click="remove(record)">删除</button>
            </div>
          </div>

          <p v-if="!filteredAutoRows.length && !filteredRecords.length" class="py-6 text-center text-xs text-slate-500">{{ keyword ? '当前搜索条件下没有记录' : '暂无记录：接入节点后自动生成主机名映射，或手动添加服务名' }}</p>
        </div>

        <div class="mt-4 flex flex-wrap items-end gap-3 border-t border-ink-700 pt-4">
          <div>
            <label class="label">名称</label>
            <input v-model="form.name" class="input font-mono" placeholder="db.internal" @blur="nameError = validateName()" />
            <p v-if="nameError" class="mt-1 text-[11px] text-red-300">{{ nameError }}</p>
          </div>
          <div>
            <label class="label">IP 地址</label>
            <input v-model="form.address" class="input font-mono" placeholder="10.0.0.5" @blur="addressError = validateAddress()" />
            <p v-if="addressError" class="mt-1 text-[11px] text-red-300">{{ addressError }}</p>
          </div>
          <div class="flex-1">
            <label class="label">描述（可选）</label>
            <input v-model="form.description" class="input" />
          </div>
          <button v-if="editingRecordId" class="btn-ghost" @click="resetForm">取消编辑</button>
          <button class="btn-primary" :disabled="!app.canOperate || saving || !form.name.trim() || !form.address.trim()" @click="save">{{ saving ? '保存中…' : editingRecordId ? '保存修改' : '添加记录' }}</button>
        </div>
      </template>
    </div>
  </div>
</template>
