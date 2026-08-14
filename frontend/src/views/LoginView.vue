<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { useAppStore } from '../stores/app'

const router = useRouter()
const app = useAppStore()

const form = reactive({ username: '', password: '', otp: '' })
const error = ref('')
const loading = ref(false)
const showPwd = ref(false)
const needsOtp = ref(false)

async function submit() {
  if (!form.username || !form.password) {
    error.value = '请输入邮箱和密码'
    return
  }
  loading.value = true
  error.value = ''
  if (await app.login(form.username, form.password, needsOtp.value ? form.otp : undefined)) {
    router.push({ name: 'home' })
  } else {
    if (app.error?.includes('otp_required')) {
      needsOtp.value = true
      error.value = '该账号已启用多因素认证，请输入动态验证码'
    } else if (app.error?.includes('otp_invalid')) {
      needsOtp.value = true
      error.value = '动态验证码错误，请重试'
    } else {
      error.value = app.error || '邮箱或密码错误'
    }
    loading.value = false
  }
}

async function ssoLogin() {
  try {
    const result = await api.ssoLogin()
    if (result.url) {
      navigateToSSO(result.url)
    } else if (result.tenants?.length) {
      const first = await api.ssoLogin(result.tenants[0])
      if (first.url) navigateToSSO(first.url)
      else error.value = '单点登录暂不可用'
    } else {
      error.value = '单点登录尚未配置'
    }
  } catch {
    error.value = '单点登录暂不可用'
  }
}

// S7：SSO 授权地址仅允许 http/https，防后端配置或发现文档异常时
// location.href 被赋值为 javascript: 等可执行协议。
function navigateToSSO(url: string) {
  const parsed = new URL(url, window.location.href)
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    error.value = '单点登录地址无效'
    return
  }
  window.location.href = parsed.href
}
</script>

<template>
  <div class="relative flex h-full items-center justify-center overflow-hidden bg-ink-950 p-6">
    <div class="pointer-events-none absolute -top-40 left-1/2 h-96 w-[42rem] -translate-x-1/2 rounded-full bg-emerald-500/10 blur-3xl"></div>
    <div class="pointer-events-none absolute bottom-0 left-0 h-72 w-72 rounded-full bg-cyan-500/10 blur-3xl"></div>

    <div class="relative w-full max-w-sm">
      <div class="mb-8 text-center">
        <div class="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-emerald-500/15 ring-1 ring-emerald-500/40 shadow-glow">
          <svg viewBox="0 0 24 24" fill="none" class="h-7 w-7 text-emerald-400" stroke="currentColor" stroke-width="1.6">
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z" />
          </svg>
        </div>
        <h1 class="text-2xl font-bold text-white">{{ app.settings.dashboardName }}</h1>
        <p class="mt-1 text-sm text-slate-500">登录以管理你的 WireGuard 多接口网络</p>
      </div>

      <form class="panel space-y-4 p-7" @submit.prevent="submit">
        <div>
          <label class="label">邮箱</label>
          <input v-model.trim="form.username" class="input" placeholder="请输入邮箱" autocomplete="username" />
        </div>
        <div>
          <label class="label">密码</label>
          <div class="relative">
            <input v-model="form.password" :type="showPwd ? 'text' : 'password'" class="input pr-10" placeholder="请输入密码" autocomplete="current-password" />
            <button type="button" class="absolute right-3 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300" @click="showPwd = !showPwd" tabindex="-1">
              <svg v-if="showPwd" viewBox="0 0 24 24" fill="none" class="h-5 w-5" stroke="currentColor" stroke-width="1.6"><path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" /></svg>
              <svg v-else viewBox="0 0 24 24" fill="none" class="h-5 w-5" stroke="currentColor" stroke-width="1.6"><path stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178z" /><path stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" /></svg>
            </button>
          </div>
        </div>

        <div v-if="needsOtp">
          <label class="label">动态验证码</label>
          <input v-model="form.otp" class="input font-mono" inputmode="numeric" maxlength="6" placeholder="6 位验证码" autocomplete="one-time-code" />
        </div>

        <p v-if="error" class="flex items-center gap-1.5 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-400 ring-1 ring-red-500/30">
          <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4 shrink-0" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" /></svg>
          {{ error }}
        </p>

        <button type="submit" class="btn-primary w-full" :disabled="loading">
          <svg v-if="loading" class="h-4 w-4 animate-spin" viewBox="0 0 24 24" fill="none"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/><path class="opacity-90" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"/></svg>
          {{ loading ? '登录中…' : '登 录' }}
        </button>

        <div class="flex items-center gap-3">
          <span class="h-px flex-1 bg-ink-700"></span>
          <span class="text-[11px] text-slate-600">或</span>
          <span class="h-px flex-1 bg-ink-700"></span>
        </div>
        <button type="button" class="btn-secondary w-full" @click="ssoLogin">使用单点登录（SSO）</button>
      </form>
    </div>
  </div>
</template>
