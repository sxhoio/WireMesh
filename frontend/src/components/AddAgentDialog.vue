<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { apiBase } from '../api'
import { useMeshStore } from '../stores/mesh'

const emit = defineEmits<{ (e: 'close'): void }>()
const mesh = useMeshStore()

const copied = ref(false)
const issuing = ref(false)
const enrollmentToken = ref('')
const expiresAt = ref('')
const error = ref('')
const form = reactive({ projectId: '', networkId: '', name: '', ttlMinutes: 30 })

const networks = computed(() => mesh.networks.filter((network) => network.projectId === form.projectId))
const serverUrl = computed(() => apiBase || location.origin)
const command = computed(() => enrollmentToken.value
  ? `go run ./cmd/wiremesh-agent -server ${serverUrl.value} -enroll-token ${enrollmentToken.value} -name ${form.name.trim() || '<节点名称>'}`
  : '# 请先选择项目和网络，然后生成一次性注册令牌')

watch(
  () => mesh.projects,
  (projects) => {
    if (!projects.some((project) => project.id === form.projectId)) form.projectId = projects[0]?.id || ''
  },
  { immediate: true },
)
watch(
  [() => form.projectId, networks],
  () => {
    if (!networks.value.some((network) => network.id === form.networkId)) form.networkId = networks.value[0]?.id || ''
    enrollmentToken.value = ''
    expiresAt.value = ''
  },
  { immediate: true },
)

async function issueToken() {
  if (!form.projectId || !form.networkId) {
    error.value = '请先选择项目和网络'
    return
  }
  issuing.value = true
  error.value = ''
  enrollmentToken.value = ''
  try {
    const result = await mesh.createEnrollment(form.projectId, form.networkId, form.ttlMinutes)
    enrollmentToken.value = result.token
    expiresAt.value = result.expires_at
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '注册令牌生成失败'
  } finally {
    issuing.value = false
  }
}

async function copy() {
  if (!enrollmentToken.value) return
  await navigator.clipboard.writeText(command.value)
  copied.value = true
  window.setTimeout(() => (copied.value = false), 1600)
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-ink-950/70 p-4 backdrop-blur-sm" @click.self="emit('close')">
    <div class="panel flex max-h-[88vh] w-full max-w-2xl flex-col overflow-hidden">
      <div class="flex items-center justify-between border-b border-ink-700 px-6 py-4">
        <div>
          <h2 class="text-base font-semibold text-white">接入新节点（Agent）</h2>
          <p class="mt-0.5 text-xs text-slate-500">签发后端一次性注册令牌，并在目标主机启动 WireMesh Agent</p>
        </div>
        <button class="rounded-lg p-1.5 text-slate-500 hover:bg-ink-800 hover:text-slate-300" @click="emit('close')">
          <svg viewBox="0 0 24 24" fill="none" class="h-5 w-5" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>

      <div class="min-h-0 flex-1 overflow-y-auto p-6">
        <div v-if="!mesh.projects.length" class="rounded-xl bg-amber-500/10 p-4 text-sm text-amber-300 ring-1 ring-amber-500/30">
          后端当前没有可用项目。请先通过 WireMesh API 创建项目和网络，再签发注册令牌。
        </div>
        <template v-else>
          <div class="grid gap-3 sm:grid-cols-2">
            <div>
              <label class="label">项目</label>
              <select v-model="form.projectId" class="input">
                <option v-for="project in mesh.projects" :key="project.id" :value="project.id">{{ project.name }}</option>
              </select>
            </div>
            <div>
              <label class="label">网络</label>
              <select v-model="form.networkId" class="input">
                <option v-for="network in networks" :key="network.id" :value="network.id">{{ network.name }}</option>
              </select>
            </div>
            <div>
              <label class="label">节点名称</label>
              <input v-model.trim="form.name" class="input" placeholder="如 edge-01" />
            </div>
            <div>
              <label class="label">令牌有效期（分钟）</label>
              <input v-model.number="form.ttlMinutes" type="number" min="1" max="1440" class="input" />
            </div>
          </div>

          <div class="mt-5 flex items-center justify-between gap-3">
            <p class="text-xs text-slate-500">令牌由后端签发且仅用于真实 Agent 注册；页面不会预先创建节点。</p>
            <button class="btn-primary shrink-0" :disabled="issuing || !form.networkId" @click="issueToken">
              {{ issuing ? '生成中…' : '生成一次性注册令牌' }}
            </button>
          </div>

          <p v-if="error" class="mt-4 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ error }}</p>

          <div v-if="enrollmentToken" class="mt-5 space-y-3">
            <div class="rounded-xl bg-emerald-500/10 p-3 text-xs text-emerald-300 ring-1 ring-emerald-500/30">
              注册令牌已签发<span v-if="expiresAt">，过期时间：{{ new Date(expiresAt).toLocaleString() }}</span>。令牌只会显示在当前窗口，请妥善保存。
            </div>
            <div class="relative">
              <pre class="overflow-x-auto rounded-xl bg-ink-950 p-4 pr-24 font-mono text-xs leading-relaxed text-emerald-200/90 ring-1 ring-ink-600">{{ command }}</pre>
              <button class="btn-ghost absolute right-3 top-3 !px-3 !py-1.5 text-xs" @click="copy">{{ copied ? '已复制' : '复制命令' }}</button>
            </div>
          </div>

          <ul class="mt-5 space-y-1.5 text-[11px] leading-relaxed text-slate-500">
            <li>· 命令需在 WireMesh 仓库根目录执行，或使用你部署的 Agent 二进制传入相同参数。</li>
            <li>· Agent 完成真实注册并开始上报后，节点才会出现在节点列表。</li>
            <li>· 私钥不会作为伪造数据保存在浏览器中。</li>
          </ul>
        </template>
      </div>
    </div>
  </div>
</template>
