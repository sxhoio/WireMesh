<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'

const route = useRoute()
const router = useRouter()
const app = useAppStore()
const mesh = useMeshStore()

type ToastKind = 'error' | 'success'
interface ToastMessage { id: number; kind: ToastKind; message: string }

const toasts = ref<ToastMessage[]>([])
const toastTimers = new Map<number, number>()
let nextToastId = 1

function dismissToast(id: number) {
  const timer = toastTimers.get(id)
  if (timer) window.clearTimeout(timer)
  toastTimers.delete(id)
  toasts.value = toasts.value.filter((toast) => toast.id !== id)
}

function pushToast(message: string, kind: ToastKind) {
  const value = message.trim()
  if (!value) return
  const id = nextToastId++
  toasts.value.push({ id, kind, message: value })
  toastTimers.set(id, window.setTimeout(() => dismissToast(id), 5000))
}

watch(
  () => ({ error: mesh.error, notice: mesh.notice }),
  ({ error, notice }) => {
    if (error) pushToast(error, 'error')
    else if (notice) pushToast(notice, 'success')
    if (error || notice) mesh.clearMessage()
  },
  { flush: 'post', immediate: true },
)

onMounted(() => mesh.startPolling())
onUnmounted(() => {
  mesh.stopPolling()
  toastTimers.forEach((timer) => window.clearTimeout(timer))
  toastTimers.clear()
})

const nav = [
  { name: 'home', label: '首页', path: '/', icon: 'M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418' },
  { name: 'nodes', label: '节点列表', path: '/nodes', icon: 'M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3m-19.5 0a4.5 4.5 0 01.9-2.7L5.737 5.1a3.375 3.375 0 012.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 01.9 2.7m0 0a3 3 0 01-3 3m0 3h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008zm-3 6h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008z' },
  { name: 'clients', label: '客户端接入', path: '/clients', icon: 'M10.5 1.5H8.25A2.25 2.25 0 006 3.75v16.5a2.25 2.25 0 002.25 2.25h7.5A2.25 2.25 0 0018 20.25V3.75a2.25 2.25 0 00-2.25-2.25H13.5m-3 0V3h3V1.5m-3 0h3m-3 18.75h3' },
  { name: 'alerts', label: '告警中心', path: '/alerts', icon: 'M14.857 17.082a23.848 23.848 0 005.454-1.31A8.967 8.967 0 0118 9.75v-.7V9A6 6 0 006 9v.75a8.967 8.967 0 01-2.312 6.022c1.733.64 3.56 1.085 5.455 1.31m5.714 0a24.255 24.255 0 01-5.714 0m5.714 0a3 3 0 11-5.714 0' },
  { name: 'access', label: '访问策略', path: '/access', icon: 'M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z' },
  { name: 'dns', label: 'DNS 管理', path: '/dns', icon: 'M21 12a9 9 0 11-18 0 9 9 0 0118 0zM9.75 9.75c0 2.5 4.5 3.75 4.5 6 0 2-1.5 3-3 3s-3-1-3-3M14.25 6.75a3 3 0 11-6 0 3 3 0 016 0z' },
  { name: 'settings', label: '系统设置', path: '/settings', icon: 'M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.324.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 011.37.49l1.296 2.247a1.125 1.125 0 01-.26 1.431l-1.003.827c-.293.24-.438.613-.431.992a6.759 6.759 0 010 .255c-.007.378.138.75.43.99l1.005.828c.424.35.534.954.26 1.43l-1.298 2.247a1.125 1.125 0 01-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.57 6.57 0 01-.22.128c-.331.183-.581.495-.644.869l-.213 1.28c-.09.543-.56.941-1.11.941h-2.594c-.55 0-1.02-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 01-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 01-1.369-.49l-1.297-2.247a1.125 1.125 0 01.26-1.431l1.004-.827c.292-.24.437-.613.43-.992a6.932 6.932 0 010-.255c.007-.378-.138-.75-.43-.99l-1.004-.828a1.125 1.125 0 01-.26-1.43l1.297-2.247a1.125 1.125 0 011.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.087.22-.128.332-.183.582-.495.644-.869l.214-1.281zM15 12a3 3 0 11-6 0 3 3 0 016 0z' },
]

const networkOptions = computed(() =>
  mesh.selectedProjectId === 'all' ? mesh.networks : mesh.networks.filter((n) => n.projectId === mesh.selectedProjectId),
)

const sidebarOpen = ref(false)

const roleLabel = computed(() => {
  switch (app.user?.role) {
    case 'admin': return '管理员'
    case 'operator': return '操作员'
    case 'viewer': return '只读'
    default: return ''
  }
})

function logout() {
  app.logout()
  router.push({ name: 'login' })
}

</script>

<template>
  <div class="flex h-full">
    <!-- 移动端遮罩 -->
    <div v-if="sidebarOpen" class="fixed inset-0 z-30 bg-ink-950/70 backdrop-blur-sm lg:hidden" @click="sidebarOpen = false"></div>

    <!-- 侧边栏：移动端抽屉，桌面常驻 -->
    <aside
      class="fixed inset-y-0 left-0 z-40 flex w-60 shrink-0 flex-col border-r border-ink-700 bg-ink-900/95 transition-transform duration-200 lg:static lg:z-auto lg:translate-x-0 lg:bg-ink-900/60"
      :class="sidebarOpen ? 'translate-x-0' : '-translate-x-full'"
    >
      <div class="flex items-center gap-3 px-5 py-5">
        <div class="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-500/15 ring-1 ring-emerald-500/40">
          <svg viewBox="0 0 24 24" fill="none" class="h-5 w-5 text-emerald-400" stroke="currentColor" stroke-width="1.8">
            <path stroke-linecap="round" stroke-linejoin="round" d="M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z" />
          </svg>
        </div>
        <div>
          <p class="text-sm font-bold leading-tight text-white">{{ app.settings.dashboardName }}</p>
          <p class="text-[11px] text-slate-500">WireMesh</p>
        </div>
      </div>

      <nav class="mt-2 flex-1 space-y-1 px-3">
        <router-link
          v-for="item in nav"
          :key="item.name"
          :to="item.path"
          class="relative flex h-10 items-center gap-3 rounded-xl px-3.5 text-sm font-medium transition"
          :class="route.name === item.name ? 'bg-emerald-500/15 text-emerald-300 ring-1 ring-emerald-500/30' : 'text-slate-400 hover:bg-ink-800 hover:text-slate-200'"
          @click="sidebarOpen = false"
        >
          <svg viewBox="0 0 24 24" fill="none" class="h-5 w-5 shrink-0" stroke="currentColor" stroke-width="1.6">
            <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
          </svg>
          {{ item.label }}
          <span v-if="item.name === 'nodes' && mesh.stats.linkDown" class="absolute right-3 top-1/2 flex h-5 min-w-5 -translate-y-1/2 items-center justify-center rounded-full bg-red-500/20 px-1.5 text-[10px] font-bold text-red-400 ring-1 ring-red-500/40">{{ mesh.stats.linkDown }}</span>
        </router-link>
      </nav>

      <div class="border-t border-ink-700 p-4">
        <div class="flex items-center gap-3">
          <div class="flex h-9 w-9 items-center justify-center rounded-full bg-cyan-500/15 text-sm font-bold text-cyan-300 ring-1 ring-cyan-500/40">
            {{ app.username.slice(0, 1).toUpperCase() }}
          </div>
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm font-medium text-white">{{ app.username }}</p>
            <p class="text-[11px] text-slate-500">{{ roleLabel }}</p>
          </div>
          <button class="rounded-lg p-2 text-slate-500 transition hover:bg-ink-800 hover:text-red-400" title="退出登录" @click="logout">
            <svg viewBox="0 0 24 24" fill="none" class="h-5 w-5" stroke="currentColor" stroke-width="1.8">
              <path stroke-linecap="round" stroke-linejoin="round" d="M15.75 9V5.25A2.25 2.25 0 0013.5 3h-6a2.25 2.25 0 00-2.25 2.25v13.5A2.25 2.25 0 007.5 21h6a2.25 2.25 0 002.25-2.25V15m3 0l3-3m0 0l-3-3m3 3H9" />
            </svg>
          </button>
        </div>
      </div>
    </aside>

    <!-- 主区域 -->
    <div class="flex min-w-0 flex-1 flex-col">
      <header class="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-ink-700 bg-ink-900/40 px-4 py-3 sm:px-6">
        <!-- 汉堡按钮（移动端） -->
        <button class="rounded-lg p-2 text-slate-400 transition hover:bg-ink-800 hover:text-slate-200 lg:hidden" @click="sidebarOpen = true">
          <svg viewBox="0 0 24 24" fill="none" class="h-5 w-5" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M3.75 6.75h16.5M3.75 12h16.5m-16.5 5.25h16.5" /></svg>
        </button>

        <!-- 项目 / 网络选择器 -->
        <div class="flex min-w-0 items-center gap-2.5">
          <div class="flex min-w-0 items-center gap-1.5">
            <span class="shrink-0 text-[11px] text-slate-500">项目</span>
            <select
              class="input !w-32 max-w-40 !truncate !py-1.5 !text-xs"
              :value="mesh.selectedProjectId"
              @change="mesh.setProject(($event.target as HTMLSelectElement).value)"
            >
              <option value="all">全部项目</option>
              <option v-for="p in mesh.projects" :key="p.id" :value="p.id">{{ p.name }}</option>
            </select>
          </div>
          <div class="flex min-w-0 items-center gap-1.5">
            <span class="shrink-0 text-[11px] text-slate-500">网络</span>
            <select v-model="mesh.selectedNetworkId" class="input !w-32 max-w-40 !truncate !py-1.5 !text-xs">
              <option value="all">全部网络</option>
              <option v-for="n in networkOptions" :key="n.id" :value="n.id">{{ n.name }}</option>
            </select>
          </div>
        </div>

        <div class="ml-auto flex items-center gap-2 text-xs text-slate-400">
          <span class="chip bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/30">
            <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-emerald-400"></span>
            节点 {{ mesh.stats.agentOnline }}/{{ mesh.stats.agentTotal }} 在线
          </span>
          <span v-if="mesh.stats.linkDown" class="chip bg-red-500/10 text-red-400 ring-1 ring-red-500/30">异常链路 {{ mesh.stats.linkDown }}</span>
          <span v-if="mesh.stats.tempCount" class="chip bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/30">临时对等端 {{ mesh.stats.tempCount }}</span>
        </div>
      </header>

      <main class="relative min-h-0 flex-1 overflow-y-auto p-6">

          <router-view />

      </main>
    </div>

    <TransitionGroup
      name="toast"
      tag="div"
      class="pointer-events-none fixed bottom-5 left-4 right-4 z-[80] flex flex-col items-end gap-2.5 sm:left-auto sm:w-[24rem]"
    >
      <button
        v-for="toast in toasts"
        :key="toast.id"
        type="button"
        class="pointer-events-auto flex w-full items-start gap-3 rounded-2xl border px-4 py-3.5 text-left shadow-2xl backdrop-blur-xl transition hover:-translate-y-0.5"
        :class="toast.kind === 'error' ? 'border-red-500/35 bg-ink-900/95 text-red-200 shadow-red-950/30' : 'border-emerald-500/35 bg-ink-900/95 text-emerald-200 shadow-emerald-950/30'"
        title="点击关闭"
        @click="dismissToast(toast.id)"
      >
        <span class="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-xl ring-1" :class="toast.kind === 'error' ? 'bg-red-500/15 text-red-300 ring-red-500/35' : 'bg-emerald-500/15 text-emerald-300 ring-emerald-500/35'">
          <svg v-if="toast.kind === 'error'" viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v4m0 4h.01M10.3 3.8L2.5 17.3A2 2 0 004.2 20h15.6a2 2 0 001.7-2.7L13.7 3.8a2 2 0 00-3.4 0z" /></svg>
          <svg v-else viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M5 12.5l4.2 4.2L19 7" /></svg>
        </span>
        <span class="min-w-0 flex-1">
          <span class="block text-xs font-semibold">{{ toast.kind === 'error' ? '操作失败' : '操作成功' }}</span>
          <span class="mt-1 block break-words text-xs leading-5 text-slate-300">{{ toast.message }}</span>
          <span class="mt-1.5 block text-[10px] text-slate-600">点击后关闭</span>
        </span>
      </button>
    </TransitionGroup>

    <ConfirmDialog />
  </div>
</template>

<style scoped>
.slide-up-enter-active,
.slide-up-leave-active {
  transition: all 0.25s ease;
}
.slide-up-enter-from,
.slide-up-leave-to {
  opacity: 0;
  transform: translateY(12px);
}
.toast-enter-active,
.toast-leave-active,
.toast-move {
  transition: opacity 0.24s ease, transform 0.24s ease;
}
.toast-enter-from {
  opacity: 0;
  transform: translate(18px, 14px) scale(0.97);
}
.toast-leave-to {
  opacity: 0;
  transform: translateY(-28px) scale(0.97);
}
</style>
