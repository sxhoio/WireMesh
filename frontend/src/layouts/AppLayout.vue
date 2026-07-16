<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'

const route = useRoute()
const router = useRouter()
const app = useAppStore()
const mesh = useMeshStore()

onMounted(() => mesh.startPolling())
onUnmounted(() => mesh.stopPolling())

const nav = [
  { name: 'home', label: '首页', path: '/', icon: 'M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418' },
  { name: 'nodes', label: '节点列表', path: '/nodes', icon: 'M5.25 14.25h13.5m-13.5 0a3 3 0 01-3-3m3 3a3 3 0 100 6h13.5a3 3 0 100-6m-16.5-3a3 3 0 013-3h13.5a3 3 0 013 3m-19.5 0a4.5 4.5 0 01.9-2.7L5.737 5.1a3.375 3.375 0 012.7-1.35h7.126c1.062 0 2.062.5 2.7 1.35l2.587 3.45a4.5 4.5 0 01.9 2.7m0 0a3 3 0 01-3 3m0 3h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008zm-3 6h.008v.008h-.008v-.008zm0-6h.008v.008h-.008v-.008z' },
  { name: 'settings', label: '系统设置', path: '/settings', icon: 'M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.324.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 011.37.49l1.296 2.247a1.125 1.125 0 01-.26 1.431l-1.003.827c-.293.24-.438.613-.431.992a6.759 6.759 0 010 .255c-.007.378.138.75.43.99l1.005.828c.424.35.534.954.26 1.43l-1.298 2.247a1.125 1.125 0 01-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.57 6.57 0 01-.22.128c-.331.183-.581.495-.644.869l-.213 1.28c-.09.543-.56.941-1.11.941h-2.594c-.55 0-1.02-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 01-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 01-1.369-.49l-1.297-2.247a1.125 1.125 0 01.26-1.431l1.004-.827c.292-.24.437-.613.43-.992a6.932 6.932 0 010-.255c.007-.378-.138-.75-.43-.99l-1.004-.828a1.125 1.125 0 01-.26-1.43l1.297-2.247a1.125 1.125 0 011.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.087.22-.128.332-.183.582-.495.644-.869l.214-1.281zM15 12a3 3 0 11-6 0 3 3 0 016 0z' },
]

const networkOptions = computed(() =>
  mesh.selectedProjectId === 'all' ? mesh.networks : mesh.networks.filter((n) => n.projectId === mesh.selectedProjectId),
)

const showPublishConfirm = ref(false)
const sidebarOpen = ref(false)

function logout() {
  app.logout()
  router.push({ name: 'login' })
}

async function doPublish() {
  await mesh.publish(app.username)
  showPublishConfirm.value = false
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
          <p class="text-[11px] text-slate-500">WireMesh v2</p>
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
          <span v-if="item.name === 'nodes' && mesh.stats.linkBad" class="absolute right-3 top-1/2 flex h-5 min-w-5 -translate-y-1/2 items-center justify-center rounded-full bg-red-500/20 px-1.5 text-[10px] font-bold text-red-400 ring-1 ring-red-500/40">{{ mesh.stats.linkBad }}</span>
        </router-link>
      </nav>

      <div class="border-t border-ink-700 p-4">
        <div class="flex items-center gap-3">
          <div class="flex h-9 w-9 items-center justify-center rounded-full bg-cyan-500/15 text-sm font-bold text-cyan-300 ring-1 ring-cyan-500/40">
            {{ app.username.slice(0, 1).toUpperCase() }}
          </div>
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm font-medium text-white">{{ app.username }}</p>
            <p class="text-[11px] text-slate-500">Admin</p>
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
            Agent {{ mesh.stats.agentOnline }}/{{ mesh.stats.agentTotal }} 在线
          </span>
          <span v-if="mesh.stats.linkBad" class="chip bg-red-500/10 text-red-400 ring-1 ring-red-500/30">异常链路 {{ mesh.stats.linkBad }}</span>
          <span v-if="mesh.stats.tempCount" class="chip bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/30">临时 Peer {{ mesh.stats.tempCount }}</span>
        </div>
      </header>

      <main class="relative min-h-0 flex-1 overflow-y-auto p-6">
        <div v-if="mesh.error || mesh.notice" class="mb-4 flex items-start justify-between gap-3 rounded-xl px-4 py-3 text-sm ring-1" :class="mesh.error ? 'bg-red-500/10 text-red-300 ring-red-500/30' : 'bg-emerald-500/10 text-emerald-300 ring-emerald-500/30'">
          <span>{{ mesh.error || mesh.notice }}</span>
          <button class="shrink-0 text-current opacity-70 hover:opacity-100" @click="mesh.clearMessage()">关闭</button>
        </div>
        <router-view />

        <!-- 待发布配置条 -->
        <transition name="slide-up">
          <div v-if="app.canOperate && mesh.selectedNetworkId !== 'all'" class="sticky bottom-0 z-20 mt-4">
            <div class="panel flex items-center gap-4 !border-amber-500/40 !bg-ink-900/95 px-5 py-3.5 shadow-2xl">
              <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-amber-500/15 text-amber-400 ring-1 ring-amber-500/40">
                <svg viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" /></svg>
              </span>
              <div class="min-w-0 flex-1">
                <p class="text-sm font-medium text-amber-200">发布当前网络配置</p>
                <p class="truncate text-xs text-slate-500">{{ mesh.networkById(mesh.selectedNetworkId)?.name || mesh.selectedNetworkId }}</p>
              </div>
              <button class="btn-primary shrink-0 !py-2 text-xs" @click="showPublishConfirm = true">发布配置</button>
            </div>
          </div>
        </transition>
      </main>
    </div>

    <!-- 发布确认 -->
    <div v-if="showPublishConfirm" class="fixed inset-0 z-50 flex items-center justify-center bg-ink-950/70 p-4 backdrop-blur-sm" @click.self="showPublishConfirm = false">
      <div class="panel w-full max-w-md p-6">
        <h3 class="text-base font-semibold text-white">发布配置</h3>
        <p class="mt-2 text-sm text-slate-400">将为网络 <span class="font-medium text-white">{{ mesh.networkById(mesh.selectedNetworkId)?.name || mesh.selectedNetworkId }}</span> 生成新的不可变配置版本，并由后端下发到相关 Agent。</p>
        <div class="mt-5 flex justify-end gap-2.5">
          <button class="btn-ghost" @click="showPublishConfirm = false">取消</button>
          <button class="btn-primary" @click="doPublish">确认发布</button>
        </div>
      </div>
    </div>
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
</style>
