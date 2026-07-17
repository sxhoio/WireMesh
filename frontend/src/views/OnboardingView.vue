<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useAppStore } from '../stores/app'
import type { DatabaseDriver, DatabaseSetupConfig } from '../api'

const router = useRouter()
const app = useAppStore()

const step = ref(0)
const steps = ['欢迎', '数据库', '管理员账号', '确认配置', '完成']
const tested = ref(false)
const databaseDirty = ref(false)
const databaseReady = ref(app.databaseConfigured)
const form = reactive({ name: '', email: '', password: '', confirm: '' })
const database = reactive({
  driver: (app.databaseDriver || 'sqlite') as DatabaseDriver,
  sqlitePath: 'wiremesh.db',
  host: '127.0.0.1',
  port: app.databaseDriver === 'postgres' ? 5432 : 3306,
  name: 'wiremesh',
  username: 'wiremesh',
  password: '',
  sslMode: app.databaseDriver === 'postgres' ? 'prefer' : 'preferred',
})
const errors = reactive<Record<string, string>>({})

const driverLabel = computed(() => ({ sqlite: 'SQLite', mysql: 'MySQL', postgres: 'PostgreSQL' })[database.driver])
const databaseTarget = computed(() => {
  if (!app.databaseConfigurable) return '由服务器环境变量管理'
  if (databaseReady.value && !databaseDirty.value) return `已保存的 ${driverLabel.value} 配置`
  return database.driver === 'sqlite'
    ? database.sqlitePath || 'wiremesh.db'
    : `${database.host}:${database.port}/${database.name}`
})

const canNext = computed(() => {
  if (app.loading) return false
  if (step.value === 1) {
    if (!app.databaseConfigurable || (databaseReady.value && !databaseDirty.value)) return true
    return database.driver === 'sqlite'
      ? database.sqlitePath.trim().length > 0
      : database.host.trim().length > 0 && database.port > 0 && database.name.trim().length > 0 && database.username.trim().length > 0
  }
  if (step.value === 2) {
    return form.name.trim().length >= 2
      && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email.trim())
      && form.password.length >= 8
      && form.password === form.confirm
  }
  return true
})

watch(database, () => {
  tested.value = false
  databaseDirty.value = true
}, { deep: true })

function applyDriverDefaults() {
  tested.value = false
  if (database.driver === 'mysql') {
    database.port = 3306
    database.sslMode = 'preferred'
  } else if (database.driver === 'postgres') {
    database.port = 5432
    database.sslMode = 'prefer'
  }
}

function clearErrors() { Object.keys(errors).forEach((key) => delete errors[key]) }

function validateDatabase() {
  clearErrors()
  if (database.driver === 'sqlite') {
    if (!database.sqlitePath.trim()) errors.sqlitePath = '请输入 SQLite 数据库文件路径'
  } else {
    if (!database.host.trim()) errors.host = '请输入数据库主机地址'
    if (!Number.isInteger(database.port) || database.port < 1 || database.port > 65535) errors.port = '端口必须在 1 到 65535 之间'
    if (!database.name.trim()) errors.database = '请输入数据库名称'
    if (!database.username.trim()) errors.username = '请输入数据库用户名'
  }
  return Object.keys(errors).length === 0
}

function validateAdmin() {
  clearErrors()
  if (form.name.trim().length < 2) errors.name = '管理员名称至少 2 个字符'
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email.trim())) errors.email = '请输入有效的邮箱地址'
  if (form.password.length < 8) errors.password = '密码至少 8 位'
  if (form.password !== form.confirm) errors.confirm = '两次输入的密码不一致'
  return Object.keys(errors).length === 0
}

function databasePayload(): DatabaseSetupConfig {
  if (database.driver === 'sqlite') {
    return { driver: 'sqlite', sqlite_path: database.sqlitePath.trim() }
  }
  return {
    driver: database.driver,
    host: database.host.trim(),
    port: database.port,
    database: database.name.trim(),
    username: database.username.trim(),
    password: database.password,
    ssl_mode: database.sslMode,
  }
}

async function testConnection() {
  if (!validateDatabase()) return
  tested.value = await app.testDatabase(databasePayload())
}

async function next() {
  if (step.value === 1 && app.databaseConfigurable && (!databaseReady.value || databaseDirty.value)) {
    if (!validateDatabase()) return
    if (!tested.value && !await app.testDatabase(databasePayload())) return
    const result = await app.configureDatabase(databasePayload())
    if (!result) return
    databaseReady.value = true
    databaseDirty.value = false
    tested.value = true
    database.password = ''
    if (result.initialized) {
      router.replace({ name: 'login' })
      return
    }
  }
  if (step.value === 2 && !validateAdmin()) return
  if (step.value === 3) {
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

function finish() { router.replace({ name: 'login' }) }
</script>

<template>
  <div class="relative flex min-h-full items-center justify-center overflow-auto bg-ink-950 p-6 py-10">
    <div class="pointer-events-none absolute -top-40 left-1/2 h-96 w-[42rem] -translate-x-1/2 rounded-full bg-emerald-500/10 blur-3xl"></div>
    <div class="pointer-events-none absolute bottom-0 right-0 h-72 w-72 rounded-full bg-cyan-500/10 blur-3xl"></div>

    <div class="relative w-full max-w-3xl">
      <div class="mb-8 flex items-center justify-center gap-0">
        <template v-for="(label, index) in steps" :key="label">
          <div class="flex items-center">
            <div class="flex h-9 w-9 items-center justify-center rounded-full text-xs font-bold transition-all" :class="index < step ? 'bg-emerald-500 text-ink-950' : index === step ? 'bg-emerald-500/20 text-emerald-300 ring-2 ring-emerald-400' : 'bg-ink-800 text-slate-500 ring-1 ring-ink-600'">
              <svg v-if="index < step" viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="3"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" /></svg>
              <span v-else>{{ index + 1 }}</span>
            </div>
            <span class="ml-2 mr-3 hidden text-xs font-medium md:block" :class="index <= step ? 'text-slate-200' : 'text-slate-600'">{{ label }}</span>
          </div>
          <div v-if="index < steps.length - 1" class="mx-1 h-px w-5 sm:w-8" :class="index < step ? 'bg-emerald-500' : 'bg-ink-600'"></div>
        </template>
      </div>

      <div class="panel p-6 sm:p-8">
        <div v-if="step === 0" class="text-center">
          <div class="mx-auto mb-6 flex h-16 w-16 items-center justify-center rounded-2xl bg-emerald-500/15 ring-1 ring-emerald-500/40 shadow-glow">
            <svg viewBox="0 0 24 24" fill="none" class="h-8 w-8 text-emerald-400" stroke="currentColor" stroke-width="1.6"><path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z" /></svg>
          </div>
          <h2 class="text-2xl font-bold text-white">欢迎使用 WireMesh 控制台</h2>
          <p class="mx-auto mt-3 max-w-lg text-sm leading-relaxed text-slate-400">首次启动需要先选择业务数据库并自动创建数据表，然后创建唯一的初始管理员。数据库中已有用户后，引导入口会自动关闭。</p>
          <div class="mt-6 grid gap-3 text-left sm:grid-cols-3">
            <div class="rounded-xl bg-ink-800/70 p-4 ring-1 ring-ink-600"><p class="text-sm font-semibold text-emerald-300">选择存储</p><p class="mt-1 text-xs leading-relaxed text-slate-500">支持本地 SQLite、MySQL 与 PostgreSQL</p></div>
            <div class="rounded-xl bg-ink-800/70 p-4 ring-1 ring-ink-600"><p class="text-sm font-semibold text-cyan-300">连接验证</p><p class="mt-1 text-xs leading-relaxed text-slate-500">保存前测试连接，避免错误配置</p></div>
            <div class="rounded-xl bg-ink-800/70 p-4 ring-1 ring-ink-600"><p class="text-sm font-semibold text-violet-300">自动建表</p><p class="mt-1 text-xs leading-relaxed text-slate-500">连接成功后自动创建 WireMesh 所需表</p></div>
          </div>
        </div>

        <div v-else-if="step === 1">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div><h2 class="text-xl font-bold text-white">配置数据库</h2><p class="mt-1 text-sm text-slate-500">选择存储类型并验证连接。数据库本身需要预先存在，WireMesh 会自动创建表。</p></div>
            <span v-if="databaseReady && !databaseDirty" class="rounded-full bg-emerald-500/10 px-3 py-1 text-xs text-emerald-300 ring-1 ring-emerald-500/30">已配置 {{ app.databaseDriver || driverLabel }}</span>
          </div>

          <div v-if="!app.databaseConfigurable" class="mt-6 rounded-xl bg-cyan-500/10 p-5 ring-1 ring-cyan-500/30">
            <p class="text-sm font-semibold text-cyan-300">数据库由服务器环境变量管理</p>
            <p class="mt-2 text-sm leading-relaxed text-slate-400">当前使用 {{ app.databaseDriver || '外部' }} 数据库。为避免网页配置覆盖部署参数，引导界面不会修改该连接；继续后可直接创建初始管理员。</p>
          </div>
          <template v-else>
          <div class="mt-6 grid gap-3 sm:grid-cols-3">
            <button v-for="item in [{ value: 'sqlite', label: 'SQLite', note: '本地文件，适合单机部署' }, { value: 'mysql', label: 'MySQL', note: '连接现有 MySQL 数据库' }, { value: 'postgres', label: 'PostgreSQL', note: '连接现有 PostgreSQL 数据库' }]" :key="item.value" type="button" class="rounded-xl p-4 text-left ring-1 transition" :class="database.driver === item.value ? 'bg-emerald-500/10 ring-emerald-500/60' : 'bg-ink-800/60 ring-ink-600 hover:ring-ink-500'" @click="database.driver = item.value as DatabaseDriver; applyDriverDefaults()">
              <p class="text-sm font-semibold" :class="database.driver === item.value ? 'text-emerald-300' : 'text-slate-200'">{{ item.label }}</p>
              <p class="mt-1 text-xs text-slate-500">{{ item.note }}</p>
            </button>
          </div>

          <div v-if="database.driver === 'sqlite'" class="mt-6">
            <label class="label">数据库文件路径</label>
            <input v-model="database.sqlitePath" class="input font-mono" placeholder="wiremesh.db" />
            <p v-if="errors.sqlitePath" class="mt-1 text-xs text-red-400">{{ errors.sqlitePath }}</p>
            <p class="mt-2 text-xs leading-relaxed text-slate-500">相对路径以数据库配置文件所在目录为基准。目录不存在时会自动创建，数据直接存储在本机。</p>
          </div>

          <div v-else class="mt-6 grid gap-4 sm:grid-cols-2">
            <div><label class="label">主机地址</label><input v-model="database.host" class="input" placeholder="127.0.0.1" /><p v-if="errors.host" class="mt-1 text-xs text-red-400">{{ errors.host }}</p></div>
            <div><label class="label">端口</label><input v-model.number="database.port" type="number" min="1" max="65535" class="input" /><p v-if="errors.port" class="mt-1 text-xs text-red-400">{{ errors.port }}</p></div>
            <div><label class="label">数据库名称</label><input v-model="database.name" class="input" placeholder="wiremesh" /><p v-if="errors.database" class="mt-1 text-xs text-red-400">{{ errors.database }}</p></div>
            <div><label class="label">用户名</label><input v-model="database.username" class="input" autocomplete="username" placeholder="wiremesh" /><p v-if="errors.username" class="mt-1 text-xs text-red-400">{{ errors.username }}</p></div>
            <div><label class="label">密码</label><input v-model="database.password" type="password" class="input" autocomplete="new-password" placeholder="数据库密码" /></div>
            <div>
              <label class="label">{{ database.driver === 'mysql' ? 'TLS 模式' : 'SSL 模式' }}</label>
              <select v-model="database.sslMode" class="input">
                <template v-if="database.driver === 'mysql'"><option value="preferred">优先加密</option><option value="require">要求 TLS</option><option value="skip-verify">TLS（跳过证书验证）</option><option value="disable">禁用 TLS</option></template>
                <template v-else><option value="prefer">优先加密</option><option value="require">要求 SSL</option><option value="verify-ca">验证 CA</option><option value="verify-full">完整验证</option><option value="disable">禁用 SSL</option></template>
              </select>
            </div>
          </div>

          <div class="mt-5 flex flex-wrap items-center justify-between gap-3 rounded-xl bg-ink-800/70 p-4 ring-1 ring-ink-600">
            <div><p class="text-sm font-medium text-slate-200">连接目标：<span class="font-mono text-cyan-300">{{ databaseTarget }}</span></p><p class="mt-1 text-xs text-slate-500">点击测试不会保存配置；继续时会再次验证并自动创建全部表。已保存的数据库可直接继续，修改任一字段后会重新验证并保存。</p></div>
            <button type="button" class="btn-ghost" :disabled="app.loading" @click="testConnection"><span v-if="tested" class="text-emerald-300">✓ 连接正常</span><span v-else>{{ app.loading ? '正在测试…' : '测试连接' }}</span></button>
          </div>
          </template>
        </div>

        <div v-else-if="step === 2">
          <h2 class="text-xl font-bold text-white">创建管理员账号</h2>
          <p class="mt-1 text-sm text-slate-500">数据库表已准备完成。该账号将拥有 WireMesh 管理员权限。</p>
          <div class="mt-6 grid gap-4 sm:grid-cols-2">
            <div><label class="label">管理员名称</label><input v-model="form.name" class="input" placeholder="例如：网络管理员" autocomplete="name" /><p v-if="errors.name" class="mt-1 text-xs text-red-400">{{ errors.name }}</p></div>
            <div><label class="label">登录邮箱</label><input v-model="form.email" type="email" class="input" placeholder="admin@example.com" autocomplete="username" /><p v-if="errors.email" class="mt-1 text-xs text-red-400">{{ errors.email }}</p></div>
            <div><label class="label">密码</label><input v-model="form.password" type="password" class="input" placeholder="至少 8 位" autocomplete="new-password" /><p v-if="errors.password" class="mt-1 text-xs text-red-400">{{ errors.password }}</p></div>
            <div><label class="label">确认密码</label><input v-model="form.confirm" type="password" class="input" placeholder="再次输入密码" autocomplete="new-password" /><p v-if="errors.confirm" class="mt-1 text-xs text-red-400">{{ errors.confirm }}</p></div>
          </div>
        </div>

        <div v-else-if="step === 3">
          <h2 class="text-xl font-bold text-white">确认初始化</h2>
          <p class="mt-1 text-sm text-slate-500">确认数据库与管理员信息。提交后将创建初始租户和管理员账号。</p>
          <dl class="mt-6 space-y-3 rounded-xl bg-ink-800/70 p-5 ring-1 ring-ink-600">
            <div class="flex items-center justify-between gap-4 text-sm"><dt class="text-slate-500">数据库类型</dt><dd class="font-medium text-emerald-300">{{ driverLabel }}</dd></div>
            <div class="flex items-center justify-between gap-4 text-sm"><dt class="text-slate-500">连接目标</dt><dd class="truncate font-mono text-slate-200">{{ databaseTarget }}</dd></div>
            <div class="flex items-center justify-between gap-4 text-sm"><dt class="text-slate-500">数据表状态</dt><dd class="text-emerald-300">已自动创建</dd></div>
            <div class="border-t border-ink-600 pt-3 flex items-center justify-between gap-4 text-sm"><dt class="text-slate-500">管理员名称</dt><dd class="truncate font-medium text-slate-200">{{ form.name }}</dd></div>
            <div class="flex items-center justify-between gap-4 text-sm"><dt class="text-slate-500">登录邮箱</dt><dd class="truncate font-mono text-slate-200">{{ form.email.toLowerCase() }}</dd></div>
            <div class="flex items-center justify-between gap-4 text-sm"><dt class="text-slate-500">账号角色</dt><dd class="text-emerald-300">Administrator</dd></div>
          </dl>
          <p class="mt-4 text-xs leading-relaxed text-amber-300/80">数据库密码已加密存储且不会在界面中回显；管理员密码只保存安全哈希。</p>
        </div>

        <div v-else class="text-center">
          <div class="mx-auto mb-6 flex h-16 w-16 items-center justify-center rounded-full bg-emerald-500/15 ring-1 ring-emerald-500/40 shadow-glow"><svg viewBox="0 0 24 24" fill="none" class="h-8 w-8 text-emerald-400" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" /></svg></div>
          <h2 class="text-2xl font-bold text-white">初始化完成</h2>
          <p class="mt-3 text-sm text-slate-400">数据库和初始管理员已配置完成。现在可以使用刚刚设置的邮箱与密码登录控制台。</p>
          <dl class="mx-auto mt-6 max-w-md space-y-2 rounded-xl bg-ink-800/70 p-4 text-left ring-1 ring-ink-600">
            <div class="flex justify-between gap-4 text-sm"><dt class="text-slate-500">数据库</dt><dd class="font-medium text-emerald-300">{{ driverLabel }}</dd></div>
            <div class="flex justify-between gap-4 text-sm"><dt class="text-slate-500">管理员</dt><dd class="truncate font-medium text-slate-200">{{ form.name }}</dd></div>
            <div class="flex justify-between gap-4 text-sm"><dt class="text-slate-500">登录邮箱</dt><dd class="truncate font-mono text-slate-200">{{ form.email.toLowerCase() }}</dd></div>
          </dl>
        </div>

        <p v-if="app.error && (step === 1 || step === 3)" class="mt-5 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-400 ring-1 ring-red-500/30">{{ app.error }}</p>

        <div class="mt-8 flex items-center justify-between">
          <button v-if="step > 0 && step < steps.length - 1" class="btn-ghost" :disabled="app.loading" @click="step--">上一步</button><span v-else></span>
          <button v-if="step < steps.length - 1" class="btn-primary" :disabled="!canNext" @click="next">
            <svg v-if="app.loading" class="h-4 w-4 animate-spin" viewBox="0 0 24 24" fill="none"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/><path class="opacity-90" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"/></svg>
            {{ app.loading ? (step === 1 ? '正在连接并创建表…' : '正在初始化…') : step === 0 ? '开始配置' : step === 1 ? '保存并创建数据表' : step === 3 ? '创建初始管理员' : '下一步' }}
            <svg v-if="!app.loading" viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M13.5 4.5L21 12l-7.5 7.5M21 12H3" /></svg>
          </button>
          <button v-else class="btn-primary" @click="finish">进入登录</button>
        </div>
      </div>
    </div>
  </div>
</template>
