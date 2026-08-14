<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { api, type ApiAlertEvent, type ApiAlertRule } from '../api'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import { requestConfirm } from '../utils/confirm'
import { ago, fmtDateTime } from '../utils/format'

const app = useAppStore()
const mesh = useMeshStore()

const rules = ref<ApiAlertRule[]>([])
const events = ref<ApiAlertEvent[]>([])
const eventsHasMore = ref(false)
const eventsOffset = ref(0)
const eventsLoadingMore = ref(false)
const loading = ref(false)
const error = ref('')
const eventsError = ref('')
const saving = ref(false)
const editingId = ref<string | null>(null)
const formError = ref('')
const evaluatingId = ref<string | null>(null)
const clearing = ref(false)
const eventFilter = ref<'all' | 'sent' | 'failed' | 'recorded' | 'recovered'>('all')

type DurationUnit = 's' | 'm' | 'h'

const typeMeta: Record<ApiAlertRule['type'], { label: string; description: string; chip: string; recommendedSec: number }> = {
  node_offline: { label: '节点离线', description: '节点超过阈值时间未上报心跳，判定为离线。推荐阈值 5 分钟（300 秒）。', chip: 'bg-red-500/10 text-red-300 ring-red-500/30', recommendedSec: 300 },
  link_down: { label: '链路握手超时', description: 'WireGuard Peer 最近握手时间超过阈值，判定链路异常。推荐阈值 3 分钟（180 秒）。', chip: 'bg-amber-500/10 text-amber-300 ring-amber-500/30', recommendedSec: 180 },
  config_failed: { label: '配置下发失败', description: '节点应用配置失败（按最近一次失败时间判断）。推荐阈值 10 分钟（600 秒）。', chip: 'bg-violet-500/10 text-violet-300 ring-violet-500/30', recommendedSec: 600 },
}

const eventStatusMeta: Record<ApiAlertEvent['status'], { label: string; chip: string }> = {
  sent: { label: '已告警', chip: 'bg-amber-500/10 text-amber-300 ring-amber-500/30' },
  failed: { label: '推送失败', chip: 'bg-red-500/10 text-red-300 ring-red-500/30' },
  recorded: { label: '仅记录', chip: 'bg-slate-500/10 text-slate-400 ring-slate-500/30' },
  recovered: { label: '已恢复', chip: 'bg-emerald-500/10 text-emerald-300 ring-emerald-500/30' },
}

function toSeconds(value: number, unit: DurationUnit) {
  return unit === 'h' ? Math.round(value * 3600) : unit === 'm' ? Math.round(value * 60) : Math.round(value)
}

function decompose(sec: number): { value: number; unit: DurationUnit } {
  if (sec > 0 && sec % 3600 === 0) return { value: sec / 3600, unit: 'h' }
  if (sec > 0 && sec % 60 === 0) return { value: sec / 60, unit: 'm' }
  return { value: sec, unit: 's' }
}

function fmtDuration(sec: number) {
  if (sec === 0) return '0 秒'
  if (sec % 3600 === 0) return `${sec / 3600} 小时`
  if (sec % 60 === 0) return `${sec / 60} 分钟`
  return `${sec} 秒`
}

const form = reactive({
  name: '',
  type: 'node_offline' as ApiAlertRule['type'],
  thresholdValue: 5,
  thresholdUnit: 'm' as DurationUnit,
  quietValue: 60,
  quietUnit: 'm' as DurationUnit,
  channel_ids: [] as string[],
  enabled: true,
  scopeType: 'all' as 'all' | 'network' | 'node',
  scopeIds: [] as string[],
})

const unitOptions: { value: DurationUnit; label: string }[] = [
  { value: 's', label: '秒' },
  { value: 'm', label: '分钟' },
  { value: 'h', label: '小时' },
]

const scopeOptions: { value: 'all' | 'network' | 'node'; label: string; hint: string }[] = [
  { value: 'all', label: '全部节点', hint: '作用于租户内所有启用节点' },
  { value: 'network', label: '指定网络', hint: '只监控所选网络内的节点' },
  { value: 'node', label: '指定节点', hint: '只监控所选节点' },
]

function channelName(id: string) {
  return mesh.notifyChannels.find((channel) => channel.id === id)?.name || '已删除的渠道'
}
function channelMissing(id: string) {
  return !mesh.notifyChannels.some((channel) => channel.id === id)
}
function networkName(id: string) {
  return mesh.networks.find((network) => network.id === id)?.name || '已删除的网络'
}
function nodeName(id: string) {
  return mesh.agents.find((agent) => agent.id === id)?.name || '已删除的节点'
}

function scopeLabel(rule: ApiAlertRule) {
  if (rule.scope_type === 'network') return `网络 × ${rule.scope_ids.length}`
  if (rule.scope_type === 'node') return `节点 × ${rule.scope_ids.length}`
  return '全部节点'
}

function scopeNames(rule: ApiAlertRule) {
  if (rule.scope_type === 'network') return rule.scope_ids.map(networkName).join('、')
  if (rule.scope_type === 'node') return rule.scope_ids.map(nodeName).join('、')
  return ''
}

function toggleChannel(id: string) {
  const index = form.channel_ids.indexOf(id)
  if (index >= 0) form.channel_ids.splice(index, 1)
  else form.channel_ids.push(id)
}

function toggleScopeId(id: string) {
  const index = form.scopeIds.indexOf(id)
  if (index >= 0) form.scopeIds.splice(index, 1)
  else form.scopeIds.push(id)
}

function applyRecommended() {
  const threshold = decompose(typeMeta[form.type].recommendedSec)
  form.thresholdValue = threshold.value
  form.thresholdUnit = threshold.unit
}

function resetForm() {
  editingId.value = null
  form.name = ''
  form.type = 'node_offline'
  form.thresholdValue = 5
  form.thresholdUnit = 'm'
  form.quietValue = 60
  form.quietUnit = 'm'
  form.channel_ids = []
  form.enabled = true
  form.scopeType = 'all'
  form.scopeIds = []
  formError.value = ''
}

function editRule(rule: ApiAlertRule) {
  editingId.value = rule.id
  form.name = rule.name
  form.type = rule.type
  const threshold = decompose(rule.threshold_sec)
  form.thresholdValue = threshold.value
  form.thresholdUnit = threshold.unit
  const quiet = decompose(rule.quiet_sec)
  form.quietValue = quiet.value
  form.quietUnit = quiet.unit
  form.channel_ids = [...rule.channel_ids]
  form.enabled = rule.enabled
  form.scopeType = rule.scope_type || 'all'
  form.scopeIds = [...(rule.scope_ids || [])]
  formError.value = ''
}

function validateForm() {
  if (!form.name.trim()) return '请输入规则名称'
  const thresholdSec = toSeconds(form.thresholdValue, form.thresholdUnit)
  if (!Number.isInteger(thresholdSec) || thresholdSec < 1 || thresholdSec > 86400) return '阈值换算后必须在 1-86400 秒之间'
  const quietSec = toSeconds(form.quietValue, form.quietUnit)
  if (!Number.isInteger(quietSec) || quietSec < 0 || quietSec > 604800) return '静默期换算后必须在 0-604800 秒之间'
  if (form.scopeType !== 'all' && !form.scopeIds.length) return '请至少选择一个作用范围对象'
  return ''
}

async function saveRule() {
  const validation = validateForm()
  if (validation) {
    formError.value = validation
    return
  }
  saving.value = true
  formError.value = ''
  const payload = {
    name: form.name.trim(),
    type: form.type,
    threshold_sec: toSeconds(form.thresholdValue, form.thresholdUnit),
    quiet_sec: toSeconds(form.quietValue, form.quietUnit),
    channel_ids: form.channel_ids,
    enabled: form.enabled,
    scope_type: form.scopeType,
    scope_ids: form.scopeType === 'all' ? [] : form.scopeIds,
  }
  try {
    if (editingId.value) await api.updateAlertRule(editingId.value, payload)
    else await api.createAlertRule(payload)
    resetForm()
    await load(true)
  } catch (reason) {
    formError.value = reason instanceof Error ? reason.message : '保存规则失败'
  } finally {
    saving.value = false
  }
}

async function removeRule(rule: ApiAlertRule) {
  const confirmed = await requestConfirm({
    title: '删除告警规则',
    message: `确定删除规则“${rule.name}”吗？此操作无法恢复。`,
    confirmText: '删除规则',
    variant: 'danger',
  })
  if (!confirmed) return
  try {
    await api.deleteAlertRule(rule.id)
    if (editingId.value === rule.id) resetForm()
    await load(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '删除规则失败'
  }
}

async function toggleRule(rule: ApiAlertRule) {
  try {
    await api.updateAlertRule(rule.id, {
      name: rule.name, type: rule.type, threshold_sec: rule.threshold_sec,
      quiet_sec: rule.quiet_sec, channel_ids: rule.channel_ids, enabled: !rule.enabled,
      scope_type: rule.scope_type || 'all', scope_ids: rule.scope_ids || [],
    })
    await load(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '更新规则失败'
  }
}

async function evaluateRule(rule: ApiAlertRule) {
  if (evaluatingId.value) return
  evaluatingId.value = rule.id
  try {
    const result = await api.evaluateAlertRule(rule.id)
    mesh.notice = `评估完成：检查 ${result.evaluated} 个节点，命中 ${result.triggered} 个，结果已写入告警历史`
    await load(true)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '立即评估失败'
  } finally {
    evaluatingId.value = null
  }
}

async function clearEvents() {
  if (clearing.value || !events.value.length) return
  const confirmed = await requestConfirm({
    title: '清空告警历史',
    message: `确定清空全部 ${events.value.length} 条告警记录吗？此操作无法恢复。`,
    confirmText: '清空历史',
    variant: 'danger',
  })
  if (!confirmed) return
  clearing.value = true
  try {
    await api.clearAlertEvents()
    events.value = []
    mesh.notice = '告警历史已清空'
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '清空告警历史失败'
  } finally {
    clearing.value = false
  }
}

async function load(silent = false) {
  if (!silent) loading.value = true
  error.value = ''
  eventsError.value = ''
  const [ruleResult, eventResult] = await Promise.allSettled([api.alertRules(), api.alertEvents()])
  if (ruleResult.status === 'fulfilled') rules.value = ruleResult.value
  else error.value = ruleResult.reason instanceof Error ? ruleResult.reason.message : '加载规则失败'
  if (eventResult.status === 'fulfilled') {
    events.value = eventResult.value.items
    eventsOffset.value = eventResult.value.items.length
    eventsHasMore.value = eventResult.value.has_more
  } else eventsError.value = eventResult.reason instanceof Error ? eventResult.reason.message : '加载告警历史失败'
  if (!silent) loading.value = false
}

async function loadMoreEvents() {
  if (eventsLoadingMore.value || !eventsHasMore.value) return
  eventsLoadingMore.value = true
  try {
    const page = await api.alertEvents(100, eventsOffset.value)
    events.value = [...events.value, ...page.items]
    eventsOffset.value += page.items.length
    eventsHasMore.value = page.has_more
  } catch (reason) {
    eventsError.value = reason instanceof Error ? reason.message : '加载告警历史失败'
  } finally {
    eventsLoadingMore.value = false
  }
}

const filteredEvents = computed(() => {
  if (eventFilter.value === 'all') return events.value
  return events.value.filter((event) => event.status === eventFilter.value)
})

let refreshTimer: number | undefined
onMounted(() => {
  void load()
  refreshTimer = window.setInterval(() => {
    if (!document.hidden) void load(true)
  }, 30000)
})
onUnmounted(() => window.clearInterval(refreshTimer))
</script>

<template>
  <div class="flex h-full flex-col gap-5">
    <p v-if="error" class="rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ error }}</p>

    <div class="grid min-h-0 flex-1 grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1.1fr)_minmax(26rem,0.9fr)]">
      <!-- 规则列表 -->
      <div class="panel min-h-0 overflow-y-auto p-5">
        <div class="mb-3 flex items-center justify-between">
          <div>
            <h2 class="text-sm font-semibold text-white">告警规则</h2>
            <p class="mt-0.5 text-xs text-slate-500">条件满足时向绑定的通知渠道发送告警，并在静默期内对同一节点只告警一次；故障恢复后会发送恢复通知。</p>
          </div>
          <div class="flex shrink-0 items-center gap-2">
            <span class="chip bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">{{ rules.length }} 条规则</span>
            <button class="rounded-md p-1 text-slate-500 transition hover:bg-ink-800 hover:text-slate-300" :disabled="loading" title="刷新规则与历史" aria-label="刷新规则与历史" @click="load(false)">
              <svg viewBox="0 0 24 24" fill="none" class="h-3.5 w-3.5" :class="{ 'animate-spin': loading }" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M16.023 9.348h4.992v-.001M2.985 19.644v-4.992m0 0h4.992m-4.993 0l3.181 3.183a8.25 8.25 0 0013.803-3.7M4.031 9.865a8.25 8.25 0 0113.803-3.7l3.181 3.182m0-4.991v4.99" /></svg>
            </button>
          </div>
        </div>
        <p v-if="loading && !rules.length" class="py-8 text-center text-xs text-slate-500">加载中…</p>
        <p v-else-if="!rules.length" class="py-8 text-center text-xs text-slate-500">暂无告警规则，请创建第一条规则。</p>
        <div v-else class="space-y-2.5">
          <div v-for="rule in rules" :key="rule.id" class="rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600">
            <div class="flex flex-wrap items-center gap-2.5">
              <span class="chip ring-1" :class="typeMeta[rule.type].chip">{{ typeMeta[rule.type].label }}</span>
              <p class="min-w-0 flex-1 truncate text-sm font-medium text-slate-200">{{ rule.name }}</p>
              <span class="chip shrink-0 bg-ink-900/70 text-slate-400 ring-1 ring-ink-600" :title="scopeNames(rule) || '租户内全部节点'">{{ scopeLabel(rule) }}</span>
              <button class="relative h-5 w-9 shrink-0 rounded-full transition" :class="rule.enabled ? 'bg-emerald-500' : 'bg-ink-600'" :title="rule.enabled ? '点击停用' : '点击启用'" :aria-label="rule.enabled ? '停用规则' : '启用规则'" :disabled="!app.isAdmin" @click="toggleRule(rule)">
                <span class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all" :class="rule.enabled ? 'left-[18px]' : 'left-0.5'"></span>
              </button>
            </div>
            <p class="mt-2 text-[11px] text-slate-500">
              阈值 {{ fmtDuration(rule.threshold_sec) }} · 静默期 {{ fmtDuration(rule.quiet_sec) }} ·
              <span v-if="rule.channel_ids.length">
                通知到 <span class="text-slate-400">{{ rule.channel_ids.map(channelName).join('、') }}</span>
                <span v-if="rule.channel_ids.some(channelMissing)" class="text-amber-400">（含已删除渠道）</span>
              </span>
              <span v-else class="text-amber-400">未绑定通知渠道（仅记录）</span>
            </p>
            <p class="mt-1 text-[11px] text-slate-600">{{ typeMeta[rule.type].description }}</p>
            <div class="mt-2.5 flex flex-wrap gap-2">
              <button v-if="app.isAdmin" class="chip bg-emerald-500/10 text-emerald-300 ring-1 ring-emerald-500/30 hover:bg-emerald-500/20" :disabled="evaluatingId === rule.id" @click="evaluateRule(rule)">
                {{ evaluatingId === rule.id ? '评估中…' : '立即评估' }}
              </button>
              <button class="chip bg-slate-500/10 text-slate-300 ring-1 ring-slate-500/30 hover:bg-ink-700" :disabled="!app.isAdmin" @click="editRule(rule)">编辑</button>
              <button class="chip bg-red-500/10 text-red-300 ring-1 ring-red-500/30 hover:bg-red-500/20" :disabled="!app.isAdmin" @click="removeRule(rule)">删除</button>
            </div>
          </div>
        </div>
      </div>

      <!-- 新建/编辑 + 历史 -->
      <div class="flex min-h-0 flex-col gap-5 overflow-y-auto">
        <div class="panel shrink-0 p-5">
          <div class="flex items-start justify-between gap-3">
            <div>
              <h2 class="text-sm font-semibold text-white">{{ editingId ? '编辑规则' : '新建规则' }}</h2>
              <p class="mt-0.5 text-xs text-slate-500">通知渠道在「系统设置 → 通知配置」中创建。</p>
            </div>
            <button v-if="editingId" class="btn-ghost !py-1.5 text-xs" @click="resetForm">取消编辑</button>
          </div>
          <div class="mt-4 grid grid-cols-1 gap-3.5 sm:grid-cols-2">
            <div class="sm:col-span-2"><label class="label">规则名称</label><input v-model="form.name" class="input" placeholder="如：生产节点离线告警" /></div>
            <div class="sm:col-span-2">
              <label class="label">规则类型</label>
              <select v-model="form.type" class="input" @change="applyRecommended">
                <option value="node_offline">节点离线</option>
                <option value="link_down">链路握手超时</option>
                <option value="config_failed">配置下发失败</option>
              </select>
              <p class="mt-1.5 flex items-center justify-between gap-2 rounded-lg bg-ink-900/60 px-3 py-2 text-[11px] leading-relaxed text-slate-500 ring-1 ring-ink-700">
                {{ typeMeta[form.type].description }}
                <button class="shrink-0 rounded-md px-2 py-1 text-[11px] text-cyan-300 transition hover:bg-cyan-500/10" @click="applyRecommended">填入推荐值</button>
              </p>
            </div>
            <div>
              <label class="label">阈值</label>
              <div class="flex gap-2">
                <input v-model.number="form.thresholdValue" type="number" min="1" class="input" />
                <select v-model="form.thresholdUnit" class="input !w-24 shrink-0">
                  <option v-for="option in unitOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                </select>
              </div>
            </div>
            <div>
              <label class="label">静默期</label>
              <div class="flex gap-2">
                <input v-model.number="form.quietValue" type="number" min="0" class="input" />
                <select v-model="form.quietUnit" class="input !w-24 shrink-0">
                  <option v-for="option in unitOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                </select>
              </div>
            </div>
            <div class="flex items-end pb-2">
              <label class="flex items-center gap-2 text-xs text-slate-400"><input v-model="form.enabled" type="checkbox" class="accent-emerald-500" />启用规则</label>
            </div>
            <div class="sm:col-span-2">
              <label class="label">作用范围</label>
              <div class="flex flex-wrap gap-1.5">
                <button
                  v-for="option in scopeOptions"
                  :key="option.value"
                  class="chip transition"
                  :class="form.scopeType === option.value ? 'bg-cyan-500/15 text-cyan-300 ring-1 ring-cyan-500/40' : 'bg-ink-800 text-slate-400 ring-1 ring-ink-600 hover:text-slate-200'"
                  :title="option.hint"
                  @click="form.scopeType = option.value"
                >
                  {{ option.label }}
                </button>
              </div>
              <div v-if="form.scopeType === 'network'" class="mt-2 flex flex-wrap gap-1.5">
                <button
                  v-for="network in mesh.networks"
                  :key="network.id"
                  class="chip transition"
                  :class="form.scopeIds.includes(network.id) ? 'bg-cyan-500/15 text-cyan-300 ring-1 ring-cyan-500/40' : 'bg-ink-800 text-slate-400 ring-1 ring-ink-600 hover:text-slate-200'"
                  @click="toggleScopeId(network.id)"
                >
                  {{ network.name }}
                </button>
                <p v-if="!mesh.networks.length" class="text-[11px] text-amber-400">暂无网络可选</p>
              </div>
              <div v-else-if="form.scopeType === 'node'" class="mt-2 flex max-h-32 flex-wrap gap-1.5 overflow-y-auto">
                <button
                  v-for="agent in mesh.agents"
                  :key="agent.id"
                  class="chip transition"
                  :class="form.scopeIds.includes(agent.id) ? 'bg-cyan-500/15 text-cyan-300 ring-1 ring-cyan-500/40' : 'bg-ink-800 text-slate-400 ring-1 ring-ink-600 hover:text-slate-200'"
                  @click="toggleScopeId(agent.id)"
                >
                  {{ agent.name }}
                </button>
                <p v-if="!mesh.agents.length" class="text-[11px] text-amber-400">暂无节点可选</p>
              </div>
              <p v-else class="mt-2 text-[11px] text-slate-600">作用于租户内所有启用的节点。</p>
            </div>
            <div class="sm:col-span-2">
              <label class="label">通知渠道</label>
              <div v-if="mesh.notifyChannels.length" class="flex flex-wrap gap-1.5">
                <button
                  v-for="channel in mesh.notifyChannels"
                  :key="channel.id"
                  class="chip transition"
                  :class="form.channel_ids.includes(channel.id) ? 'bg-cyan-500/15 text-cyan-300 ring-1 ring-cyan-500/40' : 'bg-ink-800 text-slate-400 ring-1 ring-ink-600 hover:text-slate-200'"
                  @click="toggleChannel(channel.id)"
                >
                  {{ channel.name }}
                </button>
              </div>
              <p v-else class="text-[11px] text-amber-400">尚未配置通知渠道，规则将只记录不推送。</p>
            </div>
          </div>
          <p v-if="formError" class="mt-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ formError }}</p>
          <div class="mt-4 flex justify-end"><button class="btn-primary" :disabled="!app.isAdmin || saving" @click="saveRule">{{ saving ? '保存中…' : editingId ? '保存修改' : '创建规则' }}</button></div>
        </div>

        <div class="panel min-h-0 flex-1 p-5">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h2 class="text-sm font-semibold text-white">告警历史 <span class="ml-1 text-xs font-normal text-slate-500">{{ filteredEvents.length }}/{{ events.length }} 条</span></h2>
            <div class="flex items-center gap-2">
              <select v-model="eventFilter" class="input !w-28 !py-1.5 !text-xs">
                <option value="all">全部状态</option>
                <option value="sent">已告警</option>
                <option value="failed">推送失败</option>
                <option value="recorded">仅记录</option>
                <option value="recovered">已恢复</option>
              </select>
              <button v-if="app.isAdmin && events.length" class="chip bg-red-500/10 text-red-300 ring-1 ring-red-500/30 hover:bg-red-500/20" :disabled="clearing" @click="clearEvents">
                {{ clearing ? '清空中…' : '清空历史' }}
              </button>
            </div>
          </div>
          <p v-if="eventsError" class="mt-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">告警历史加载失败：{{ eventsError }}</p>
          <div class="mt-3 max-h-96 space-y-2 overflow-y-auto pr-1">
            <div v-for="event in filteredEvents" :key="event.id" class="flex items-start gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
              <span class="chip mt-0.5 shrink-0 ring-1" :class="eventStatusMeta[event.status].chip">{{ eventStatusMeta[event.status].label }}</span>
              <div class="min-w-0 flex-1">
                <p class="text-xs text-slate-200">{{ event.message }}</p>
                <p class="mt-1 text-[11px] text-slate-500">{{ event.rule_name }} · {{ event.node_name }} · {{ fmtDateTime(Date.parse(event.created_at)) }}（{{ ago(Date.parse(event.created_at)) }}）</p>
                <p v-if="event.status === 'failed'" class="mt-0.5 text-[11px] text-slate-600">推送失败可能由渠道配置错误、渠道已删除或服务端不可达引起，可在「系统设置 → 通知配置」的发送记录中查看详情。</p>
              </div>
            </div>
            <p v-if="!events.length" class="py-6 text-center text-xs text-slate-500">暂无告警记录</p>
            <p v-else-if="!filteredEvents.length" class="py-6 text-center text-xs text-slate-500">当前筛选条件下没有记录</p>
            <button v-if="eventsHasMore" class="mt-2 w-full rounded-xl bg-ink-800/60 px-3 py-2 text-center text-xs text-cyan-300 ring-1 ring-ink-600 transition hover:bg-ink-800 disabled:text-slate-600" :disabled="eventsLoadingMore" @click="loadMoreEvents">
              {{ eventsLoadingMore ? '加载中…' : '加载更多（已加载 ' + events.length + ' 条）' }}
            </button>
            <p v-else-if="events.length" class="mt-2 text-center text-[11px] text-slate-600">共 {{ events.length }} 条记录</p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
