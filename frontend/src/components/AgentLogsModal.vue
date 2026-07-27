<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, type ApiNodeLog } from '../api'
import type { Agent } from '../types'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'

const props = defineProps<{ agent: Agent }>()
const emit = defineEmits<{ close: [] }>()
const app = useAppStore()
const mesh = useMeshStore()
const logs = ref<ApiNodeLog[]>([])
const currentError = ref('')
const loading = ref(false)
const clearing = ref(false)
const error = ref('')
const onlyErrors = ref(false)
const hasMore = ref(false)
const offset = ref(0)
const pageSize = 50

function logLevelClass(level: string) {
  const value = level.toLowerCase()
  if (value === 'error' || value === 'fatal') return 'text-red-300'
  if (value === 'warning' || value === 'warn') return 'text-amber-300'
  if (value === 'debug') return 'text-violet-300'
  return 'text-cyan-300'
}

function logLevelLabel(level: string) {
  return (level || 'info').toUpperCase().padEnd(5, ' ')
}

function time(value: string) { return value ? new Date(value).toLocaleString('zh-CN') : '-' }

const terminalLogs = computed(() => logs.value.map((log) => ({
  id: log.id,
  level: logLevelLabel(log.level),
  levelClass: logLevelClass(log.level),
  source: log.source || 'agent',
  time: time(log.created_at),
  message: log.message || '',
})))

async function load(reset = true) {
  if (loading.value) return
  loading.value = true
  error.value = ''
  if (reset) offset.value = 0
  try {
    const page = await api.nodeLogs(props.agent.id, pageSize, offset.value, onlyErrors.value)
    logs.value = reset ? page.items : [...logs.value, ...page.items]
    currentError.value = page.current_error || ''
    offset.value += page.items.length
    hasMore.value = page.has_more
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载日志失败'
  } finally {
    loading.value = false
  }
}

async function clearLogs() {
  if (clearing.value || !window.confirm(`确定清空“${props.agent.name}”的 Agent 日志吗？`)) return
  clearing.value = true
  try {
    if (await mesh.clearAgentLogs(props.agent.id)) {
      logs.value = []
      offset.value = 0
      hasMore.value = false
    }
  } finally {
    clearing.value = false
  }
}

function toggleErrors() {
  onlyErrors.value = !onlyErrors.value
  void load(true)
}

onMounted(load)
</script>
<template>
  <div class="fixed inset-0 z-[80] flex items-center justify-center bg-black/65 p-4" @click.self="emit('close')">
    <div class="panel flex max-h-[80vh] w-full max-w-3xl flex-col overflow-hidden">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-ink-700 px-6 py-5">
        <div><h3 class="text-base font-semibold text-white">{{ agent.name }} · Agent 日志</h3><p class="mt-1 text-xs text-slate-500">仅按页加载最近命令记录，服务端自动限制日志数量</p></div>
        <div class="flex items-center gap-2">
          <button class="btn-secondary" :class="onlyErrors ? 'text-red-300 ring-1 ring-red-500/30' : ''" :disabled="loading" @click="toggleErrors">{{ onlyErrors ? '显示全部' : '仅看异常' }}</button>
          <button v-if="app.canOperate" class="btn-secondary text-red-300" :disabled="clearing" @click="clearLogs">{{ clearing ? '清空中…' : '清空日志' }}</button>
          <button class="btn-secondary" :disabled="loading" @click="load(true)">刷新</button>
          <button class="text-slate-500 hover:text-white" @click="emit('close')">✕</button>
        </div>
      </div>
      <div class="min-h-0 flex-1 overflow-auto p-5">
        <p v-if="error" class="mb-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300">{{ error }}</p>
        <p v-if="currentError" class="mb-3 rounded-lg bg-amber-500/10 px-3 py-2 text-xs leading-relaxed text-amber-300 ring-1 ring-amber-500/30">当前 WireGuard 采集异常：{{ currentError }}</p>
        <p v-if="loading && !logs.length" class="py-10 text-center text-sm text-slate-500">加载中…</p>
        <p v-else-if="!logs.length" class="py-10 text-center text-sm text-slate-500">暂无 Agent 日志</p>
        <div v-else class="border-y border-ink-700 bg-[#05070a] font-mono text-[11px] leading-5 text-slate-400">
          <div class="sticky top-0 z-10 flex min-w-[48rem] border-b border-ink-800 bg-[#05070a] px-3 py-1 text-[10px] uppercase tracking-wide text-slate-600">
            <span class="w-40 shrink-0">time</span>
            <span class="w-16 shrink-0">level</span>
            <span class="w-24 shrink-0">source</span>
            <span class="min-w-0 flex-1">message</span>
          </div>
          <div class="max-h-[52vh] overflow-auto">
            <div
              v-for="log in terminalLogs"
              :key="log.id"
              class="flex min-w-[48rem] items-start border-b border-white/[0.025] px-3 py-0.5 last:border-b-0 hover:bg-white/[0.035]"
            >
              <span class="w-40 shrink-0 select-none text-slate-600">{{ log.time }}</span>
              <span class="w-16 shrink-0 select-none whitespace-pre" :class="log.levelClass">{{ log.level }}</span>
              <span class="w-24 shrink-0 select-none truncate text-slate-500">{{ log.source }}</span>
              <span class="min-w-0 flex-1 whitespace-pre-wrap break-all text-slate-300">{{ log.message }}</span>
            </div>
            <button v-if="hasMore" class="block w-full border-t border-ink-800 px-3 py-2 text-center text-xs text-cyan-300 hover:bg-cyan-500/10 disabled:text-slate-600" :disabled="loading" @click="load(false)">{{ loading ? '加载中…' : '加载更多' }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
