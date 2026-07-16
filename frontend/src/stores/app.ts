import { defineStore } from 'pinia'
import { api, session, type ApiUser } from '../api'
import type { SystemSettings } from '../types'

const settings: SystemSettings = {
  dashboardName: 'WireMesh 控制台',
  sessionTimeoutMin: 120,
  netDefaults: { dns: '', port: 51820, mtu: 1420, keepalive: 25, defaultTopology: 'full-mesh' },
  statusRules: { agentOfflineSec: 120, handshakeSec: 180, redFailCount: 3 },
  collect: { reportSec: 10, probeSec: 15, mapRefreshSec: 30 },
  retention: { rawDays: 0, hourlyDays: 0, dailyDays: 0 },
  agent: { token: '', labels: '', upgradePolicy: 'manual' },
}

export const useAppStore = defineStore('app', {
  state: () => ({
    onboarded: false,
    authed: false,
    initialized: false,
    loading: false,
    error: '',
    user: null as ApiUser | null,
    username: '',
    password: '',
    settings: structuredClone(settings),
  }),
  getters: {
    isAdmin: (s) => s.user?.role === 'admin',
    canOperate: (s) => s.user?.role === 'admin' || s.user?.role === 'operator',
  },
  actions: {
    async restore() {
      if (this.initialized) return
      try {
        const status = await api.setupStatus()
        this.onboarded = status.initialized
        if (!status.initialized) {
          session.clear()
          this.user = null
          this.username = ''
          this.authed = false
          return
        }
        if (!session.token) return
        this.user = await api.me()
        this.username = this.user.name || this.user.email
        this.authed = true
      } catch (reason) {
        session.clear()
        this.onboarded = true
        this.user = null
        this.authed = false
        this.error = reason instanceof Error ? reason.message : '无法读取初始化状态'
      } finally { this.initialized = true }
    },
    async setup(payload: { email: string; name: string; password: string }) {
      this.loading = true
      this.error = ''
      try {
        const result = await api.setup(payload)
        session.clear()
        this.onboarded = true
        this.authed = false
        this.user = null
        this.username = result.user.email
        this.initialized = true
        return true
      } catch (reason) {
        this.error = reason instanceof Error ? reason.message : '初始化失败'
        return false
      } finally { this.loading = false }
    },
    async login(email: string, password: string) {
      this.loading = true
      this.error = ''
      try {
        const result = await api.login(email, password)
        session.token = result.token
        this.user = result.user
        this.username = result.user.name || result.user.email
        this.authed = true
        this.initialized = true
        return true
      } catch (reason) {
        this.error = reason instanceof Error ? reason.message : '登录失败'
        return false
      } finally { this.loading = false }
    },
    logout() {
      session.clear()
      this.user = null
      this.username = ''
      this.password = ''
      this.authed = false
      this.initialized = true
    },
    updateSettings(patch: Partial<SystemSettings>) { this.settings = { ...this.settings, ...patch } },
    resetAll() { this.logout(); this.settings = structuredClone(settings) },
    persist() {},
  },
})
