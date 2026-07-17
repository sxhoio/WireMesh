<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api, type ApiNodeLog } from '../api'
import type { Agent } from '../types'

const props = defineProps<{ agent: Agent }>()
const emit = defineEmits<{ close: [] }>()
const logs = ref<ApiNodeLog[]>([])
const loading = ref(false)
const error = ref('')
async function load() { loading.value = true; error.value = ''; try { logs.value = await api.nodeLogs(props.agent.id) } catch (reason) { error.value = reason instanceof Error ? reason.message : '加载日志失败' } finally { loading.value = false } }
onMounted(load)
function time(value: string) { return value ? new Date(value).toLocaleString('zh-CN') : '-' }
</script>
<template>
  <div class="fixed inset-0 z-[80] flex items-center justify-center bg-black/65 p-4" @click.self="emit('close')">
    <div class="panel flex max-h-[80vh] w-full max-w-3xl flex-col overflow-hidden">
      <div class="flex items-center justify-between border-b border-ink-700 px-6 py-5"><div><h3 class="text-base font-semibold text-white">{{ agent.name }} · Agent 日志</h3><p class="mt-1 text-xs text-slate-500">配置下发、立即采集和连通性检测结果</p></div><div class="flex gap-2"><button class="btn-secondary" :disabled="loading" @click="load">刷新</button><button class="text-slate-500 hover:text-white" @click="emit('close')">✕</button></div></div>
      <div class="min-h-0 flex-1 overflow-auto p-5">
        <p v-if="error" class="mb-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300">{{ error }}</p>
        <p v-if="loading && !logs.length" class="py-10 text-center text-sm text-slate-500">加载中…</p>
        <p v-else-if="!logs.length" class="py-10 text-center text-sm text-slate-500">暂无 Agent 日志</p>
        <div v-else class="space-y-2"><div v-for="log in logs" :key="log.id" class="rounded-xl border border-ink-700 bg-ink-900/60 px-4 py-3"><div class="flex items-center gap-2 text-[11px]"><span class="chip" :class="log.level === 'error' ? 'bg-red-500/10 text-red-300' : log.level === 'warning' ? 'bg-amber-500/10 text-amber-300' : 'bg-cyan-500/10 text-cyan-300'">{{ log.level }}</span><span class="text-slate-500">{{ log.source }}</span><span class="ml-auto text-slate-600">{{ time(log.created_at) }}</span></div><p class="mt-2 whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-slate-300">{{ log.message }}</p></div></div>
      </div>
    </div>
  </div>
</template>
