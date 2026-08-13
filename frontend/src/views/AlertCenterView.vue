<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { api, type ApiAlertEvent, type ApiAlertRule } from '../api'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import { fmtDateTime } from '../utils/format'

const app = useAppStore()
const mesh = useMeshStore()

const rules = ref<ApiAlertRule[]>([])
const events = ref<ApiAlertEvent[]>([])
const loading = ref(false)
const error = ref('')
const saving = ref(false)
const editingId = ref<string | null>(null)
const formError = ref('')

const typeMeta: Record<ApiAlertRule['type'], { label: string; description: string; chip: string }> = {
  node_offline: { label: '节点离线', description: '节点超过阈值时间未上报心跳', chip: 'bg-red-500/10 text-red-300 ring-red-500/30' },
  link_down: { label: '链路握手超时', description: 'Peer 最近握手时间超过阈值', chip: 'bg-amber-500/10 text-amber-300 ring-amber-500/30' },
  config_failed: { label: '配置下发失败', description: '节点应用配置失败', chip: 'bg-violet-500/10 text-violet-300 ring-violet-500/30' },
}

const form = reactive({
  name: '',
  type: 'node_offline' as ApiAlertRule['type'],
  threshold_sec: 300,
  quiet_sec: 3600,
  channel_ids: [] as string[],
  enabled: true,
})

const selectedChannels = computed(() => form.channel_ids)

function toggleChannel(id: string) {
  const index = form.channel_ids.indexOf(id)
  if (index >= 0) form.channel_ids.splice(index, 1)
  else form.channel_ids.push(id)
}

function resetForm() {
  editingId.value = null
  form.name = ''
  form.type = 'node_offline'
  form.threshold_sec = 300
  form.quiet_sec = 3600
  form.channel_ids = []
  form.enabled = true
  formError.value = ''
}

function editRule(rule: ApiAlertRule) {
  editingId.value = rule.id
  form.name = rule.name
  form.type = rule.type
  form.threshold_sec = rule.threshold_sec
  form.quiet_sec = rule.quiet_sec
  form.channel_ids = [...rule.channel_ids]
  form.enabled = rule.enabled
  formError.value = ''
}

function validateForm() {
  if (!form.name.trim()) return '请输入规则名称'
  if (!Number.isInteger(form.threshold_sec) || form.threshold_sec < 1 || form.threshold_sec > 86400) return '阈值必须在 1-86400 秒之间'
  if (!Number.isInteger(form.quiet_sec) || form.quiet_sec < 0 || form.quiet_sec > 604800) return '静默期必须在 0-604800 秒之间'
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
    threshold_sec: form.threshold_sec,
    quiet_sec: form.quiet_sec,
    channel_ids: form.channel_ids,
    enabled: form.enabled,
  }
  try {
    if (editingId.value) await api.updateAlertRule(editingId.value, payload)
    else await api.createAlertRule(payload)
    resetForm()
    await load()
  } catch (reason) {
    formError.value = reason instanceof Error ? reason.message : '保存规则失败'
  } finally {
    saving.value = false
  }
}

async function removeRule(rule: ApiAlertRule) {
  try {
    await api.deleteAlertRule(rule.id)
    if (editingId.value === rule.id) resetForm()
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '删除规则失败'
  }
}

async function toggleRule(rule: ApiAlertRule) {
  try {
    await api.updateAlertRule(rule.id, {
      name: rule.name, type: rule.type, threshold_sec: rule.threshold_sec,
      quiet_sec: rule.quiet_sec, channel_ids: rule.channel_ids, enabled: !rule.enabled,
    })
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '更新规则失败'
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [ruleResult, eventResult] = await Promise.allSettled([api.alertRules(), api.alertEvents()])
    if (ruleResult.status === 'fulfilled') rules.value = ruleResult.value
    else error.value = ruleResult.reason instanceof Error ? ruleResult.reason.message : '加载规则失败'
    if (eventResult.status === 'fulfilled') events.value = eventResult.value
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="flex h-full flex-col gap-5">
    <p v-if="error" class="rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ error }}</p>

    <div class="grid min-h-0 flex-1 grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1.1fr)_minmax(24rem,0.9fr)]">
      <!-- 规则列表 -->
      <div class="panel min-h-0 overflow-y-auto p-5">
        <div class="mb-3 flex items-center justify-between">
          <div>
            <h2 class="text-sm font-semibold text-white">告警规则</h2>
            <p class="mt-0.5 text-xs text-slate-500">条件满足时向绑定的通知渠道发送告警，并在静默期内对同一节点只告警一次。</p>
          </div>
          <span class="chip bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">{{ rules.length }} 条规则</span>
        </div>
        <p v-if="loading && !rules.length" class="py-8 text-center text-xs text-slate-500">加载中…</p>
        <p v-else-if="!rules.length" class="py-8 text-center text-xs text-slate-500">暂无告警规则，请在右侧创建第一条规则。</p>
        <div v-else class="space-y-2.5">
          <div v-for="rule in rules" :key="rule.id" class="rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600">
            <div class="flex flex-wrap items-center gap-2.5">
              <span class="chip ring-1" :class="typeMeta[rule.type].chip">{{ typeMeta[rule.type].label }}</span>
              <p class="min-w-0 flex-1 truncate text-sm font-medium text-slate-200">{{ rule.name }}</p>
              <button class="relative h-5 w-9 shrink-0 rounded-full transition" :class="rule.enabled ? 'bg-emerald-500' : 'bg-ink-600'" :title="rule.enabled ? '点击停用' : '点击启用'" :disabled="!app.isAdmin" @click="toggleRule(rule)">
                <span class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all" :class="rule.enabled ? 'left-[18px]' : 'left-0.5'"></span>
              </button>
            </div>
            <p class="mt-2 text-[11px] text-slate-500">
              阈值 {{ rule.threshold_sec }}s · 静默期 {{ rule.quiet_sec }}s ·
              <span v-if="rule.channel_ids.length">通知到 {{ rule.channel_ids.length }} 个渠道</span>
              <span v-else class="text-amber-400">未绑定通知渠道（仅记录）</span>
            </p>
            <p class="mt-1 text-[11px] text-slate-600">{{ typeMeta[rule.type].description }}</p>
            <div class="mt-2.5 flex gap-2">
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
            <div>
              <label class="label">规则类型</label>
              <select v-model="form.type" class="input">
                <option value="node_offline">节点离线</option>
                <option value="link_down">链路握手超时</option>
                <option value="config_failed">配置下发失败</option>
              </select>
            </div>
            <div><label class="label">阈值（秒）</label><input v-model.number="form.threshold_sec" type="number" min="1" max="86400" class="input" /></div>
            <div><label class="label">静默期（秒）</label><input v-model.number="form.quiet_sec" type="number" min="0" max="604800" class="input" /></div>
            <div class="flex items-end pb-2">
              <label class="flex items-center gap-2 text-xs text-slate-400"><input v-model="form.enabled" type="checkbox" class="accent-emerald-500" />启用规则</label>
            </div>
            <div class="sm:col-span-2">
              <label class="label">通知渠道</label>
              <div v-if="mesh.notifyChannels.length" class="flex flex-wrap gap-1.5">
                <button
                  v-for="channel in mesh.notifyChannels"
                  :key="channel.id"
                  class="chip transition"
                  :class="selectedChannels.includes(channel.id) ? 'bg-cyan-500/15 text-cyan-300 ring-1 ring-cyan-500/40' : 'bg-ink-800 text-slate-400 ring-1 ring-ink-600 hover:text-slate-200'"
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
          <h2 class="text-sm font-semibold text-white">告警历史</h2>
          <div class="mt-3 max-h-96 space-y-2 overflow-y-auto pr-1">
            <div v-for="event in events" :key="event.id" class="flex items-start gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
              <span class="chip mt-0.5 shrink-0 ring-1" :class="event.status === 'failed' ? 'bg-red-500/10 text-red-300 ring-red-500/30' : 'bg-amber-500/10 text-amber-300 ring-amber-500/30'">{{ event.status === 'failed' ? '推送失败' : '已告警' }}</span>
              <div class="min-w-0 flex-1">
                <p class="text-xs text-slate-200">{{ event.message }}</p>
                <p class="mt-1 text-[11px] text-slate-500">{{ event.rule_name }} · {{ event.node_name }} · {{ fmtDateTime(Date.parse(event.created_at)) }}</p>
              </div>
            </div>
            <p v-if="!events.length" class="py-6 text-center text-xs text-slate-500">暂无告警记录</p>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
