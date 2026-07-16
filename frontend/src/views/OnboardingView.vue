<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '../stores/app'

const router = useRouter()
const app = useAppStore()

const step = ref(0)
const steps = ['欢迎', '管理员账号', '确认配置', '完成']
const form = reactive({
  name: '',
  email: '',
  password: '',
  confirm: '',
})
const errors = reactive<Record<string, string>>({})

const canNext = computed(() => {
  if (app.loading) return false
  if (step.value === 1) {
    return form.name.trim().length >= 2
      && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email.trim())
      && form.password.length >= 8
      && form.password === form.confirm
  }
  return true
})

function validateAdmin() {
  Object.keys(errors).forEach((key) => delete errors[key])
  if (form.name.trim().length < 2) errors.name = '管理员名称至少 2 个字符'
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email.trim())) errors.email = '请输入有效的邮箱地址'
  if (form.password.length < 8) errors.password = '密码至少 8 位'
  if (form.password !== form.confirm) errors.confirm = '两次输入的密码不一致'
  return Object.keys(errors).length === 0
}

async function next() {
  if (step.value === 1 && !validateAdmin()) return
  if (step.value === 2) {
    const created = await app.setup({
      email: form.email.trim().toLowerCase(),
      name: form.name.trim(),
      password: form.password,
    })
    if (!created) return
    form.password = ''
    form.confirm = ''
  }
  if (step.value < steps.length - 1) step.value++
}

function finish() {
  router.replace({ name: 'login' })
}
</script>

<template>
  <div class="relative flex h-full items-center justify-center overflow-hidden bg-ink-950 p-6">
    <div class="pointer-events-none absolute -top-40 left-1/2 h-96 w-[42rem] -translate-x-1/2 rounded-full bg-emerald-500/10 blur-3xl"></div>
    <div class="pointer-events-none absolute bottom-0 right-0 h-72 w-72 rounded-full bg-cyan-500/10 blur-3xl"></div>

    <div class="relative w-full max-w-xl">
      <div class="mb-8 flex items-center justify-center gap-0">
        <template v-for="(label, index) in steps" :key="label">
          <div class="flex items-center">
            <div
              class="flex h-9 w-9 items-center justify-center rounded-full text-xs font-bold transition-all"
              :class="index < step ? 'bg-emerald-500 text-ink-950' : index === step ? 'bg-emerald-500/20 text-emerald-300 ring-2 ring-emerald-400' : 'bg-ink-800 text-slate-500 ring-1 ring-ink-600'"
            >
              <svg v-if="index < step" viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" /></svg>
              <span v-else>{{ index + 1 }}</span>
            </div>
            <span class="ml-2 mr-4 hidden text-xs font-medium sm:block" :class="index <= step ? 'text-slate-200' : 'text-slate-600'">{{ label }}</span>
          </div>
          <div v-if="index < steps.length - 1" class="mx-1 h-px w-8 sm:w-12" :class="index < step ? 'bg-emerald-500' : 'bg-ink-600'"></div>
        </template>
      </div>

      <div class="panel p-8">
        <div v-if="step === 0" class="text-center">
          <div class="mx-auto mb-6 flex h-16 w-16 items-center justify-center rounded-2xl bg-emerald-500/15 ring-1 ring-emerald-500/40 shadow-glow">
            <svg viewBox="0 0 24 24" fill="none" class="h-8 w-8 text-emerald-400" stroke="currentColor" stroke-width="1.6">
              <path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z" />
            </svg>
          </div>
          <h2 class="text-2xl font-bold text-white">欢迎使用 WireMesh 控制台</h2>
          <p class="mx-auto mt-3 max-w-md text-sm leading-relaxed text-slate-400">
            当前数据库尚未初始化。请通过此引导创建唯一的初始管理员，完成后初始化入口将自动关闭。
          </p>
          <div class="mt-6 grid grid-cols-3 gap-3 text-left">
            <div class="rounded-xl bg-ink-800/70 p-3.5 ring-1 ring-ink-600">
              <p class="text-sm font-semibold text-emerald-300">全球拓扑</p>
              <p class="mt-1 text-xs leading-relaxed text-slate-500">按地理位置总览 Agent 与隧道链路</p>
            </div>
            <div class="rounded-xl bg-ink-800/70 p-3.5 ring-1 ring-ink-600">
              <p class="text-sm font-semibold text-cyan-300">多接口管理</p>
              <p class="mt-1 text-xs leading-relaxed text-slate-500">一个 Agent 管理多个 WireGuard 接口</p>
            </div>
            <div class="rounded-xl bg-ink-800/70 p-3.5 ring-1 ring-ink-600">
              <p class="text-sm font-semibold text-violet-300">安全初始化</p>
              <p class="mt-1 text-xs leading-relaxed text-slate-500">仅空数据库允许创建初始管理员</p>
            </div>
          </div>
        </div>

        <div v-else-if="step === 1">
          <h2 class="text-xl font-bold text-white">创建管理员账号</h2>
          <p class="mt-1 text-sm text-slate-500">该账号将写入 WireMesh 数据库，并拥有管理员权限。</p>
          <div class="mt-6 space-y-4">
            <div>
              <label class="label">管理员名称</label>
              <input v-model="form.name" class="input" placeholder="例如：网络管理员" autocomplete="name" />
              <p v-if="errors.name" class="mt-1 text-xs text-red-400">{{ errors.name }}</p>
            </div>
            <div>
              <label class="label">登录邮箱</label>
              <input v-model="form.email" type="email" class="input" placeholder="admin@example.com" autocomplete="username" />
              <p v-if="errors.email" class="mt-1 text-xs text-red-400">{{ errors.email }}</p>
            </div>
            <div>
              <label class="label">密码</label>
              <input v-model="form.password" type="password" class="input" placeholder="至少 8 位" autocomplete="new-password" />
              <p v-if="errors.password" class="mt-1 text-xs text-red-400">{{ errors.password }}</p>
            </div>
            <div>
              <label class="label">确认密码</label>
              <input v-model="form.confirm" type="password" class="input" placeholder="再次输入密码" autocomplete="new-password" />
              <p v-if="errors.confirm" class="mt-1 text-xs text-red-400">{{ errors.confirm }}</p>
            </div>
          </div>
        </div>

        <div v-else-if="step === 2">
          <h2 class="text-xl font-bold text-white">确认初始化</h2>
          <p class="mt-1 text-sm text-slate-500">提交后将创建初始租户和管理员账号，此操作只能成功执行一次。</p>
          <dl class="mt-6 space-y-3 rounded-xl bg-ink-800/70 p-5 ring-1 ring-ink-600">
            <div class="flex items-center justify-between gap-4 text-sm"><dt class="text-slate-500">管理员名称</dt><dd class="truncate font-medium text-slate-200">{{ form.name }}</dd></div>
            <div class="flex items-center justify-between gap-4 text-sm"><dt class="text-slate-500">登录邮箱</dt><dd class="truncate font-mono text-slate-200">{{ form.email.toLowerCase() }}</dd></div>
            <div class="flex items-center justify-between gap-4 text-sm"><dt class="text-slate-500">账号角色</dt><dd class="text-emerald-300">Administrator</dd></div>
            <div class="flex items-center justify-between gap-4 text-sm"><dt class="text-slate-500">初始化保护</dt><dd class="text-slate-200">数据库非空后自动关闭</dd></div>
          </dl>
          <p class="mt-4 text-xs leading-relaxed text-amber-300/80">请确认邮箱正确。WireMesh 不会保存明文密码，也不会生成默认管理员密码。</p>
        </div>

        <div v-else class="text-center">
          <div class="mx-auto mb-6 flex h-16 w-16 items-center justify-center rounded-full bg-emerald-500/15 ring-1 ring-emerald-500/40 shadow-glow">
            <svg viewBox="0 0 24 24" fill="none" class="h-8 w-8 text-emerald-400" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" /></svg>
          </div>
          <h2 class="text-2xl font-bold text-white">初始化完成</h2>
          <p class="mt-3 text-sm text-slate-400">初始管理员已写入数据库。现在可以使用刚刚配置的邮箱和密码登录控制台。</p>
          <dl class="mx-auto mt-6 max-w-sm space-y-2 rounded-xl bg-ink-800/70 p-4 text-left ring-1 ring-ink-600">
            <div class="flex justify-between gap-4 text-sm"><dt class="text-slate-500">管理员</dt><dd class="truncate font-medium text-slate-200">{{ form.name }}</dd></div>
            <div class="flex justify-between gap-4 text-sm"><dt class="text-slate-500">登录邮箱</dt><dd class="truncate font-mono text-slate-200">{{ form.email.toLowerCase() }}</dd></div>
          </dl>
        </div>

        <p v-if="app.error && step === 2" class="mt-5 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-400 ring-1 ring-red-500/30">{{ app.error }}</p>

        <div class="mt-8 flex items-center justify-between">
          <button v-if="step > 0 && step < steps.length - 1" class="btn-ghost" :disabled="app.loading" @click="step--">上一步</button>
          <span v-else></span>
          <button v-if="step < steps.length - 1" class="btn-primary" :disabled="!canNext" @click="next">
            <svg v-if="app.loading" class="h-4 w-4 animate-spin" viewBox="0 0 24 24" fill="none"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/><path class="opacity-90" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"/></svg>
            {{ app.loading ? '正在初始化…' : step === 0 ? '开始配置' : step === 2 ? '创建初始管理员' : '下一步' }}
            <svg v-if="!app.loading" viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5L21 12l-7.5 7.5M21 12H3" /></svg>
          </button>
          <button v-else class="btn-primary" @click="finish">进入登录</button>
        </div>
      </div>
    </div>
  </div>
</template>
