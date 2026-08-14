<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import QRCode from 'qrcode'
import CustomPeerModal from '../components/CustomPeerModal.vue'
import { api, type ApiAPIToken, type ApiUserSession } from '../api'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import type { NotificationConfig, NotifyChannel, NotifyChannelType, UserAccount } from '../types'
import { requestConfirm } from '../utils/confirm'
import { ago, fmtDateTime } from '../utils/format'

const app = useAppStore()
const mesh = useMeshStore()
const router = useRouter()

const tabGroups = [
  {
    label: '基础参数',
    items: [
      { k: 'net', l: '网络默认值', d: '新建网络与接口时的默认参数' },
      { k: 'status', l: '状态判定', d: '链路与节点在线状态的判定阈值' },
      { k: 'collect', l: '数据采集', d: 'Agent 上报与连通探测周期' },
      { k: 'retention', l: '流量保留', d: '流量采样数据的分层保留策略' },
      { k: 'geoip', l: 'GeoIP', d: 'MaxMind 地理位置数据库配置与测试' },
    ],
  },
  {
    label: '资源与集成',
    items: [
      { k: 'project', l: '项目与网络', d: '项目、网络与自定义拓扑管理' },
      { k: 'notify', l: '通知配置', d: '告警通知渠道、模板与发送记录' },
      { k: 'agent', l: 'Agent', d: '节点 Agent 接入入口' },
      { k: 'apitoken', l: 'API 令牌', d: '供脚本与 CI 使用的长期 API 凭据' },
    ],
  },
  {
    label: '安全与账号',
    items: [
      { k: 'users', l: '用户与权限', d: '控制台用户、角色与登录状态' },
      { k: 'sessions', l: '登录会话', d: '活跃会话查看与强制下线' },
      { k: 'mfa', l: '多因素认证', d: 'TOTP 动态验证码绑定' },
      { k: 'sso', l: '单点登录', d: 'OIDC 单点登录接入' },
    ],
  },
  {
    label: '审计与运维',
    items: [
      { k: 'audit', l: '审计日志', d: '控制台操作审计记录' },
      { k: 'publish', l: '发布记录', d: '配置版本发布历史与节点应用结果' },
      { k: 'backup', l: '备份与恢复', d: 'SQLite 在线备份与恢复' },
    ],
  },
] as const

type TabKey = (typeof tabGroups)[number]['items'][number]['k']

const initialTab = sessionStorage.getItem('settings-tab')
const tab = ref<TabKey>(initialTab && tabGroups.some((group) => group.items.some((item) => item.k === initialTab)) ? (initialTab as TabKey) : 'net')
watch(tab, (value) => sessionStorage.setItem('settings-tab', value))
const allTabs = computed(() => {
  const out: { k: TabKey; l: string; d: string }[] = []
  for (const group of tabGroups) out.push(...group.items)
  return out
})
const currentTabMeta = computed(() => allTabs.value.find((item) => item.k === tab.value))
const formTabKeys: readonly string[] = ['net', 'status', 'collect', 'retention']

/** 设置类 tab 是否有未保存的修改（与已生效配置逐字段对比） */
const formDirty = computed(
  () => formTabKeys.includes(tab.value) && JSON.stringify(form) !== JSON.stringify(app.settings),
)

// ---- Agent 状态页 ----
const agentManifest = computed(() => mesh.agentUpdate.manifest)
const agentNodeStatus = (id: string) => mesh.agentUpdate.node_status.find((status) => status.node_id === id)
const updatableCount = computed(() => mesh.agentUpdate.node_status.filter((status) => status.needs_update).length)
const manualOnlyCount = computed(() => mesh.agentUpdate.node_status.filter((status) => status.reason).length)
const agentVersionGroups = computed(() => {
  const counts = new Map<string, number>()
  for (const agent of mesh.agents) {
    const version = agent.version || '未知版本'
    counts.set(version, (counts.get(version) || 0) + 1)
  }
  return [...counts.entries()].sort((a, b) => b[1] - a[1])
})

/** 切换 tab：设置类 tab 有未保存修改时先确认，避免静默丢弃 */
function selectTab(key: TabKey) {
  if (tab.value === key) return
  if (formDirty.value) {
    void requestConfirm({
      title: '有未保存的修改',
      message: '当前设置尚未保存，切换后修改将丢失。仍要切换吗？',
      confirmText: '放弃修改并切换',
      variant: 'warning',
    }).then((ok) => {
      if (ok) tab.value = key
    })
    return
  }
  tab.value = key
}

const saved = ref(false)
let savedTimer: number | undefined
const confirmReset = ref(false)
const customPeerNetwork = ref<string | null>(null)

const form = reactive(JSON.parse(JSON.stringify(app.settings)) as typeof app.settings)

const geoTestIP = ref('')
const geoTestResult = ref<string | null>(null)
const geoDbPath = ref(mesh.geoip.dbPath)
const geoSaving = ref(false)
const geoReloading = ref(false)
// 重载进度条：区间模拟（mmdb 解析为同步阻塞，无法拿到真实百分比）
const geoReloadProgress = ref(0)
let geoReloadTimer: number | undefined

watch(
  () => mesh.geoip.dbPath,
  (path, previousPath) => {
    if (!geoDbPath.value.trim() || geoDbPath.value.trim() === (previousPath || '')) geoDbPath.value = path
  },
  { immediate: true },
)

async function saveGeoDbPath() {
  if (geoSaving.value) return
  geoSaving.value = true
  try {
    if (await mesh.updateGeoDbPath(geoDbPath.value.trim(), app.username)) geoDbPath.value = mesh.geoip.dbPath
  } finally {
    geoSaving.value = false
  }
}

async function reloadGeoDb() {
  if (geoReloading.value || !mesh.geoip.dbPath) return
  geoReloading.value = true
  geoReloadProgress.value = 0
  // 区间模拟：重载期间缓慢推进（0→90%），完成后补到 100% 再复位
  geoReloadTimer = window.setInterval(() => {
    geoReloadProgress.value = Math.min(90, geoReloadProgress.value + 7)
  }, 120)
  try {
    await mesh.reloadGeoIP(app.username)
    geoReloadProgress.value = 100
  } finally {
    if (geoReloadTimer) window.clearInterval(geoReloadTimer)
    geoReloadTimer = undefined
    window.setTimeout(() => { geoReloadProgress.value = 0; geoReloading.value = false }, 350)
  }
}

const roleMeta = { admin: { l: 'Admin', c: 'bg-red-500/10 text-red-300 ring-red-500/30' }, operator: { l: 'Operator', c: 'bg-cyan-500/10 text-cyan-300 ring-cyan-500/30' }, viewer: { l: 'Viewer', c: 'bg-slate-500/10 text-slate-400 ring-slate-500/30' } }
const topologyLabel = { 'full-mesh': 'Full Mesh', 'hub-spoke': 'Hub-Spoke', custom: 'Custom' }

const newUser = reactive({ name: '', email: '', password: '', role: 'viewer' as 'viewer' | 'operator' | 'admin' })

const newProject = reactive({ name: '', desc: '' })
const newNetwork = reactive({ projectId: mesh.projects[0]?.id ?? '', name: '', cidr: '', topology: 'full-mesh' as 'full-mesh' | 'hub-spoke' | 'custom' })

watch(
  () => mesh.projects,
  (projects) => {
    if (!projects.some((project) => project.id === newNetwork.projectId)) newNetwork.projectId = projects[0]?.id ?? ''
  },
  { immediate: true },
)

const canAddNetwork = computed(
  () => app.canOperate && !!newNetwork.projectId && !!newNetwork.name.trim() && /^\d+\.\d+\.\d+\.\d+\/\d{1,2}$/.test(newNetwork.cidr.trim()),
)

async function addProject() {
  if (!app.isAdmin || !newProject.name.trim()) return
  if (await mesh.addProject({ name: newProject.name.trim(), desc: newProject.desc.trim() }, app.username)) {
    newProject.name = ''
    newProject.desc = ''
    if (!newNetwork.projectId) newNetwork.projectId = mesh.projects[0]?.id ?? ''
  }
}

async function addNetwork() {
  if (!app.canOperate || !canAddNetwork.value) return
  if (await mesh.addNetwork(
    { projectId: newNetwork.projectId, name: newNetwork.name.trim(), cidr: newNetwork.cidr.trim(), topology: newNetwork.topology },
    app.username,
  )) {
    newNetwork.name = ''
    newNetwork.cidr = ''
  }
}

async function save() {
  if (await app.updateSettings(JSON.parse(JSON.stringify(form)))) {
    saved.value = true
    if (savedTimer) window.clearTimeout(savedTimer)
    savedTimer = window.setTimeout(() => (saved.value = false), 1600)
  } else {
    mesh.error = app.error || '保存系统设置失败'
  }
}


async function testGeoIP() {
  geoTestResult.value = null
  const result = await mesh.lookupGeoIP(geoTestIP.value.trim())
  if (result) {
    const place = [result.country, result.city].filter(Boolean).join(' / ') || '未匹配到城市信息'
    geoTestResult.value = result.ip + ' · ' + place + ' · ' + result.latitude + ', ' + result.longitude + (result.timezone ? ' · ' + result.timezone : '')
  }
}

async function addUser() {
  if (!app.isAdmin || !newUser.name.trim() || !newUser.email.trim() || newUser.password.length < 8) return
  if (await mesh.addUser({ name: newUser.name.trim(), email: newUser.email.trim(), password: newUser.password, role: newUser.role })) {
    newUser.name = ''
    newUser.email = ''
    newUser.password = ''
    newUser.role = 'viewer'
  }
}

// ---- 修改自己的密码 ----
const passwordForm = reactive({ oldPassword: '', newPassword: '', confirm: '', otp: '' })
const passwordSaving = ref(false)
const passwordError = ref('')

async function changeMyPassword() {
  passwordError.value = ''
  if (!passwordForm.oldPassword || !passwordForm.newPassword) {
    passwordError.value = '请输入旧密码与新密码'
    return
  }
  if (passwordForm.newPassword.length < 8) {
    passwordError.value = '新密码至少需要 8 个字符'
    return
  }
  if (passwordForm.newPassword !== passwordForm.confirm) {
    passwordError.value = '两次输入的新密码不一致'
    return
  }
  if (mfaEnabled.value && !passwordForm.otp.trim()) {
    passwordError.value = '该账号已启用多因素认证，请输入动态验证码'
    return
  }
  passwordSaving.value = true
  try {
    await api.changePassword({ old_password: passwordForm.oldPassword, new_password: passwordForm.newPassword, otp: passwordForm.otp.trim() || undefined })
    mesh.notice = '密码已修改'
    passwordForm.oldPassword = ''
    passwordForm.newPassword = ''
    passwordForm.confirm = ''
    passwordForm.otp = ''
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : '修改密码失败'
    passwordError.value = message.includes('otp_required') ? '该账号已启用多因素认证，请输入动态验证码' : message.includes('otp_invalid') ? '动态验证码错误，请重试' : message
  } finally {
    passwordSaving.value = false
  }
}

// ---- 用户管理（角色 / 启停 / 删除）----
async function changeUserRole(user: UserAccount, role: UserAccount['role']) {
  if (user.role === role) return
  await mesh.updateUserAccount(user.id, { role })
}

async function toggleUserActive(user: UserAccount) {
  if (user.active) {
    const confirmed = await requestConfirm({
      title: '停用用户',
      message: `确定停用“${user.name}”吗？其现有登录会话将立即失效。`,
      confirmText: '停用用户',
      variant: 'warning',
    })
    if (!confirmed) return
  }
  await mesh.updateUserAccount(user.id, { active: !user.active })
}

async function deleteUser(user: UserAccount) {
  const confirmed = await requestConfirm({
    title: '删除用户',
    message: `确定删除用户“${user.name}”（${user.email}）吗？此操作无法恢复。`,
    confirmText: '删除用户',
    variant: 'danger',
  })
  if (!confirmed) return
  await mesh.removeUserAccount(user.id)
}

function doReset() {
  app.resetAll()
  confirmReset.value = false
  router.push({ name: 'login' })
}

const currentRevision = computed(() => mesh.revisions[0])

async function refreshAudit() {
  await mesh.loadAuditPage(true)
}

async function clearAuditLogs() {
  const confirmed = await requestConfirm({
    title: '清空审计日志',
    message: '确定清空全部审计日志吗？\n此操作无法恢复。',
    confirmText: '清空日志',
    variant: 'danger',
  })
  if (!confirmed) return
  await mesh.clearAudit()
}

function auditActionClass(action: string) {
  const normalized = action.toLowerCase()
  if (normalized.includes('failed') || normalized.includes('fail') || normalized.includes('error') || normalized.includes('delete') || normalized.includes('clear') || normalized.includes('清空') || normalized.includes('删除') || normalized.includes('失败') || normalized.includes('回滚')) {
    return 'text-red-300'
  }
  if (normalized.includes('publish') || normalized.includes('config') || normalized.includes('save') || normalized.includes('update') || normalized.includes('发布') || normalized.includes('配置') || normalized.includes('保存') || normalized.includes('更新') || normalized.includes('成功') || normalized.includes('重载')) {
    return 'text-emerald-300'
  }
  if (normalized.includes('login') || normalized.includes('setup') || normalized.includes('create') || normalized.includes('登录') || normalized.includes('初始化') || normalized.includes('创建')) {
    return 'text-violet-300'
  }
  return 'text-slate-300'
}

// ---- 审计日志筛选 ----
const auditKeyword = ref('')
const auditCategory = ref<'all' | 'danger' | 'change' | 'login'>('all')

function auditCategoryOf(action: string) {
  const normalized = action.toLowerCase()
  if (normalized.includes('failed') || normalized.includes('fail') || normalized.includes('error') || normalized.includes('delete') || normalized.includes('clear') || normalized.includes('revoke') || normalized.includes('回滚') || normalized.includes('删除') || normalized.includes('清空') || normalized.includes('失败')) return 'danger' as const
  if (normalized.includes('login') || normalized.includes('登录')) return 'login' as const
  return 'change' as const
}

const filteredAudit = computed(() => {
  let list = mesh.audit
  if (auditCategory.value !== 'all') list = list.filter((entry) => auditCategoryOf(entry.action) === auditCategory.value)
  const keyword = auditKeyword.value.trim().toLowerCase()
  if (keyword) list = list.filter((entry) => `${entry.user} ${entry.action} ${entry.detail}`.toLowerCase().includes(keyword))
  return list
})

// ---- 通知记录筛选 ----
const notifyLogFilter = ref<'all' | 'sent' | 'failed' | 'test'>('all')
const filteredNotifyLogs = computed(() => {
  if (notifyLogFilter.value === 'all') return mesh.notifyLogs
  return mesh.notifyLogs.filter((log) => log.status === notifyLogFilter.value)
})

// ---- 通知配置 ----
const channelTypeMeta: Record<NotifyChannelType, { l: string; c: string; description: string }> = {
  webhook: { l: 'Webhook', c: 'bg-cyan-500/10 text-cyan-300 ring-cyan-500/30', description: '自定义 HTTP 请求、请求头和签名' },
  dingtalk: { l: '钉钉', c: 'bg-blue-500/10 text-blue-300 ring-blue-500/30', description: '群机器人 Webhook 与加签' },
  wecom: { l: '企业微信', c: 'bg-emerald-500/10 text-emerald-300 ring-emerald-500/30', description: '群机器人文本或 Markdown 消息' },
  feishu: { l: '飞书', c: 'bg-violet-500/10 text-violet-300 ring-violet-500/30', description: '自定义机器人与签名校验' },
  telegram: { l: 'Telegram', c: 'bg-sky-500/10 text-sky-300 ring-sky-500/30', description: 'Bot API、群组和 Topic' },
  email: { l: '邮件', c: 'bg-amber-500/10 text-amber-300 ring-amber-500/30', description: 'SMTP、收件人和主题模板' },
}
const defaultMessageTemplate = '【{{.Title}}】\n事件：{{.Event}}\n节点：{{.NodeName}}\n状态：{{.NodeStatus}}\n详情：{{.Message}}\n时间：{{.OccurredAt}}'
const defaultWebhookTemplate = '{"event":{{json .Event}},"title":{{json .Title}},"message":{{json .Message}},"nodeName":{{json .NodeName}},"nodeStatus":{{json .NodeStatus}},"occurredAt":{{json .OccurredAt}}}'
const defaultSubjectTemplate = 'WireMesh：{{.Title}}'
const webhookJSONExample = '{{json .Message}}'
const templateVariables = [
  { key: '{{.Event}}', label: '事件类型' }, { key: '{{.Title}}', label: '通知标题' }, { key: '{{.Message}}', label: '详细内容' },
  { key: '{{.NodeName}}', label: '节点名称' }, { key: '{{.NodeID}}', label: '节点 ID' }, { key: '{{.NodeStatus}}', label: '节点状态' },
  { key: '{{.NetworkName}}', label: '网络名称' }, { key: '{{.ProjectName}}', label: '项目名称' }, { key: '{{.Endpoint}}', label: '节点端点' },
  { key: '{{.Region}}', label: '节点区域' }, { key: '{{.OS}}', label: '操作系统' }, { key: '{{.AgentVersion}}', label: 'Agent 版本' },
  { key: '{{.OccurredAt}}', label: '发生时间' }, { key: '{{.DashboardURL}}', label: '控制台地址' },
]
function defaultChannelConfig(type: NotifyChannelType): NotificationConfig {
  if (type === 'webhook') return { method: 'POST', contentType: 'application/json', signatureType: 'none', timeoutSec: 8, headers: [], allowPrivate: false }
  if (type === 'dingtalk') return { messageType: 'markdown', timeoutSec: 8, atAll: false, atMobiles: [], allowPrivate: false }
  if (type === 'wecom') return { messageType: 'markdown', timeoutSec: 8, atAll: false, atMobiles: [], atUserIds: [], allowPrivate: false }
  if (type === 'feishu') return { messageType: 'text', timeoutSec: 8, atAll: false, allowPrivate: false }
  if (type === 'telegram') return { parseMode: 'HTML', timeoutSec: 8, disableWebPagePreview: true, disableNotification: false, useProxy: false }
  return { smtpPort: 587, encryption: 'starttls', timeoutSec: 10, to: [], cc: [], allowPrivate: false, skipTlsVerify: false }
}
function defaultTemplate(type: NotifyChannelType) { return type === 'webhook' ? defaultWebhookTemplate : defaultMessageTemplate }
const editingChannelId = ref<string | null>(null)
const channelForm = reactive({
  name: '', type: 'dingtalk' as NotifyChannelType, config: defaultChannelConfig('dingtalk'),
  template: defaultTemplate('dingtalk'), subjectTemplate: defaultSubjectTemplate,
  atMobilesText: '', atUserIdsText: '', recipientsText: '', ccText: '', scope: 'all' as 'all' | 'custom', agents: [] as string[],
})
const channelFormError = ref('')
function splitConfigList(value: string) { return [...new Set(value.split(/[\n,，;；]+/).map((item) => item.trim()).filter(Boolean))] }
function resetChannelForm() {
  editingChannelId.value = null; channelForm.name = ''; channelForm.type = 'dingtalk'; channelForm.config = defaultChannelConfig('dingtalk')
  channelForm.template = defaultTemplate('dingtalk'); channelForm.subjectTemplate = defaultSubjectTemplate
  channelForm.atMobilesText = ''; channelForm.atUserIdsText = ''; channelForm.recipientsText = ''; channelForm.ccText = ''
  channelForm.scope = 'all'; channelForm.agents = []; channelFormError.value = ''
}
function changeChannelType() {
  channelForm.config = defaultChannelConfig(channelForm.type); channelForm.template = defaultTemplate(channelForm.type); channelForm.subjectTemplate = defaultSubjectTemplate
  channelForm.atMobilesText = ''; channelForm.atUserIdsText = ''; channelForm.recipientsText = ''; channelForm.ccText = ''; channelFormError.value = ''
}
function editChannel(c: NotifyChannel) {
  editingChannelId.value = c.id; channelForm.name = c.name; channelForm.type = c.type
  channelForm.config = JSON.parse(JSON.stringify(c.config || defaultChannelConfig(c.type)))
  channelForm.template = c.template || defaultTemplate(c.type); channelForm.subjectTemplate = c.subjectTemplate || defaultSubjectTemplate
  channelForm.atMobilesText = c.config.atMobiles?.join('\n') || ''; channelForm.atUserIdsText = c.config.atUserIds?.join('\n') || ''
  channelForm.recipientsText = ''; channelForm.ccText = ''; channelForm.scope = c.agents === 'all' ? 'all' : 'custom'
  channelForm.agents = c.agents === 'all' ? [] : [...c.agents]; channelFormError.value = ''
}
function toggleChannelAgent(id: string) { const i = channelForm.agents.indexOf(id); if (i >= 0) channelForm.agents.splice(i, 1); else channelForm.agents.push(id) }
function addWebhookHeader() { if (!channelForm.config.headers) channelForm.config.headers = []; channelForm.config.headers.push({ name: '', value: '' }) }
function removeWebhookHeader(index: number) { channelForm.config.headers?.splice(index, 1) }
function restoreDefaultTemplate() { channelForm.template = defaultTemplate(channelForm.type); if (channelForm.type === 'email') channelForm.subjectTemplate = defaultSubjectTemplate }
function insertTemplateVariable(value: string) { channelForm.template += (channelForm.template.endsWith('\n') || !channelForm.template ? '' : '\n') + value }
function isConfigured(field: 'url' | 'secret' | 'botToken' | 'proxyUrl' | 'chatId' | 'password' | 'recipients' | 'cc') {
  const config = channelForm.config
  if (field === 'url') return !!config.urlConfigured; if (field === 'secret') return !!config.secretConfigured
  if (field === 'botToken') return !!config.botTokenConfigured; if (field === 'proxyUrl') return !!config.proxyUrlConfigured
  if (field === 'chatId') return !!config.chatIdConfigured; if (field === 'password') return !!config.passwordConfigured
  if (field === 'recipients') return !!config.recipientsConfigured
  return !!config.ccConfigured
}
function configuredPlaceholder(field: Parameters<typeof isConfigured>[0], fallback: string) { return isConfigured(field) ? '已安全保存，留空保持不变' : fallback }
function validProxyURL(value: string) {
  try {
    const proxy = new URL(value)
    return ['http:', 'https:', 'socks5:', 'socks5h:'].includes(proxy.protocol) && !!proxy.hostname && !proxy.search && !proxy.hash && (proxy.pathname === '' || proxy.pathname === '/')
  } catch {
    return false
  }
}
function validateChannelForm(config: NotificationConfig) {
  if (!channelForm.name.trim()) return '请输入渠道名称'
  if (!channelForm.template.trim()) return '请输入通知消息模板'
  if (channelForm.scope === 'custom' && !channelForm.agents.length) return '指定节点时至少选择一个节点'
  if (channelForm.type === 'webhook') {
    if (!config.url && !config.urlConfigured) return '请输入 Webhook URL'
    if (config.signatureType !== 'none' && !config.secret && !config.secretConfigured) return '当前签名方式需要填写密钥或令牌'
    if (config.headers?.some((header) => !header.name.trim())) return '自定义请求头名称不能为空'
  } else if (['dingtalk', 'wecom', 'feishu'].includes(channelForm.type)) {
    if (!config.url && !config.urlConfigured) return '请输入机器人 Webhook URL'
  } else if (channelForm.type === 'telegram') {
    if (!config.botToken && !config.botTokenConfigured) return '请输入 Bot Token'
    if (!config.chatId && !config.chatIdConfigured) return '请输入 Chat ID'
    if (config.useProxy && !config.proxyUrl && !config.proxyUrlConfigured) return '启用代理后请输入代理地址'
    if (config.proxyUrl && !validProxyURL(config.proxyUrl)) return '代理地址必须使用 http、https、socks5 或 socks5h 协议'
  } else if (channelForm.type === 'email') {
    if (!config.smtpHost?.trim()) return '请输入 SMTP 主机'
    if (!config.fromAddress?.trim()) return '请输入发件人地址'
    if (!splitConfigList(channelForm.recipientsText).length && !config.recipientsConfigured) return '请至少填写一个收件人'
    if (config.username && !config.password && !config.passwordConfigured) return '配置 SMTP 用户名时必须填写密码'
    if (!channelForm.subjectTemplate.trim()) return '请输入邮件主题模板'
  }
  return ''
}
async function saveChannel() {
  channelFormError.value = ''
  const config = JSON.parse(JSON.stringify(channelForm.config)) as NotificationConfig
  const mobiles = splitConfigList(channelForm.atMobilesText); const userIds = splitConfigList(channelForm.atUserIdsText)
  if (mobiles.length || !config.atMobilesConfigured) config.atMobiles = mobiles; else delete config.atMobiles
  if (userIds.length || !config.atUserIdsConfigured) config.atUserIds = userIds; else delete config.atUserIds
  if (channelForm.type === 'email') {
    const recipients = splitConfigList(channelForm.recipientsText); const cc = splitConfigList(channelForm.ccText)
    if (recipients.length) config.to = recipients
    if (channelForm.ccText.trim() || !config.ccConfigured) config.cc = cc
  }
  const error = validateChannelForm(config); if (error) { channelFormError.value = error; return }
  const payload = {
    name: channelForm.name.trim(), type: channelForm.type, config, template: channelForm.template, subjectTemplate: channelForm.subjectTemplate,
    enabled: editingChannelId.value ? (mesh.notifyChannels.find((channel) => channel.id === editingChannelId.value)?.enabled ?? true) : true,
    agents: channelForm.scope === 'all' ? 'all' as const : [...channelForm.agents],
  }
  const ok = editingChannelId.value ? await mesh.updateNotifyChannel(editingChannelId.value, payload, app.username) : await mesh.addNotifyChannel(payload, app.username)
  if (ok) resetChannelForm(); else channelFormError.value = mesh.error || '保存通知渠道失败'
}
async function confirmDeleteChannel(c: NotifyChannel) {
  const confirmed = await requestConfirm({
    title: '删除通知渠道',
    message: `确定删除「${c.name}」吗？删除后其绑定节点的断开告警将不再推送。`,
    confirmText: '删除渠道',
    variant: 'danger',
  })
  if (!confirmed) return
  await mesh.removeNotifyChannel(c.id, app.username)
}
function channelAgentsLabel(c: NotifyChannel) { if (c.agents === 'all') return '全部节点'; return c.agents.map((id) => mesh.agentById(id)?.name ?? id).join('、') }
function channelConfigSummary(c: NotifyChannel) {
  const config = c.config || {}
  if (c.type === 'webhook') return (config.method || 'POST') + ' · ' + (config.contentType || 'application/json') + ' · ' + (config.urlConfigured ? '目标已配置' : '目标未配置')
  if (c.type === 'telegram') return 'Bot ' + (config.botTokenConfigured ? '已配置' : '未配置') + ' · Chat ' + (config.chatIdConfigured ? '已配置' : '未配置') + ' · ' + (config.parseMode || '纯文本') + ' · ' + (config.useProxy ? (config.proxyUrlConfigured ? '代理已配置' : '代理未配置') : '直连')
  if (c.type === 'email') return (config.smtpHost || 'SMTP 未配置') + ':' + (config.smtpPort || '-') + ' · ' + (config.recipientCount || 0) + ' 个收件人 · ' + (config.encryption || 'none')
  return (config.messageType || 'text') + ' · ' + (config.urlConfigured ? 'Webhook 已配置' : 'Webhook 未配置') + (config.secretConfigured ? ' · 签名已配置' : '')
}

// ---- API 令牌 ----
const apiTokens = ref<ApiAPIToken[]>([])
const newTokenName = ref('')
const newTokenTTL = ref(0)
const createdToken = ref<string | null>(null)
const tokenSaving = ref(false)

async function loadAPITokens() {
  try {
    apiTokens.value = await api.apiTokens()
  } catch {
    apiTokens.value = []
  }
}

watch(() => tab.value, (value) => {
  if (value === 'apitoken') void loadAPITokens()
  if (value === 'sessions') void loadSessions()
  if (value === 'mfa') void loadMFA()
  if (value === 'sso') void loadSSO()
})

async function createAPIToken() {
  if (!newTokenName.value.trim() || tokenSaving.value) return
  tokenSaving.value = true
  try {
    const result = await api.createAPIToken({ name: newTokenName.value.trim(), ttl_days: newTokenTTL.value })
    createdToken.value = result.token
    newTokenName.value = ''
    await loadAPITokens()
  } catch (reason) {
    mesh.error = reason instanceof Error ? reason.message : '创建 API 令牌失败'
  } finally {
    tokenSaving.value = false
  }
}

async function removeAPIToken(id: string) {
  const confirmed = await requestConfirm({
    title: '撤销 API 令牌',
    message: '确定撤销该令牌吗？使用它的脚本将立即失去访问权限，此操作无法恢复。',
    confirmText: '撤销令牌',
    variant: 'danger',
  })
  if (!confirmed) return
  try {
    await api.deleteAPIToken(id)
    await loadAPITokens()
  } catch (reason) {
    mesh.error = reason instanceof Error ? reason.message : '撤销令牌失败'
  }
}

// ---- 备份与恢复 ----
const backingUp = ref(false)
const restoring = ref(false)
const restoreFile = ref<File | null>(null)

async function downloadBackup() {
  if (backingUp.value) return
  backingUp.value = true
  try {
    const blob = await api.backupDownload()
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = 'wiremesh-backup.db'
    anchor.click()
    URL.revokeObjectURL(url)
    mesh.notice = '数据库备份已下载'
  } catch (reason) {
    mesh.error = reason instanceof Error ? reason.message : '备份下载失败'
  } finally {
    backingUp.value = false
  }
}

function pickRestoreFile(event: Event) {
  restoreFile.value = (event.target as HTMLInputElement).files?.[0] || null
}

async function restoreBackup() {
  if (!restoreFile.value || restoring.value) return
  const confirmed = await requestConfirm({
    title: '恢复数据库备份',
    message: '恢复将立即用备份文件替换当前数据库：未保存的变更将丢失，所有用户需要重新登录。确定继续吗？',
    confirmText: '恢复数据库',
    variant: 'danger',
  })
  if (!confirmed) return
  restoring.value = true
  try {
    await api.backupRestore(restoreFile.value)
    restoreFile.value = null
    mesh.notice = '数据库已恢复；请重新登录以继续'
    app.logout()
    router.push({ name: 'login' })
  } catch (reason) {
    mesh.error = reason instanceof Error ? reason.message : '恢复失败'
  } finally {
    restoring.value = false
  }
}

// ---- 登录会话 ----
const sessions = ref<ApiUserSession[]>([])

async function loadSessions() {
  try {
    sessions.value = await api.sessions()
  } catch {
    sessions.value = []
  }
}

async function revokeSession(id: string) {
  const confirmed = await requestConfirm({
    title: '强制下线',
    message: '确定强制下线该会话吗？对应令牌将立即失效。',
    confirmText: '强制下线',
    variant: 'danger',
  })
  if (!confirmed) return
  try {
    await api.revokeSession(id)
    await loadSessions()
  } catch (reason) {
    mesh.error = reason instanceof Error ? reason.message : '强制下线失败'
  }
}

// ---- 多因素认证 ----
const mfaEnabled = ref(false)
const mfaSecret = ref('')
const mfaUri = ref('')
const mfaQr = ref('')
const mfaOtp = ref('')

async function loadMFA() {
  try {
    mfaEnabled.value = (await api.mfaStatus()).enabled
  } catch {
    mfaEnabled.value = false
  }
}

async function setupMFA() {
  try {
    const result = await api.mfaSetup()
    mfaSecret.value = result.secret
    mfaUri.value = result.uri
    mfaQr.value = await QRCode.toDataURL(result.uri, { width: 240, margin: 1 })
    mfaOtp.value = ''
  } catch (reason) {
    mesh.error = reason instanceof Error ? reason.message : 'MFA 初始化失败'
  }
}

async function enableMFA() {
  try {
    await api.mfaEnable(mfaOtp.value.trim())
    mfaEnabled.value = true
    mfaSecret.value = ''
    mfaUri.value = ''
    mfaQr.value = ''
    mfaOtp.value = ''
    mesh.notice = '多因素认证已启用'
  } catch (reason) {
    mesh.error = reason instanceof Error ? reason.message : '验证码校验失败'
  }
}

async function disableMFA() {
  const password = window.prompt('关闭多因素认证需要验证当前密码，请输入密码：', '')
  if (password === null) return
  const otp = mfaEnabled.value ? window.prompt('请输入认证器中的 6 位动态验证码：', '') : ''
  if (mfaEnabled.value && otp === null) return
  try {
    await api.mfaDisable({ password, otp: otp?.trim() || undefined })
    mfaEnabled.value = false
    mesh.notice = '多因素认证已关闭'
  } catch (reason) {
    const message = reason instanceof Error ? reason.message : '关闭 MFA 失败'
    mesh.error = message.includes('otp_required') ? '请输入动态验证码后重试' : message.includes('otp_invalid') ? '动态验证码错误，请重试' : message
  }
}

// ---- 单点登录（SSO / OIDC） ----
const ssoForm = reactive({ issuer: '', client_id: '', client_secret: '', enabled: false })
const ssoSecretConfigured = ref(false)
const ssoSaving = ref(false)

async function loadSSO() {
  try {
    const config = await api.ssoConfig()
    ssoForm.issuer = config.issuer
    ssoForm.client_id = config.client_id
    ssoForm.enabled = config.enabled
    ssoSecretConfigured.value = config.client_secret_configured
    ssoForm.client_secret = ''
  } catch {
    ssoForm.issuer = ''
    ssoForm.client_id = ''
    ssoForm.enabled = false
    ssoSecretConfigured.value = false
  }
}

async function saveSSO() {
  if (ssoSaving.value) return
  ssoSaving.value = true
  try {
    const result = await api.updateSSOConfig({
      issuer: ssoForm.issuer.trim(),
      client_id: ssoForm.client_id.trim(),
      client_secret: ssoForm.client_secret,
      enabled: ssoForm.enabled,
    })
    ssoSecretConfigured.value = result.client_secret_configured
    ssoForm.client_secret = ''
    mesh.notice = '单点登录配置已保存'
  } catch (reason) {
    mesh.error = reason instanceof Error ? reason.message : '保存 SSO 配置失败'
  } finally {
    ssoSaving.value = false
  }
}

onUnmounted(() => { if (savedTimer) window.clearTimeout(savedTimer) })

</script>

<template>
  <div class="grid w-full grid-cols-1 gap-4 lg:grid-cols-[12rem_minmax(0,1fr)] lg:gap-6">
    <!-- 分组导航：移动端横向滚动，桌面左侧竖排分组 -->
    <aside class="w-full min-w-0">
      <div class="flex gap-1 overflow-x-auto pb-1 lg:sticky lg:top-0 lg:flex lg:max-h-[calc(100vh-6rem)] lg:flex-col lg:overflow-y-auto lg:pb-0">
        <template v-for="group in tabGroups" :key="group.label">
          <p class="hidden shrink-0 px-3.5 pb-1 pt-3 text-[10px] font-semibold uppercase tracking-wider text-slate-600 first:pt-0 lg:block lg:whitespace-nowrap">{{ group.label }}</p>
          <button
            v-for="t in group.items"
            :key="t.k"
            class="shrink-0 whitespace-nowrap rounded-xl px-3.5 py-2 text-left text-sm font-medium transition lg:w-full lg:shrink"
            :class="tab === t.k ? 'bg-emerald-500/15 text-emerald-300 ring-1 ring-emerald-500/30' : 'text-slate-400 hover:bg-ink-800 hover:text-slate-200'"
            @click="selectTab(t.k)"
          >
            {{ t.l }}
          </button>
        </template>
        <div class="mt-auto hidden border-t border-ink-700 pt-2 lg:block">
          <button class="w-full whitespace-nowrap rounded-xl px-3.5 py-2 text-left text-xs text-red-400/80 transition hover:bg-red-500/10 hover:text-red-300" @click="confirmReset = true">退出并重置本地界面</button>
        </div>
      </div>
    </aside>

    <!-- 右侧内容 -->
    <div class="min-w-0 w-full space-y-5">
      <!-- 页头：当前分组标题、说明与保存入口 -->
      <div class="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-ink-950/60 px-4 py-3 ring-1 ring-ink-700">
        <div>
          <h1 class="flex items-center gap-2 text-base font-semibold text-white">
            {{ currentTabMeta?.l }}
            <span v-if="formDirty" class="chip bg-amber-500/10 text-amber-300 ring-1 ring-amber-500/30">有未保存的修改</span>
          </h1>
          <p class="mt-0.5 text-xs text-slate-500">{{ currentTabMeta?.d }}</p>
        </div>
        <button v-if="formTabKeys.includes(tab)" class="btn-primary min-w-28" :disabled="app.loading || !app.isAdmin" @click="save">
          <svg v-if="saved" viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" /></svg>
          {{ saved ? '已保存' : '保存设置' }}
        </button>
      </div>
      <!-- 网络默认值 -->
      <section v-if="tab === 'net'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">网络默认值</h2>
        <p class="mt-0.5 text-xs text-slate-500">新建网络/接口时的默认参数</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2 2xl:grid-cols-3">
          <div><label class="label">DNS 服务器</label><input v-model="form.netDefaults.dns" class="input font-mono" /></div>
          <div><label class="label">监听端口 (UDP)</label><input v-model.number="form.netDefaults.port" type="number" class="input font-mono" /></div>
          <div><label class="label">MTU</label><input v-model.number="form.netDefaults.mtu" type="number" class="input font-mono" /></div>
          <div><label class="label">Persistent Keepalive (s)</label><input v-model.number="form.netDefaults.keepalive" type="number" class="input font-mono" /></div>
          <div>
            <label class="label">默认拓扑</label>
            <select v-model="form.netDefaults.defaultTopology" class="input">
              <option value="full-mesh">Full Mesh（全网互联）</option>
              <option value="hub-spoke">Hub-Spoke（星型）</option>
              <option value="custom">Custom（手动选择）</option>
            </select>
          </div>
        </div>
      </section>

      <!-- 状态判定 -->
      <section v-else-if="tab === 'status'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">状态判定</h2>
        <p class="mt-0.5 text-xs text-slate-500">链路绿/黄/红/灰的判定阈值</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3 2xl:gap-6">
          <div>
            <label class="label">Agent 离线超时（秒）</label>
            <input v-model.number="form.statusRules.agentOfflineSec" type="number" class="input font-mono" />
            <p class="mt-1 text-[11px] text-slate-600">超过该时间未上报 → 离线，链路灰色</p>
          </div>
          <div>
            <label class="label">握手超时（秒）</label>
            <input v-model.number="form.statusRules.handshakeSec" type="number" class="input font-mono" />
            <p class="mt-1 text-[11px] text-slate-600">最近握手超过该值 → 黄色；3 分钟内成功 → 绿色前提</p>
          </div>
          <div>
            <label class="label">红线失败次数</label>
            <input v-model.number="form.statusRules.redFailCount" type="number" class="input font-mono" />
            <p class="mt-1 text-[11px] text-slate-600">双方在线但连续探测失败达到该次数 → 红色</p>
          </div>
        </div>
      </section>

      <!-- 数据采集 -->
      <section v-else-if="tab === 'collect'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">数据采集</h2>
        <p class="mt-0.5 text-xs text-slate-500">Agent 上报与连通探测周期</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3 2xl:gap-6">
          <div><label class="label">上报周期（秒）</label><input v-model.number="form.collect.reportSec" type="number" min="5" class="input font-mono" /></div>
          <div><label class="label">探测周期（秒）</label><input v-model.number="form.collect.probeSec" type="number" min="5" class="input font-mono" /></div>
          <div><label class="label">地图刷新周期（秒）</label><input v-model.number="form.collect.mapRefreshSec" type="number" min="5" class="input font-mono" /></div>
        </div>
      </section>

      <!-- 流量保留 -->
      <section v-else-if="tab === 'retention'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">流量保留</h2>
        <p class="mt-0.5 text-xs text-slate-500">分层保留策略：原始数据用于 5 分钟到 24 小时曲线，小时聚合用于 7 天，日聚合用于 30 天</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3 2xl:gap-6">
          <div><label class="label">原始数据（天）</label><input v-model.number="form.retention.rawDays" type="number" min="1" class="input font-mono" /></div>
          <div><label class="label">小时聚合（天）</label><input v-model.number="form.retention.hourlyDays" type="number" min="1" class="input font-mono" /></div>
          <div><label class="label">日聚合（天）</label><input v-model.number="form.retention.dailyDays" type="number" min="1" class="input font-mono" /></div>
        </div>
      </section>

      <!-- GeoIP -->
      <section v-else-if="tab === 'geoip'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">GeoIP</h2>
        <p class="mt-0.5 text-xs text-slate-500">通过 Endpoint 公网 IP 解析地理位置；隧道私网地址不用于定位</p>
        <div class="mt-4 space-y-4">
          <div>
            <label class="label">数据库路径（MaxMind GeoLite2-City.mmdb）</label>
            <div class="flex flex-col gap-2.5 sm:flex-row">
              <input v-model="geoDbPath" class="input min-w-0 flex-1 font-mono" placeholder="GeoLite2-City.mmdb" @keyup.enter="saveGeoDbPath" />
              <button class="btn-primary shrink-0" :disabled="geoSaving || !geoDbPath.trim() || geoDbPath.trim() === mesh.geoip.dbPath" @click="saveGeoDbPath">{{ geoSaving ? '正在加载…' : '保存并重载' }}</button>
            </div>
            <p class="mt-1.5 text-[11px] leading-5 text-slate-500">填写 WireMesh 服务端能够访问的文件路径。相对路径会自动转换为绝对路径；Docker 部署建议将文件挂载到 <span class="font-mono text-slate-400">/data/GeoLite2-City.mmdb</span>。</p>
            <p v-if="mesh.geoip.dbPath" class="mt-1 break-all font-mono text-[11px] text-cyan-400/80">当前已加载：{{ mesh.geoip.dbPath }}</p>
          </div>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-3 2xl:gap-5">
            <div class="rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600"><p class="text-xs text-slate-500">版本</p><p class="mt-0.5 font-mono text-sm text-slate-200">{{ mesh.geoip.version || '未加载' }}</p></div>
            <div class="rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600"><p class="text-xs text-slate-500">条目数</p><p class="mt-0.5 font-mono text-sm text-slate-200">{{ mesh.geoip.entryCount.toLocaleString() }}</p></div>
            <div class="rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600"><p class="text-xs text-slate-500">更新时间</p><p class="mt-0.5 text-sm text-slate-200">{{ mesh.geoip.updatedAt ? ago(mesh.geoip.updatedAt) : '未加载' }}</p></div>
          </div>
        </div>
        <div class="mt-4 flex gap-2.5">
          <button class="btn-ghost" :disabled="geoReloading || !mesh.geoip.dbPath" @click="reloadGeoDb">{{ geoReloading ? '正在重新加载…' : '重新加载数据库' }}</button>
        </div>
        <!-- 重载进度条 -->
        <div v-if="geoReloading || geoReloadProgress > 0" class="mt-3">
          <div class="flex items-center gap-2.5">
            <div class="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-ink-700">
              <div class="h-full rounded-full bg-cyan-400 transition-all duration-150" :style="{ width: geoReloadProgress + '%' }"></div>
            </div>
            <span class="shrink-0 font-mono text-[11px] text-cyan-300">{{ geoReloadProgress }}%</span>
          </div>
          <p class="mt-1 text-[11px] text-slate-500">正在解析 GeoIP 数据库，大文件可能需要几秒…</p>
        </div>
        <div class="mt-5 border-t border-ink-700 pt-4">
          <label class="label">定位测试</label>
          <div class="flex gap-2.5">
            <input v-model="geoTestIP" class="input font-mono" placeholder="输入 IPv4 地址" @keyup.enter="testGeoIP" />
            <button class="btn-primary shrink-0" @click="testGeoIP">测试</button>
          </div>
          <p v-if="geoTestResult" class="mt-2.5 rounded-lg bg-ink-800/70 px-3 py-2 font-mono text-xs text-emerald-300 ring-1 ring-ink-600">{{ geoTestResult }}</p>
        </div>
      </section>

      <!-- Agent -->
      <section v-else-if="tab === 'agent'" class="panel p-5">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 class="text-sm font-semibold text-white">Agent</h2>
            <p class="mt-0.5 max-w-xl text-xs leading-relaxed text-slate-500">节点 Agent 使用后端签发的一次性注册令牌接入（浏览器不保存全局 Token）；本页汇总服务端更新包状态与各节点版本。</p>
          </div>
          <button class="btn-primary" @click="router.push({ name: 'nodes' })">前往节点列表接入 / 更新</button>
        </div>

        <!-- 接入默认开关：默认不强制 mTLS，HTTP 部署开箱即用 -->
        <div class="mt-4 flex items-start justify-between gap-4 rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600">
          <div class="min-w-0">
            <p class="text-xs font-semibold text-slate-300">接入命令默认启用 mTLS</p>
            <p class="mt-1 text-[11px] leading-5 text-slate-500">
              关闭（默认）：控制台生成的接入命令不强制客户端证书，HTTP/HTTPS 部署复制即用。
              开启：接入命令默认带 <code class="text-cyan-300">--mtls</code>，Agent 用注册签发的证书双向认证；
              修改此项后，<span class="text-amber-300">已部署的客户端需重新生成接入命令并更新</span>。
            </p>
          </div>
          <button
            class="relative h-6 w-11 shrink-0 rounded-full transition"
            :class="form.agent.defaultMTLS ? 'bg-emerald-500' : 'bg-ink-600'"
            :disabled="!app.isAdmin"
            :title="form.agent.defaultMTLS ? '点击关闭' : '点击开启'"
            :aria-label="form.agent.defaultMTLS ? '关闭默认 mTLS' : '开启默认 mTLS'"
            @click="form.agent.defaultMTLS = !form.agent.defaultMTLS"
          >
            <span class="absolute top-0.5 h-5 w-5 rounded-full bg-white transition-all" :class="form.agent.defaultMTLS ? 'left-[22px]' : 'left-0.5'"></span>
          </button>
        </div>

        <!-- 更新包状态 + 版本分布 -->
        <div class="mt-4 grid gap-4 sm:grid-cols-3">
          <div class="rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600 sm:col-span-1">
            <p class="text-xs text-slate-500">服务端更新包</p>
            <p class="mt-1 flex items-center gap-2 text-sm font-semibold" :class="agentManifest.available ? 'text-emerald-300' : 'text-amber-300'">
              {{ agentManifest.available ? '已配置' : '未配置' }}
              <span v-if="agentManifest.version" class="font-mono text-xs text-slate-400">v{{ agentManifest.version }}</span>
            </p>
            <p v-if="!agentManifest.available" class="mt-1.5 text-[11px] leading-relaxed text-slate-500">
              设置 <code class="text-cyan-300">WIREMESH_AGENT_BINARY</code> 与 <code class="text-cyan-300">WIREMESH_AGENT_VERSION</code> 环境变量后可远程更新 Agent{{ agentManifest.error ? '：' + agentManifest.error : '' }}。
            </p>
            <p v-else class="mt-1.5 text-[11px] text-slate-500">支持远程自更新的最低版本：{{ agentManifest.min_agent_version || '0.3.6' }}</p>
          </div>
          <div class="rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600">
            <p class="text-xs text-slate-500">可远程更新</p>
            <p class="mt-1 text-2xl font-bold text-amber-300">{{ updatableCount }}</p>
            <p class="mt-1 text-[11px] text-slate-500">版本落后于更新包、可直接下发更新的节点</p>
          </div>
          <div class="rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600">
            <p class="text-xs text-slate-500">需手动升级</p>
            <p class="mt-1 text-2xl font-bold text-red-400">{{ manualOnlyCount }}</p>
            <p class="mt-1 text-[11px] text-slate-500">版本过旧或无法识别，需在节点机器上重新执行接入脚本</p>
          </div>
        </div>

        <!-- 版本分布 -->
        <div class="mt-4 rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600">
          <p class="text-xs font-semibold text-slate-300">版本分布</p>
          <div class="mt-2.5 flex flex-wrap gap-1.5">
            <span v-for="[version, count] in agentVersionGroups" :key="version" class="chip bg-ink-900/70 font-mono text-slate-300 ring-1 ring-ink-600">
              {{ version }} × {{ count }}
            </span>
            <span v-if="!agentVersionGroups.length" class="text-[11px] text-slate-600">暂无已接入节点</span>
          </div>
        </div>

        <!-- 节点版本明细 -->
        <div class="mt-4">
          <p class="mb-2 text-xs font-semibold text-slate-300">节点版本明细（{{ mesh.agents.length }}）</p>
          <div class="space-y-1.5">
            <div v-for="agent in mesh.agents" :key="agent.id" class="flex items-center gap-3 rounded-xl bg-ink-800/60 px-4 py-2.5 ring-1 ring-ink-600">
              <span class="h-2 w-2 shrink-0 rounded-full" :class="!agent.enabled ? 'bg-slate-600' : agent.status === 'online' ? 'bg-emerald-400' : 'bg-slate-500'"></span>
              <p class="min-w-0 flex-1 truncate text-sm text-slate-200">{{ agent.name }}</p>
              <span class="shrink-0 font-mono text-xs text-slate-400">{{ agent.version || '未知版本' }}</span>
              <span v-if="!agentNodeStatus(agent.id)" class="chip shrink-0 bg-slate-500/10 text-slate-400 ring-1 ring-slate-500/30">待评估</span>
              <span v-else-if="agentNodeStatus(agent.id)!.needs_update" class="chip shrink-0 bg-amber-500/10 text-amber-300 ring-1 ring-amber-500/30">可更新</span>
              <span v-else-if="agentNodeStatus(agent.id)!.reason" class="chip shrink-0 bg-red-500/10 text-red-300 ring-1 ring-red-500/30" :title="agentNodeStatus(agent.id)!.reason">需手动升级</span>
              <span v-else class="chip shrink-0 bg-emerald-500/10 text-emerald-300 ring-1 ring-emerald-500/30">已是最新</span>
            </div>
            <p v-if="!mesh.agents.length" class="rounded-xl bg-ink-800/40 px-4 py-6 text-center text-xs text-slate-500 ring-1 ring-ink-700">暂无已接入节点，请到节点列表生成接入命令</p>
          </div>
        </div>
      </section>

      <!-- 通知配置 -->
      <section v-else-if="tab === 'notify'">
        <div class="grid grid-cols-1 items-start gap-5 xl:grid-cols-[minmax(22rem,0.9fr)_minmax(0,1.1fr)] 2xl:grid-cols-[minmax(26rem,0.85fr)_minmax(0,1.15fr)]">
          <div class="min-w-0 space-y-5">
            <div class="panel p-5">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h2 class="text-sm font-semibold text-white">通知渠道</h2>
                <p class="mt-1 text-xs leading-5 text-slate-500">每个渠道独立保存连接参数、接收目标和消息模板，密钥不会返回浏览器。</p>
              </div>
              <span class="chip shrink-0 bg-emerald-500/10 text-emerald-300 ring-1 ring-emerald-500/30">{{ mesh.notifyChannels.length }} 个渠道</span>
            </div>
            <div class="mt-4 max-h-[30rem] space-y-2.5 overflow-y-auto pr-1">
              <div v-for="c in mesh.notifyChannels" :key="c.id" class="rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600">
                <div class="flex flex-wrap items-center gap-2.5">
                  <span class="chip ring-1" :class="channelTypeMeta[c.type].c">{{ channelTypeMeta[c.type].l }}</span>
                  <p class="min-w-0 flex-1 truncate text-sm font-medium text-slate-200">{{ c.name }}</p>
                  <button class="relative h-5 w-9 shrink-0 rounded-full transition" :class="c.enabled ? 'bg-emerald-500' : 'bg-ink-600'" :title="c.enabled ? '点击停用' : '点击启用'" :aria-label="c.enabled ? '停用渠道' : '启用渠道'" :disabled="!app.isAdmin" @click="mesh.updateNotifyChannel(c.id, { enabled: !c.enabled }, app.username)">
                    <span class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all" :class="c.enabled ? 'left-[18px]' : 'left-0.5'"></span>
                  </button>
                </div>
                <p class="mt-2 break-all font-mono text-[11px] leading-5 text-slate-500">{{ channelConfigSummary(c) }}</p>
                <p class="mt-1 text-[11px] text-slate-500">范围：<span class="text-slate-300">{{ channelAgentsLabel(c) }}</span><span class="ml-2" :class="c.enabled ? 'text-emerald-400' : 'text-slate-600'">{{ c.enabled ? '· 已启用' : '· 已停用' }}</span></p>
                <div class="mt-3 flex flex-wrap gap-2">
                  <button class="chip bg-slate-500/10 text-slate-300 ring-1 ring-slate-500/30 hover:bg-ink-700" :disabled="!app.isAdmin" @click="editChannel(c)">编辑配置</button>
                  <button class="chip bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30 hover:bg-cyan-500/20" :disabled="!app.isAdmin" @click="mesh.testNotifyChannel(c.id, app.username)">发送测试</button>
                  <button class="chip bg-red-500/10 text-red-300 ring-1 ring-red-500/30 hover:bg-red-500/20" :disabled="!app.isAdmin" @click="confirmDeleteChannel(c)">删除</button>
                </div>
              </div>
              <p v-if="!mesh.notifyChannels.length" class="rounded-xl bg-ink-800/40 py-8 text-center text-xs text-slate-500 ring-1 ring-ink-700">暂无通知渠道，请在右侧创建第一个渠道。</p>
            </div>
          </div>

          <div class="panel p-5">
            <div class="flex flex-wrap items-center justify-between gap-2"><div><h2 class="text-sm font-semibold text-white">通知记录 <span class="ml-1 text-xs font-normal text-slate-500">{{ filteredNotifyLogs.length }}/{{ mesh.notifyLogs.length }} 条</span></h2><p class="mt-1 text-xs text-slate-500">测试和实际发送结果均由后端记录。</p></div>
              <select v-model="notifyLogFilter" class="input !w-28 !py-1.5 !text-xs">
                <option value="all">全部状态</option>
                <option value="sent">已送达</option>
                <option value="failed">失败</option>
                <option value="test">测试</option>
              </select>
            </div>
            <div class="mt-4 max-h-[32rem] space-y-2 overflow-y-auto pr-1">
              <div v-for="log in filteredNotifyLogs" :key="log.id" class="flex items-start gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
                <span class="chip mt-0.5 shrink-0 ring-1" :class="channelTypeMeta[log.channelType]?.c ?? 'bg-slate-500/10 text-slate-400 ring-slate-500/30'">{{ channelTypeMeta[log.channelType]?.l ?? log.channelType }}</span>
                <div class="min-w-0 flex-1"><p class="text-xs text-slate-200">{{ log.message }}</p><p class="mt-1 text-[11px] text-slate-500">{{ log.channelName }} · {{ fmtDateTime(log.time) }}</p></div>
                <span class="chip shrink-0" :class="log.status === 'failed' ? 'bg-red-500/10 text-red-300 ring-1 ring-red-500/30' : log.status === 'test' ? 'bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30' : 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/30'">{{ log.status === 'failed' ? '失败' : log.status === 'test' ? '测试' : '已送达' }}</span>
              </div>
              <p v-if="!mesh.notifyLogs.length" class="py-6 text-center text-xs text-slate-500">暂无通知记录</p>
              <p v-else-if="!filteredNotifyLogs.length" class="py-6 text-center text-xs text-slate-500">当前筛选条件下没有记录</p>
              <button v-if="mesh.notifyLogsHasMore" class="mt-2 w-full rounded-xl bg-ink-800/60 px-3 py-2 text-center text-xs text-cyan-300 ring-1 ring-ink-600 transition hover:bg-ink-800 disabled:text-slate-600" :disabled="mesh.notifyLogsLoading" @click="mesh.loadMoreNotifyLogs()">
                {{ mesh.notifyLogsLoading ? '加载中…' : '加载更多（已加载 ' + mesh.notifyLogs.length + ' 条）' }}
              </button>
              <p v-else-if="mesh.notifyLogs.length" class="mt-2 text-center text-[11px] text-slate-600">共 {{ mesh.notifyLogs.length }} 条记录</p>
            </div>
          </div>
          </div>

          <div class="panel p-5">
            <div class="flex items-start justify-between gap-3">
              <div>
                <h2 class="text-sm font-semibold text-white">{{ editingChannelId ? '编辑通知渠道' : '新建通知渠道' }}</h2>
                <p class="mt-1 text-xs text-slate-500">{{ channelTypeMeta[channelForm.type].description }}</p>
              </div>
              <button v-if="editingChannelId" class="btn-ghost shrink-0" @click="resetChannelForm">取消编辑</button>
            </div>

            <div class="mt-5 grid grid-cols-1 gap-4 md:grid-cols-2">
              <div><label class="label">渠道名称</label><input v-model="channelForm.name" class="input" placeholder="例如：生产环境值班群" /></div>
              <div><label class="label">渠道类型</label><select v-model="channelForm.type" class="input" @change="changeChannelType"><option v-for="(meta, type) in channelTypeMeta" :key="type" :value="type">{{ meta.l }}</option></select></div>
            </div>

            <!-- Webhook -->
            <div v-if="channelForm.type === 'webhook'" class="mt-5 rounded-xl bg-ink-900/55 p-4 ring-1 ring-ink-700">
              <p class="text-xs font-semibold text-slate-300">HTTP 请求</p>
              <div class="mt-3 grid grid-cols-1 gap-4 md:grid-cols-2">
                <div class="md:col-span-2"><label class="label">Webhook URL</label><input v-model="channelForm.config.url" type="password" autocomplete="new-password" class="input font-mono" :placeholder="configuredPlaceholder('url', 'https://example.com/hooks/wiremesh')" /></div>
                <div><label class="label">HTTP 方法</label><select v-model="channelForm.config.method" class="input"><option>POST</option><option>PUT</option><option>PATCH</option></select></div>
                <div><label class="label">Content-Type</label><input v-model="channelForm.config.contentType" class="input font-mono" placeholder="application/json" /></div>
                <div><label class="label">签名 / 鉴权</label><select v-model="channelForm.config.signatureType" class="input"><option value="none">无</option><option value="hmac-sha256">HMAC-SHA256</option><option value="bearer">Bearer Token</option></select></div>
                <div><label class="label">签名密钥 / Token</label><input v-model="channelForm.config.secret" type="password" autocomplete="new-password" class="input font-mono" :disabled="channelForm.config.signatureType === 'none'" :placeholder="configuredPlaceholder('secret', '仅在启用签名时必填')" /></div>
                <div><label class="label">超时时间（秒）</label><input v-model.number="channelForm.config.timeoutSec" type="number" min="1" max="60" class="input font-mono" /></div>
                <label class="flex items-center gap-2 self-end pb-2 text-xs text-slate-400"><input v-model="channelForm.config.allowPrivate" type="checkbox" class="accent-emerald-500" />允许访问内网或本机地址</label>
              </div>
              <div class="mt-4 flex items-center justify-between"><label class="label mb-0">自定义请求头</label><button class="chip bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30" @click="addWebhookHeader">添加请求头</button></div>
              <div v-if="channelForm.config.headers?.length" class="mt-2 space-y-2">
                <div v-for="(header, index) in channelForm.config.headers" :key="index" class="grid grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)_auto] gap-2">
                  <input v-model="header.name" class="input font-mono" placeholder="X-Header-Name" />
                  <input v-model="header.value" type="password" autocomplete="new-password" class="input font-mono" :placeholder="header.valueConfigured ? '已保存，留空保持不变' : '请求头值'" />
                  <button class="rounded-lg px-3 text-xs text-red-400 ring-1 ring-red-500/30 hover:bg-red-500/10" @click="removeWebhookHeader(index)">移除</button>
                </div>
              </div>
            </div>

            <!-- 钉钉 / 企业微信 / 飞书 -->
            <div v-else-if="['dingtalk', 'wecom', 'feishu'].includes(channelForm.type)" class="mt-5 rounded-xl bg-ink-900/55 p-4 ring-1 ring-ink-700">
              <p class="text-xs font-semibold text-slate-300">机器人配置</p>
              <div class="mt-3 grid grid-cols-1 gap-4 md:grid-cols-2">
                <div class="md:col-span-2"><label class="label">机器人 Webhook URL</label><input v-model="channelForm.config.url" type="password" autocomplete="new-password" class="input font-mono" :placeholder="configuredPlaceholder('url', '粘贴机器人 Webhook 地址')" /></div>
                <div><label class="label">消息类型</label><select v-model="channelForm.config.messageType" class="input"><option value="text">纯文本</option><option v-if="channelForm.type !== 'feishu'" value="markdown">Markdown</option><option v-if="channelForm.type === 'feishu'" value="post">富文本 Post</option></select></div>
                <div><label class="label">超时时间（秒）</label><input v-model.number="channelForm.config.timeoutSec" type="number" min="1" max="60" class="input font-mono" /></div>
                <div v-if="channelForm.type === 'dingtalk' || channelForm.type === 'feishu'" class="md:col-span-2"><label class="label">加签密钥 Secret（可选）</label><input v-model="channelForm.config.secret" type="password" autocomplete="new-password" class="input font-mono" :placeholder="configuredPlaceholder('secret', '未启用加签时留空')" /></div>
                <div v-if="channelForm.type === 'dingtalk' || channelForm.type === 'wecom'"><label class="label">@ 手机号（每行一个）</label><textarea v-model="channelForm.atMobilesText" rows="3" class="input resize-y font-mono" :placeholder="channelForm.config.atMobilesConfigured ? '已安全保存 ' + (channelForm.config.atMobileCount || 0) + ' 个手机号，留空保持不变' : '13800138000'"></textarea></div>
                <div v-if="channelForm.type === 'wecom'"><label class="label">@ 用户 ID（每行一个）</label><textarea v-model="channelForm.atUserIdsText" rows="3" class="input resize-y font-mono" :placeholder="channelForm.config.atUserIdsConfigured ? '已安全保存 ' + (channelForm.config.atUserIdCount || 0) + ' 个用户 ID，留空保持不变' : 'zhangsan'"></textarea></div>
                <label class="flex items-center gap-2 text-xs text-slate-400"><input v-model="channelForm.config.atAll" type="checkbox" class="accent-emerald-500" />@ 所有人</label>
                <label class="flex items-center gap-2 text-xs text-slate-400"><input v-model="channelForm.config.allowPrivate" type="checkbox" class="accent-emerald-500" />允许内网机器人地址</label>
              </div>
            </div>

            <!-- Telegram -->
            <div v-else-if="channelForm.type === 'telegram'" class="mt-5 rounded-xl bg-ink-900/55 p-4 ring-1 ring-ink-700">
              <p class="text-xs font-semibold text-slate-300">Telegram Bot API</p>
              <div class="mt-3 grid grid-cols-1 gap-4 md:grid-cols-2">
                <div><label class="label">Bot Token</label><input v-model="channelForm.config.botToken" type="password" autocomplete="new-password" class="input font-mono" :placeholder="configuredPlaceholder('botToken', '123456:AA...')" /></div>
                <div><label class="label">Chat ID</label><input v-model="channelForm.config.chatId" type="password" autocomplete="new-password" class="input font-mono" :placeholder="configuredPlaceholder('chatId', '-1001234567890')" /></div>
                <div><label class="label">Topic / Thread ID（可选）</label><input v-model="channelForm.config.threadId" class="input font-mono" placeholder="123" /></div>
                <div><label class="label">解析模式</label><select v-model="channelForm.config.parseMode" class="input"><option value="">纯文本</option><option value="HTML">HTML</option><option value="MarkdownV2">MarkdownV2</option></select></div>
                <div><label class="label">超时时间（秒）</label><input v-model.number="channelForm.config.timeoutSec" type="number" min="1" max="60" class="input font-mono" /></div>
                <label class="flex items-center gap-2 text-xs text-slate-400 md:col-span-2"><input v-model="channelForm.config.useProxy" type="checkbox" class="accent-emerald-500" />通过代理访问 Telegram Bot API</label>
                <div v-if="channelForm.config.useProxy" class="md:col-span-2">
                  <label class="label">代理地址</label>
                  <input v-model="channelForm.config.proxyUrl" type="password" autocomplete="new-password" class="input font-mono" :placeholder="configuredPlaceholder('proxyUrl', 'http://127.0.0.1:7890 或 socks5://127.0.0.1:1080')" />
                  <p class="mt-1.5 text-[11px] leading-5 text-slate-500">支持 HTTP、HTTPS、SOCKS5 和 SOCKS5H；需要认证时可使用 <span class="font-mono text-slate-400">scheme://user:password@host:port</span>。代理地址会加密保存。</p>
                </div>
                <label class="flex items-center gap-2 text-xs text-slate-400"><input v-model="channelForm.config.disableWebPagePreview" type="checkbox" class="accent-emerald-500" />关闭网页预览</label>
                <label class="flex items-center gap-2 text-xs text-slate-400"><input v-model="channelForm.config.disableNotification" type="checkbox" class="accent-emerald-500" />静默发送</label>
              </div>
            </div>

            <!-- Email -->
            <div v-else class="mt-5 rounded-xl bg-ink-900/55 p-4 ring-1 ring-ink-700">
              <p class="text-xs font-semibold text-slate-300">SMTP 与收件人</p>
              <div class="mt-3 grid grid-cols-1 gap-4 md:grid-cols-2">
                <div><label class="label">SMTP 主机</label><input v-model="channelForm.config.smtpHost" class="input font-mono" placeholder="smtp.example.com" /></div>
                <div><label class="label">SMTP 端口</label><input v-model.number="channelForm.config.smtpPort" type="number" min="1" max="65535" class="input font-mono" /></div>
                <div><label class="label">用户名（可选）</label><input v-model="channelForm.config.username" autocomplete="username" class="input font-mono" /></div>
                <div><label class="label">密码 / 授权码</label><input v-model="channelForm.config.password" type="password" autocomplete="new-password" class="input font-mono" :placeholder="configuredPlaceholder('password', 'SMTP 密码或授权码')" /></div>
                <div><label class="label">发件人地址</label><input v-model="channelForm.config.fromAddress" type="email" class="input font-mono" placeholder="wiremesh@example.com" /></div>
                <div><label class="label">发件人名称</label><input v-model="channelForm.config.fromName" class="input" placeholder="WireMesh" /></div>
                <div><label class="label">加密方式</label><select v-model="channelForm.config.encryption" class="input"><option value="starttls">STARTTLS</option><option value="tls">TLS / SMTPS</option><option value="none">无加密</option></select></div>
                <div><label class="label">超时时间（秒）</label><input v-model.number="channelForm.config.timeoutSec" type="number" min="1" max="60" class="input font-mono" /></div>
                <div><label class="label">收件人（每行一个）</label><textarea v-model="channelForm.recipientsText" rows="3" class="input resize-y font-mono" :placeholder="configuredPlaceholder('recipients', 'ops@example.com')"></textarea><p v-if="channelForm.config.recipientCount" class="mt-1 text-[11px] text-slate-500">当前已安全保存 {{ channelForm.config.recipientCount }} 个收件人。</p></div>
                <div><label class="label">抄送（每行一个）</label><textarea v-model="channelForm.ccText" rows="3" class="input resize-y font-mono" :placeholder="configuredPlaceholder('cc', 'manager@example.com')"></textarea><p v-if="channelForm.config.ccCount" class="mt-1 text-[11px] text-slate-500">当前已安全保存 {{ channelForm.config.ccCount }} 个抄送地址。</p></div>
                <label class="flex items-center gap-2 text-xs text-slate-400"><input v-model="channelForm.config.allowPrivate" type="checkbox" class="accent-emerald-500" />允许连接内网 SMTP</label>
                <label class="flex items-center gap-2 text-xs text-amber-400"><input v-model="channelForm.config.skipTlsVerify" type="checkbox" class="accent-amber-500" />跳过 TLS 证书校验（不推荐）</label>
              </div>
            </div>

            <!-- 模板 -->
            <div class="mt-5 rounded-xl bg-ink-900/55 p-4 ring-1 ring-ink-700">
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div><p class="text-xs font-semibold text-slate-300">自定义通知模板</p><p class="mt-1 text-[11px] text-slate-500">测试通知和实际通知都会在后端使用此模板渲染。</p></div>
                <button class="chip bg-slate-500/10 text-slate-300 ring-1 ring-slate-500/30" @click="restoreDefaultTemplate">恢复默认模板</button>
              </div>
              <div v-if="channelForm.type === 'email'" class="mt-3"><label class="label">邮件主题模板</label><input v-model="channelForm.subjectTemplate" class="input font-mono" /></div>
              <div class="mt-3"><label class="label">{{ channelForm.type === 'webhook' ? '请求正文模板' : '消息正文模板' }}</label><textarea v-model="channelForm.template" rows="9" class="input resize-y font-mono text-xs leading-5"></textarea></div>
              <div class="mt-3"><p class="mb-2 text-[11px] text-slate-500">点击插入可用变量；Webhook JSON 可使用 <code class="text-cyan-300">{{ webhookJSONExample }}</code> 安全生成 JSON 字符串。</p><div class="flex flex-wrap gap-1.5"><button v-for="variable in templateVariables" :key="variable.key" class="rounded-lg bg-ink-800 px-2 py-1 font-mono text-[10px] text-slate-400 ring-1 ring-ink-600 hover:text-cyan-300" :title="variable.label" @click="insertTemplateVariable(variable.key)">{{ variable.key }}</button></div></div>
            </div>

            <!-- 绑定范围 -->
            <div class="mt-5">
              <label class="label">绑定节点（节点事件触发通知）</label>
              <div class="flex gap-2"><button class="rounded-xl px-4 py-2 text-xs font-medium ring-1 transition" :class="channelForm.scope === 'all' ? 'bg-emerald-500/15 text-emerald-300 ring-emerald-500/40' : 'bg-ink-800 text-slate-400 ring-ink-600'" @click="channelForm.scope = 'all'">全部节点</button><button class="rounded-xl px-4 py-2 text-xs font-medium ring-1 transition" :class="channelForm.scope === 'custom' ? 'bg-emerald-500/15 text-emerald-300 ring-emerald-500/40' : 'bg-ink-800 text-slate-400 ring-ink-600'" @click="channelForm.scope = 'custom'">指定节点</button></div>
              <div v-if="channelForm.scope === 'custom'" class="mt-2.5 grid max-h-44 grid-cols-1 gap-1.5 overflow-y-auto rounded-xl bg-ink-950/50 p-3 ring-1 ring-ink-600 sm:grid-cols-2 2xl:grid-cols-3"><label v-for="agent in mesh.agents" :key="agent.id" class="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-slate-300 hover:bg-ink-800"><input type="checkbox" class="accent-emerald-500" :checked="channelForm.agents.includes(agent.id)" @change="toggleChannelAgent(agent.id)" /><span class="h-1.5 w-1.5 rounded-full" :class="agent.status === 'online' ? 'bg-emerald-400' : 'bg-slate-600'"></span><span class="truncate">{{ agent.name }}</span></label></div>
            </div>
            <p v-if="channelFormError" class="mt-4 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ channelFormError }}</p>
            <div class="mt-5 flex justify-end"><button class="btn-primary" :disabled="!app.isAdmin" @click="saveChannel">{{ editingChannelId ? '保存渠道配置' : '创建通知渠道' }}</button></div>
          </div>
        </div>

      </section>

      <!-- 项目与网络 -->
      <section v-else-if="tab === 'project'" class="grid grid-cols-1 items-start gap-5">
        <div class="panel p-5">
          <h2 class="text-sm font-semibold text-white">项目</h2>
          <div class="mt-3 space-y-2">
            <div v-for="p in mesh.projects" :key="p.id" class="flex items-center justify-between rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
              <div>
                <p class="text-sm font-medium text-slate-200">{{ p.name }}</p>
                <p class="text-xs text-slate-500">{{ p.desc }}</p>
              </div>
              <span class="chip bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">{{ mesh.networks.filter((n) => n.projectId === p.id).length }} 个网络</span>
            </div>
            <p v-if="!mesh.projects.length" class="rounded-xl bg-ink-800/60 px-4 py-6 text-center text-xs text-slate-500 ring-1 ring-ink-600">暂无项目，请先在下方创建一个</p>
          </div>
          <div class="mt-4 flex flex-col gap-2.5 border-t border-ink-700 pt-4 sm:flex-row sm:items-end">
            <div v-if="!app.isAdmin" class="w-full text-xs text-amber-400 sm:mb-2">只有管理员可以创建项目</div>
            <div class="flex-1"><label class="label">项目名称</label><input v-model="newProject.name" class="input" placeholder="如：生产环境" /></div>
            <div class="flex-1"><label class="label">描述</label><input v-model="newProject.desc" class="input" placeholder="可选" /></div>
            <button class="btn-primary shrink-0" :disabled="!app.isAdmin || !newProject.name.trim()" @click="addProject">新建项目</button>
          </div>
        </div>
        <div class="panel p-5">
          <h2 class="text-sm font-semibold text-white">网络与拓扑</h2>
          <p class="mt-0.5 text-xs text-slate-500">Full Mesh / Hub-Spoke 自动互联不可逐个排除；Custom 通过选择器手动指定</p>
          <div class="mt-3 space-y-2">
            <div v-for="n in mesh.networks" :key="n.id" class="flex items-center gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
              <div class="min-w-0 flex-1">
                <p class="text-sm font-medium text-slate-200">{{ n.name }} <span class="ml-1 font-mono text-xs text-slate-500">{{ n.cidr }}</span></p>
                <p class="text-xs text-slate-500">{{ mesh.projects.find((p) => p.id === n.projectId)?.name }}</p>
              </div>
              <span class="chip bg-violet-500/10 text-violet-300 ring-1 ring-violet-500/30">{{ topologyLabel[n.topology] }}</span>
              <button v-if="n.topology === 'custom'" class="chip bg-emerald-500/10 text-emerald-300 ring-1 ring-emerald-500/30 transition hover:bg-emerald-500/20" @click="customPeerNetwork = n.id">
                配置 Peer
              </button>
            </div>
            <p v-if="!mesh.networks.length" class="rounded-xl bg-ink-800/60 px-4 py-6 text-center text-xs text-slate-500 ring-1 ring-ink-600">暂无网络，请先在下方创建一个</p>
          </div>
          <div class="mt-4 border-t border-ink-700 pt-4">
            <p v-if="!mesh.projects.length" class="mb-2.5 text-xs text-amber-400">请先创建项目，再在其中新建网络</p>
            <p v-else-if="!app.canOperate" class="mb-2.5 text-xs text-amber-400">当前账号没有创建网络的权限</p>
            <div class="grid grid-cols-1 gap-2.5 sm:grid-cols-2 xl:grid-cols-[1fr_1fr_1fr_10rem_auto] xl:items-end">
              <div>
                <label class="label">所属项目</label>
                <select v-model="newNetwork.projectId" class="input" :disabled="!mesh.projects.length">
                  <option v-for="p in mesh.projects" :key="p.id" :value="p.id">{{ p.name }}</option>
                </select>
              </div>
              <div><label class="label">网络名称</label><input v-model="newNetwork.name" class="input" placeholder="如：core-backbone" /></div>
              <div><label class="label">网段 CIDR</label><input v-model="newNetwork.cidr" class="input font-mono" placeholder="10.8.0.0/24" /></div>
              <div>
                <label class="label">拓扑</label>
                <select v-model="newNetwork.topology" class="input">
                  <option value="full-mesh">Full Mesh</option>
                  <option value="hub-spoke">Hub-Spoke</option>
                  <option value="custom">Custom</option>
                </select>
              </div>
              <button class="btn-primary shrink-0" :disabled="!canAddNetwork" @click="addNetwork">新建网络</button>
            </div>
          </div>
        </div>
      </section>

      <!-- 用户与权限 -->
      <section v-else-if="tab === 'users'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">用户与权限</h2>
        <p class="mt-0.5 text-xs text-slate-500">Viewer 只读 · Operator 可调整节点与 Peer · Admin 管理系统设置、用户与 IP 库</p>
        <div class="mt-4 rounded-xl border border-ink-700 bg-ink-900/50 p-4">
          <p class="text-xs font-semibold text-slate-300">修改我的密码</p>
          <div class="mt-3 grid grid-cols-1 items-end gap-3 sm:grid-cols-[1fr_1fr_1fr_auto]">
            <div><label class="label">旧密码</label><input v-model="passwordForm.oldPassword" type="password" autocomplete="current-password" class="input" /></div>
            <div><label class="label">新密码（至少 8 位）</label><input v-model="passwordForm.newPassword" type="password" autocomplete="new-password" class="input" /></div>
            <div><label class="label">确认新密码</label><input v-model="passwordForm.confirm" type="password" autocomplete="new-password" class="input" @keyup.enter="changeMyPassword" /></div>
            <button class="btn-secondary" :disabled="passwordSaving" @click="changeMyPassword">{{ passwordSaving ? '修改中…' : '修改密码' }}</button>
          </div>
          <div v-if="mfaEnabled" class="mt-2 max-w-xs">
            <label class="label">动态验证码（已启用 MFA）</label>
            <input v-model="passwordForm.otp" class="input font-mono" inputmode="numeric" maxlength="6" placeholder="6 位验证码" autocomplete="one-time-code" />
          </div>
          <p v-if="passwordError" class="mt-2 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ passwordError }}</p>
        </div>
        <div class="mt-4 space-y-2">
          <div v-for="u in mesh.users" :key="u.id" class="flex flex-wrap items-center gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600" :class="{ 'opacity-70': !u.active }">
            <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-xs font-bold ring-1" :class="roleMeta[u.role].c">{{ u.name.slice(0, 1).toUpperCase() }}</div>
            <div class="min-w-0 flex-1">
              <p class="text-sm font-medium text-slate-200">{{ u.name }} <span class="ml-1 text-xs text-slate-500">{{ u.email }}</span><span v-if="app.user?.id === u.id" class="chip ml-1.5 bg-emerald-500/10 text-emerald-300 ring-1 ring-emerald-500/30">当前账号</span></p>
              <p class="text-[11px]" :class="u.active ? 'text-slate-500' : 'text-amber-400'">{{ u.active ? '已激活' : '已禁用' }} · {{ u.lastLogin ? '最近登录 ' + ago(u.lastLogin) : '从未登录' }}</p>
            </div>
            <template v-if="app.isAdmin && app.user?.id !== u.id">
              <select class="input !w-28 !py-1.5 !text-xs" :value="u.role" @change="changeUserRole(u, ($event.target as HTMLSelectElement).value as UserAccount['role'])">
                <option value="viewer">Viewer</option>
                <option value="operator">Operator</option>
                <option value="admin">Admin</option>
              </select>
              <button class="relative h-5 w-9 shrink-0 rounded-full transition" :class="u.active ? 'bg-emerald-500' : 'bg-ink-600'" :title="u.active ? '点击停用' : '点击启用'" :aria-label="u.active ? '停用用户' : '启用用户'" @click="toggleUserActive(u)">
                <span class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all" :class="u.active ? 'left-[18px]' : 'left-0.5'"></span>
              </button>
              <button class="chip shrink-0 bg-red-500/10 text-red-300 ring-1 ring-red-500/30" @click="deleteUser(u)">删除</button>
            </template>
            <span v-else class="chip shrink-0 ring-1" :class="roleMeta[u.role].c">{{ roleMeta[u.role].l }}</span>
          </div>
        </div>
        <div class="mt-4 grid grid-cols-1 items-end gap-2.5 border-t border-ink-700 pt-4 sm:grid-cols-2 xl:grid-cols-[1fr_1fr_1fr_8rem_auto]">
          <div><label class="label">用户名</label><input v-model="newUser.name" class="input" /></div>
          <div><label class="label">邮箱</label><input v-model="newUser.email" type="email" class="input" /></div>
          <div><label class="label">初始密码</label><input v-model="newUser.password" type="password" minlength="8" class="input" placeholder="至少 8 位" /></div>
          <div><label class="label">角色</label>
            <select v-model="newUser.role" class="input">
              <option value="viewer">Viewer</option>
              <option value="operator">Operator</option>
              <option value="admin">Admin</option>
            </select>
          </div>
          <button class="btn-primary shrink-0" :disabled="!app.isAdmin || !newUser.name.trim() || !newUser.email.trim() || newUser.password.length < 8" @click="addUser">添加</button>
        </div>
      </section>

      <!-- 审计日志 -->
      <section v-else-if="tab === 'audit'" class="panel p-5">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 class="text-sm font-semibold text-white">审计日志</h2>
            <p class="mt-0.5 text-xs text-slate-500">每次仅加载 50 条；每个租户最多自动保留 10,000 条最新记录。</p>
          </div>
          <div class="flex flex-wrap gap-2">
            <div class="relative">
              <svg viewBox="0 0 24 24" fill="none" class="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-slate-500" stroke="currentColor" stroke-width="1.8"><path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-5.197-5.197m0 0A7.5 7.5 0 105.196 5.196a7.5 7.5 0 0010.607 10.607z" /></svg>
              <input v-model="auditKeyword" class="input !w-52 !py-1.5 !pl-8 !text-xs" placeholder="搜索操作者 / 操作 / 详情…" />
            </div>
            <select v-model="auditCategory" class="input !w-32 !py-1.5 !text-xs">
              <option value="all">全部类别</option>
              <option value="change">变更</option>
              <option value="danger">删除 / 失败</option>
              <option value="login">登录</option>
            </select>
            <button class="btn-secondary !py-2 text-xs" :disabled="mesh.auditLoading" @click="refreshAudit">{{ mesh.auditLoading ? '加载中…' : '刷新' }}</button>
            <button v-if="app.isAdmin" class="btn-secondary !py-2 text-xs text-red-300" :disabled="mesh.auditLoading || !mesh.audit.length" @click="clearAuditLogs">清空日志</button>
          </div>
        </div>
        <p v-if="auditCategory !== 'all' || auditKeyword" class="mt-2 text-[11px] text-slate-600">当前显示 {{ filteredAudit.length }} / {{ mesh.audit.length }} 条</p>
        <div class="mt-4">
          <p v-if="!mesh.audit.length" class="py-8 text-center text-xs text-slate-500">后端未返回审计记录</p>
          <p v-else-if="!filteredAudit.length" class="py-8 text-center text-xs text-slate-500">当前筛选条件下没有记录</p>
          <div v-else class="max-h-[56vh] overflow-auto border-y border-ink-700 bg-[#05070a] font-mono text-[11px] leading-5 text-slate-400">
            <div class="sticky top-0 z-10 grid min-w-[68rem] grid-cols-[10rem_13rem_15rem_minmax(30rem,1fr)] border-b border-ink-800 bg-[#05070a] px-3 py-1 text-[10px] uppercase tracking-wide text-slate-600">
              <span>时间</span>
              <span>操作者</span>
              <span>操作</span>
              <span>详情</span>
            </div>
            <div>
              <div v-for="e in filteredAudit" :key="e.id" class="grid min-w-[68rem] grid-cols-[10rem_13rem_15rem_minmax(30rem,1fr)] items-start border-b border-white/[0.025] px-3 py-0.5 last:border-b-0 hover:bg-white/[0.035]">
                <span class="select-none text-slate-600">{{ fmtDateTime(e.time) }}</span>
                <span class="truncate text-cyan-300" :title="e.user">{{ e.user || '系统' }}</span>
                <span class="truncate" :class="auditActionClass(e.action)" :title="e.action">{{ e.action }}</span>
                <span class="min-w-0 truncate text-slate-300" :title="e.detail">{{ e.detail }}</span>
              </div>
              <button v-if="mesh.auditHasMore" class="block w-full border-t border-ink-800 px-3 py-2 text-center text-xs text-cyan-300 hover:bg-cyan-500/10 disabled:text-slate-600" :disabled="mesh.auditLoading" @click="mesh.loadAuditPage(false)">
                {{ mesh.auditLoading ? '加载中…' : '加载更多' }}
              </button>
            </div>
          </div>
        </div>
      </section>

      <!-- 发布记录 -->
      <section v-else-if="tab === 'publish'" class="space-y-4">
        <div v-if="!mesh.revisions.length" class="panel py-10 text-center text-xs text-slate-500">后端未返回发布记录</div>
        <div v-for="r in mesh.revisions" :key="r.id" class="panel p-5">
          <div class="flex items-center justify-between">
            <p class="text-sm font-semibold text-white">配置版本 v{{ r.version }}</p>
            <span class="text-xs text-slate-500">{{ fmtDateTime(r.time) }} · {{ r.operator }}</span>
          </div>
          <ul class="mt-3 space-y-1">
            <li v-for="(c, i) in r.changes" :key="i" class="text-xs leading-relaxed text-slate-400">· {{ c }}</li>
          </ul>
          <div class="mt-3 flex flex-wrap gap-1.5 border-t border-ink-700 pt-3">
            <span v-for="t in r.targets" :key="t.agentName" class="chip" :class="t.status === 'success' ? 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/30' : 'bg-red-500/10 text-red-400 ring-1 ring-red-500/30'">
              {{ t.agentName }} {{ t.status === 'success' ? '✓' : '✗' }}
            </span>
          </div>
        </div>
      </section>

      <!-- API 令牌 -->
      <section v-else-if="tab === 'apitoken'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">API 令牌</h2>
        <p class="mt-0.5 text-xs text-slate-500">供脚本 / CI 调用控制平面 API 的长期凭据；明文仅在创建时显示一次，服务端只保存哈希。</p>
        <div v-if="createdToken" class="mt-4 rounded-xl bg-emerald-500/10 p-4 ring-1 ring-emerald-500/40">
          <p class="text-xs font-semibold text-emerald-300">新令牌已创建（仅显示一次，请立即保存）</p>
          <pre class="mt-2 overflow-x-auto rounded-lg bg-ink-950 p-3 font-mono text-xs text-emerald-200">{{ createdToken }}</pre>
          <p class="mt-1.5 text-[11px] text-slate-500">使用方式：<code class="text-cyan-300">Authorization: Bearer &lt;令牌&gt;</code></p>
        </div>
        <div class="mt-4 space-y-2">
          <div v-for="token in apiTokens" :key="token.id" class="flex items-center gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-medium text-slate-200">{{ token.name }}</p>
              <p class="text-[11px] text-slate-500">创建于 {{ fmtDateTime(Date.parse(token.created_at)) }}<span v-if="token.created_by"> · 创建者 {{ mesh.users.find((u) => u.id === token.created_by)?.name || token.created_by }}</span><span v-if="token.last_used_at"> · 最近使用 {{ ago(Date.parse(token.last_used_at)) }}</span><span v-else> · 从未使用</span><span v-if="token.expires_at" class="text-amber-400"> · 有效期至 {{ fmtDateTime(Date.parse(token.expires_at)) }}</span></p>
            </div>
            <button class="chip bg-red-500/10 text-red-300 ring-1 ring-red-500/30" :disabled="!app.isAdmin" @click="removeAPIToken(token.id)">撤销</button>
          </div>
          <p v-if="!apiTokens.length" class="rounded-xl bg-ink-800/40 py-6 text-center text-xs text-slate-500 ring-1 ring-ink-700">暂无 API 令牌</p>
        </div>
        <div class="mt-4 flex flex-wrap items-end gap-3 border-t border-ink-700 pt-4">
          <div class="flex-1"><label class="label">令牌名称</label><input v-model="newTokenName" class="input" placeholder="如：CI 部署脚本" /></div>
          <div>
            <label class="label">有效期</label>
            <select v-model.number="newTokenTTL" class="input">
              <option :value="0">永久有效</option>
              <option :value="30">30 天</option>
              <option :value="90">90 天</option>
              <option :value="365">1 年</option>
            </select>
          </div>
          <button class="btn-primary" :disabled="!app.isAdmin || tokenSaving || !newTokenName.trim()" @click="createAPIToken">{{ tokenSaving ? '创建中…' : '生成令牌' }}</button>
        </div>
      </section>

      <!-- 备份与恢复 -->
      <section v-else-if="tab === 'backup'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">备份与恢复</h2>
        <p class="mt-0.5 text-xs text-slate-500">使用 VACUUM INTO 生成一致性在线备份；恢复会立即切换到备份中的数据库（仅 SQLite 部署支持）。</p>
        <div class="mt-4 grid gap-4 sm:grid-cols-2">
          <div class="rounded-xl bg-ink-800/60 p-5 ring-1 ring-ink-600">
            <p class="text-sm font-semibold text-slate-200">下载备份</p>
            <p class="mt-1 text-[11px] leading-relaxed text-slate-500">导出当前 SQLite 数据库的完整快照，可离线保存。</p>
            <button class="btn-secondary mt-4" :disabled="!app.isAdmin || backingUp" @click="downloadBackup">{{ backingUp ? '备份中…' : '下载备份文件' }}</button>
          </div>
          <div class="rounded-xl bg-ink-800/60 p-5 ring-1 ring-ink-600">
            <p class="text-sm font-semibold text-slate-200">恢复备份</p>
            <p class="mt-1 text-[11px] leading-relaxed text-slate-500">上传 wiremesh-backup.db，恢复完成后将自动重新登录。</p>
            <div class="mt-4 flex items-center gap-3">
              <input type="file" accept=".db" class="min-w-0 flex-1 text-xs text-slate-400" @change="pickRestoreFile" />
              <button class="btn-secondary shrink-0 text-red-300" :disabled="!app.isAdmin || restoring || !restoreFile" @click="restoreBackup">{{ restoring ? '恢复中…' : '恢复数据库' }}</button>
            </div>
          </div>
        </div>
      </section>

      <!-- 登录会话 -->
      <section v-else-if="tab === 'sessions'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">登录会话</h2>
        <p class="mt-0.5 text-xs text-slate-500">查看当前活跃的登录会话并强制下线；会话记录保存在内存中，服务重启后清空。</p>
        <div class="mt-4 space-y-2">
          <div v-for="session in sessions" :key="session.id" class="flex items-center gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
            <div class="min-w-0 flex-1">
              <p class="text-sm font-medium text-slate-200">{{ session.user_name }} <span class="ml-1 text-xs text-slate-500">{{ session.user_agent || '未知设备' }}</span></p>
              <p class="text-[11px] text-slate-500">登录于 {{ fmtDateTime(Date.parse(session.created_at)) }} · 最近活动 {{ ago(Date.parse(session.last_seen_at)) }}</p>
            </div>
            <span v-if="session.current" class="chip bg-emerald-500/10 text-emerald-300 ring-1 ring-emerald-500/30">当前会话</span>
            <button v-else class="chip bg-red-500/10 text-red-300 ring-1 ring-red-500/30" :disabled="!app.isAdmin" @click="revokeSession(session.id)">强制下线</button>
          </div>
          <p v-if="!sessions.length" class="rounded-xl bg-ink-800/40 py-6 text-center text-xs text-slate-500 ring-1 ring-ink-700">暂无活跃会话</p>
        </div>
      </section>

      <!-- 多因素认证 -->
      <section v-else-if="tab === 'mfa'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">多因素认证（MFA / TOTP）</h2>
        <p class="mt-0.5 text-xs text-slate-500">使用 Google Authenticator / 1Password 等认证器扫码，登录时额外要求 6 位动态验证码。</p>
        <div class="mt-4 flex items-center gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
          <span class="chip ring-1" :class="mfaEnabled ? 'bg-emerald-500/10 text-emerald-300 ring-emerald-500/30' : 'bg-slate-500/10 text-slate-400 ring-slate-500/30'">{{ mfaEnabled ? '已启用' : '未启用' }}</span>
          <p class="text-xs text-slate-400">{{ mfaEnabled ? '登录时需要输入认证器中的动态验证码' : '启用后为账号增加第二道登录验证' }}</p>
          <button v-if="mfaEnabled" class="btn-ghost ml-auto !py-1.5 text-xs text-red-300" @click="disableMFA">关闭 MFA</button>
        </div>

        <template v-if="!mfaEnabled">
          <div v-if="!mfaSecret" class="mt-4">
            <button class="btn-secondary" @click="setupMFA">开始设置认证器</button>
          </div>
          <div v-else class="mt-4 grid gap-5 sm:grid-cols-[15rem_1fr]">
            <div class="flex flex-col items-center gap-3 rounded-xl bg-ink-950/50 p-4 ring-1 ring-ink-600">
              <img v-if="mfaQr" :src="mfaQr" alt="MFA 二维码" class="w-48 max-w-full rounded-lg" />
              <p class="break-all font-mono text-[11px] text-slate-400">{{ mfaSecret }}</p>
            </div>
            <div>
              <p class="text-xs leading-relaxed text-slate-400">1. 使用认证器 App 扫描左侧二维码，或手动输入密钥。</p>
              <p class="mt-1 text-xs leading-relaxed text-slate-400">2. 输入认证器显示的 6 位验证码完成启用。</p>
              <div class="mt-4 flex items-end gap-3">
                <div><label class="label">动态验证码</label><input v-model="mfaOtp" class="input font-mono" inputmode="numeric" maxlength="6" placeholder="6 位验证码" /></div>
                <button class="btn-primary" :disabled="!mfaOtp.trim()" @click="enableMFA">验证并启用</button>
              </div>
            </div>
          </div>
        </template>
      </section>

      <!-- 单点登录（SSO / OIDC） -->
      <section v-else-if="tab === 'sso'" class="panel p-5">
        <h2 class="text-sm font-semibold text-white">单点登录（SSO / OIDC）</h2>
        <p class="mt-0.5 text-xs text-slate-500">对接 Keycloak、Google Workspace 等 OIDC 提供商；登录页显示「单点登录」入口，按邮箱匹配现有 WireMesh 用户。</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="sm:col-span-2"><label class="label">Issuer（提供商根地址）</label><input v-model="ssoForm.issuer" class="input font-mono" placeholder="https://accounts.google.com" /></div>
          <div><label class="label">Client ID</label><input v-model="ssoForm.client_id" class="input font-mono" /></div>
          <div><label class="label">Client Secret</label><input v-model="ssoForm.client_secret" type="password" autocomplete="new-password" class="input font-mono" :placeholder="ssoSecretConfigured ? '已安全保存，留空保持不变' : 'OIDC 客户端密钥'" /></div>
        </div>
        <label class="mt-4 flex items-center gap-2 text-xs text-slate-400"><input v-model="ssoForm.enabled" type="checkbox" class="accent-emerald-500" />启用单点登录</label>
        <div class="mt-4 flex justify-end"><button class="btn-primary" :disabled="!app.isAdmin || ssoSaving" @click="saveSSO">{{ ssoSaving ? '保存中…' : '保存配置' }}</button></div>
      </section>

      <!-- 保存栏（设置类分组显示） -->
      <p v-if="tab === 'publish' && currentRevision" class="text-center text-[11px] text-slate-600">当前生效版本 v{{ currentRevision.version }} · 发布记录来自 WireMesh 后端</p>
    </div>

    <CustomPeerModal v-if="customPeerNetwork" :network-id="customPeerNetwork" @close="customPeerNetwork = null" />

    <!-- 重置确认 -->
    <div v-if="confirmReset" class="fixed inset-0 z-50 flex items-center justify-center bg-ink-950/70 p-4 backdrop-blur-sm" @click.self="confirmReset = false">
      <div class="panel w-full max-w-sm p-6">
        <h3 class="text-base font-semibold text-white">重置控制台</h3>
        <p class="mt-2 text-sm text-slate-400">将清除当前浏览器登录状态和本地界面设置，然后返回登录页。服务端账号、项目和网络不会被删除。</p>
        <div class="mt-5 flex justify-end gap-2.5">
          <button class="btn-ghost" @click="confirmReset = false">取消</button>
          <button class="inline-flex items-center justify-center gap-2 rounded-xl bg-red-500 px-4 py-2.5 text-sm font-semibold text-white transition hover:bg-red-400 active:scale-[0.98]" @click="doReset">确认重置</button>
        </div>
      </div>
    </div>
  </div>
</template>
