import { defineStore } from 'pinia'
import { api, session, type ApiUser, type DatabaseDriver, type DatabaseSetupConfig } from '../api'
import type { SystemSettings } from '../types'

const settings: SystemSettings = {
  dashboardName: 'WireMesh 控制台',
  sessionTimeoutMin: 120,
  netDefaults: { dns: '', port: 51820, mtu: 1420, keepalive: 25, defaultTopology: 'full-mesh' },
  statusRules: { agentOfflineSec: 120, handshakeSec: 180, redFailCount: 3 },
  collect: { reportSec: 10, probeSec: 15, mapRefreshSec: 30 },
  retention: { rawDays: 0, hourlyDays: 0, dailyDays: 0 },
  agent: { labels: '', upgradePolicy: 'manual', defaultMTLS: false },
}

export const useAppStore = defineStore('app', {
  state: () => ({
    onboarded: false,
    authed: false,
    initialized: false,
    databaseConfigured: false,
    databaseConfigurable: true,
    databaseDriver: '' as DatabaseDriver | '',
    setupTokenRequired: false,
    loading: false,
    error: '',
    user: null as ApiUser | null,
    username: '',
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
        this.databaseConfigured = status.database_configured ?? true
        this.databaseConfigurable = status.database_configurable ?? true
        this.databaseDriver = status.database_driver || ''
        this.setupTokenRequired = status.setup_token_required ?? false
        if (!status.initialized) {
          session.clear()
          this.user = null
          this.username = ''
          this.authed = false
          return
        }
        // 依赖 HttpOnly cookie 判断登录态：未登录（无 cookie）时 me() 返回 401，走 catch。
        this.user = await api.me()
        this.settings = await api.settings()
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
    async testDatabase(payload: DatabaseSetupConfig) {
      this.loading = true
      this.error = ''
      try {
        await api.testDatabase(payload)
        return true
      } catch (reason) {
        this.error = reason instanceof Error ? reason.message : '数据库连接测试失败'
        return false
      } finally { this.loading = false }
    },
    async configureDatabase(payload: DatabaseSetupConfig) {
      this.loading = true
      this.error = ''
      try {
        const result = await api.configureDatabase(payload)
        this.databaseConfigured = result.configured
        this.databaseDriver = result.driver
        if (result.initialized) {
          this.onboarded = true
          this.initialized = true
        }
        return result
      } catch (reason) {
        this.error = reason instanceof Error ? reason.message : '数据库配置失败'
        return null
      } finally { this.loading = false }
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
    async login(email: string, password: string, otp?: string) {
      this.loading = true
      this.error = ''
      try {
        const result = await api.login(email, password, otp)
        session.token = result.token
        this.user = result.user
        this.settings = await api.settings()
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
      void api.logout().catch(() => {})
      session.clear()
      this.user = null
      this.username = ''
      this.authed = false
      this.initialized = true
    },
    async updateSettings(value: SystemSettings) {
      this.loading = true
      this.error = ''
      try {
        this.settings = await api.updateSettings(value)
        return true
      } catch (reason) {
        this.error = reason instanceof Error ? reason.message : '保存系统设置失败'
        return false
      } finally { this.loading = false }
    },
    resetAll() { this.logout(); this.settings = structuredClone(settings) },
  },
})
