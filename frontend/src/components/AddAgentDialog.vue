<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { apiBase } from '../api'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'

const emit = defineEmits<{ (e: 'close'): void }>()
const app = useAppStore()
const mesh = useMeshStore()

const tab = ref<'script' | 'manual'>('script')
const copied = ref<'script' | 'manual' | ''>('')
const issuing = ref(false)
const enrollmentToken = ref('')
const expiresAt = ref('')
const error = ref('')
const form = reactive({
  projectId: '',
  networkId: '',
  name: '',
  labels: app.settings.agent.labels,
  ttlMinutes: 30,
})

const networks = computed(() => mesh.networks.filter((network) => network.projectId === form.projectId))
const serverUrl = computed(() => (apiBase || location.origin).replace(/\/$/, ''))

function shellQuote(value: string) {
  return `'${value.replace(/'/g, `'"'"'`)}'`
}

const installScript = computed(() => enrollmentToken.value
  ? `# WireMesh Agent 一键安装（常驻 systemd 服务）
# 在目标 Linux 主机上以具备 sudo 权限的用户执行
curl -fsSL ${serverUrl.value}/agent/install.sh | sudo bash -s -- \\
  --server ${shellQuote(serverUrl.value)} \\
  --token ${shellQuote(enrollmentToken.value)} \\
  --project ${shellQuote(form.projectId)} \\
  --network ${shellQuote(form.networkId)} \\
  --name ${shellQuote(form.name.trim() || '<节点名称>')} \\
  --labels ${shellQuote(form.labels.trim())} \\
  --interfaces auto \\
  --report-interval ${app.settings.collect.reportSec}s \\
  --probe-interval ${app.settings.collect.probeSec}s \\
  --mtls`
  : '# 请先填写节点信息，然后生成一次性接入命令')

const manualCommand = computed(() => enrollmentToken.value
  ? `# 手动下载 Linux amd64 Agent（arm64 主机请将 arch 改为 arm64）
curl -fL ${shellQuote(serverUrl.value + '/agent/download?os=linux&arch=amd64')} -o wiremesh-agent
chmod +x wiremesh-agent
sudo ./wiremesh-agent \\
  --server ${shellQuote(serverUrl.value)} \\
  --enroll-token ${shellQuote(enrollmentToken.value)} \\
  --name ${shellQuote(form.name.trim() || '<节点名称>')} \\
  --labels ${shellQuote(form.labels.trim())} \\
  --report-interval ${app.settings.collect.reportSec}s \\
  --probe-interval ${app.settings.collect.probeSec}s \\
  --mtls`
  : '# 请先填写节点信息，然后生成一次性接入命令')

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
  if (!form.name.trim()) {
    error.value = '请输入节点名称'
    return
  }
  issuing.value = true
  error.value = ''
  enrollmentToken.value = ''
  expiresAt.value = ''
  try {
    const result = await mesh.createEnrollment(form.projectId, form.networkId, form.ttlMinutes)
    enrollmentToken.value = result.token
    expiresAt.value = result.expires_at
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '接入命令生成失败'
  } finally {
    issuing.value = false
  }
}

async function copy(kind: 'script' | 'manual') {
  if (!enrollmentToken.value) return
  const value = kind === 'script' ? installScript.value : manualCommand.value
  try {
    await navigator.clipboard.writeText(value)
  } catch {
    const textarea = document.createElement('textarea')
    textarea.value = value
    document.body.appendChild(textarea)
    textarea.select()
    document.execCommand('copy')
    textarea.remove()
  }
  copied.value = kind
  window.setTimeout(() => (copied.value = ''), 1600)
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-ink-950/70 p-4 backdrop-blur-sm" @click.self="emit('close')">
    <div class="panel flex max-h-[88vh] w-full max-w-2xl flex-col overflow-hidden">
      <div class="flex items-center justify-between border-b border-ink-700 px-6 py-4">
        <div>
          <h2 class="text-base font-semibold text-white">接入新节点（Agent）</h2>
          <p class="mt-0.5 text-xs text-slate-500">一个 Agent 管理一台主机，安装后作为系统服务持续连接 WireMesh</p>
        </div>
        <button class="rounded-lg p-1.5 text-slate-500 hover:bg-ink-800 hover:text-slate-300" @click="emit('close')">
          <svg viewBox="0 0 24 24" fill="none" class="h-5 w-5" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
        </button>
      </div>

      <div class="flex gap-1 border-b border-ink-700 px-6 pt-3">
        <button
          v-for="item in [{ key: 'script', label: '一键脚本（推荐）' }, { key: 'manual', label: '手动安装' }]"
          :key="item.key"
          class="rounded-t-lg px-4 py-2 text-sm font-medium transition"
          :class="tab === item.key ? 'bg-ink-800 text-emerald-300 ring-1 ring-inset ring-ink-600' : 'text-slate-500 hover:text-slate-300'"
          @click="tab = item.key as 'script' | 'manual'"
        >
          {{ item.label }}
        </button>
      </div>

      <div class="min-h-0 flex-1 overflow-y-auto p-6">
        <div v-if="!mesh.projects.length || !mesh.networks.length" class="rounded-xl bg-amber-500/10 px-4 py-6 text-center ring-1 ring-amber-500/30">
          <p class="text-sm text-amber-300">还没有可用的{{ mesh.projects.length ? '网络' : '项目' }}</p>
          <p class="mt-1 text-xs text-slate-500">请先到「系统设置 → 项目与网络」中创建{{ mesh.projects.length ? '网络' : '项目与网络' }}，再接入节点</p>
        </div>

        <template v-else>
          <div class="mb-4 grid gap-3 sm:grid-cols-3">
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
              <label class="label">节点名称 *</label>
              <input v-model="form.name" class="input" placeholder="如 tokyo-edge-02" />
            </div>
          </div>

          <div class="mb-4 grid gap-3 sm:grid-cols-[minmax(0,1fr)_10rem]">
            <div>
              <label class="label">节点标签</label>
              <input v-model="form.labels" class="input" placeholder="如 env=prod,region=cn-east" />
            </div>
            <div>
              <label class="label">命令有效期</label>
              <select v-model.number="form.ttlMinutes" class="input">
                <option :value="10">10 分钟</option>
                <option :value="30">30 分钟</option>
                <option :value="60">1 小时</option>
                <option :value="1440">24 小时</option>
              </select>
            </div>
          </div>

          <div class="mb-4 flex flex-col gap-3 rounded-xl bg-ink-800/60 p-3 ring-1 ring-ink-700 sm:flex-row sm:items-center sm:justify-between">
            <p class="text-xs leading-relaxed text-slate-500">项目和网络会写入后端签发的一次性令牌；令牌成功使用后立即失效。</p>
            <button class="btn-primary shrink-0" :disabled="issuing || !form.networkId || !form.name.trim()" @click="issueToken">
              {{ issuing ? '生成中…' : enrollmentToken ? '重新生成接入命令' : '生成接入命令' }}
            </button>
          </div>

          <p v-if="error" class="mb-4 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ error }}</p>
          <div v-if="enrollmentToken" class="mb-4 rounded-xl bg-emerald-500/10 p-3 text-xs text-emerald-300 ring-1 ring-emerald-500/30">
            接入命令已生成<span v-if="expiresAt">，有效至 {{ new Date(expiresAt).toLocaleString() }}</span>。令牌仅在当前窗口显示。
          </div>

          <div v-if="tab === 'script'">
            <div class="relative">
              <pre class="overflow-x-auto rounded-xl bg-ink-950 p-4 pr-24 font-mono text-xs leading-relaxed text-emerald-200/90 ring-1 ring-ink-600">{{ installScript }}</pre>
              <button v-if="enrollmentToken" class="btn-ghost absolute right-3 top-3 !px-3 !py-1.5 text-xs" @click="copy('script')">
                {{ copied === 'script' ? '已复制' : '复制' }}
              </button>
            </div>
            <ul class="mt-4 space-y-1.5 text-[11px] leading-relaxed text-slate-500">
              <li>· 在目标 Linux 主机执行后，安装器会从当前 WireMesh 服务下载匹配架构的 Agent。</li>
              <li>· Agent 将注册为 systemd 常驻服务；注册令牌只使用一次，不会保存在浏览器或服务配置中。</li>
              <li>· HTTPS 部署会使用注册时签发的客户端证书建立 mTLS 连接；HTTP 仅适用于开发环境。</li>
              <li>· 节点成功注册并首次上报后，节点列表会显示来自后端的真实节点记录。</li>
            </ul>
          </div>

          <div v-else>
            <div class="relative">
              <pre class="overflow-x-auto rounded-xl bg-ink-950 p-4 pr-24 font-mono text-xs leading-relaxed text-sky-200/90 ring-1 ring-ink-600">{{ manualCommand }}</pre>
              <button v-if="enrollmentToken" class="btn-ghost absolute right-3 top-3 !px-3 !py-1.5 text-xs" @click="copy('manual')">
                {{ copied === 'manual' ? '已复制' : '复制' }}
              </button>
            </div>
            <div class="mt-4 rounded-xl bg-amber-500/10 px-4 py-3 text-[11px] leading-relaxed text-amber-200/80 ring-1 ring-amber-500/20">
              手动方式不会自动创建 systemd 服务，适合调试或自定义进程管理。生产环境建议使用一键脚本。
            </div>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>
