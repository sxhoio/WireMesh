<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import CustomPeerModal from '../components/CustomPeerModal.vue'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import type { NotificationConfig, NotifyChannel, NotifyChannelType } from '../types'
import { ago, fmtDateTime } from '../utils/format'

const app = useAppStore()
const mesh = useMeshStore()
const router = useRouter()

const tabs = [
  { k: 'net', l: '网络默认值' },
  { k: 'status', l: '状态判定' },
  { k: 'collect', l: '数据采集' },
  { k: 'retention', l: '流量保留' },
  { k: 'geoip', l: 'GeoIP' },
  { k: 'agent', l: 'Agent' },
  { k: 'notify', l: '通知配置' },
  { k: 'project', l: '项目与网络' },
  { k: 'users', l: '用户与权限' },
  { k: 'audit', l: '审计日志' },
  { k: 'publish', l: '发布记录' },
] as const

const tab = ref<string>('net')
const saved = ref(false)
const confirmReset = ref(false)
const customPeerNetwork = ref<string | null>(null)

const form = reactive(JSON.parse(JSON.stringify(app.settings)) as typeof app.settings)

const geoTestIP = ref('')
const geoTestResult = ref<string | null>(null)
const geoDbPath = ref(mesh.geoip.dbPath)
const geoSaving = ref(false)
const geoReloading = ref(false)

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
  try {
    await mesh.reloadGeoIP(app.username)
  } finally {
    geoReloading.value = false
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
    setTimeout(() => (saved.value = false), 1600)
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

function doReset() {
  app.resetAll()
  confirmReset.value = false
  router.push({ name: 'login' })
}

const currentRevision = computed(() => mesh.revisions[0])

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
const confirmDelChannel = ref<NotifyChannel | null>(null)
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
async function deleteChannel() { if (!confirmDelChannel.value) return; if (await mesh.removeNotifyChannel(confirmDelChannel.value.id, app.username)) confirmDelChannel.value = null }
function channelAgentsLabel(c: NotifyChannel) { if (c.agents === 'all') return '全部节点'; return c.agents.map((id) => mesh.agentById(id)?.name ?? id).join('、') }
function channelConfigSummary(c: NotifyChannel) {
  const config = c.config || {}
  if (c.type === 'webhook') return (config.method || 'POST') + ' · ' + (config.contentType || 'application/json') + ' · ' + (config.urlConfigured ? '目标已配置' : '目标未配置')
  if (c.type === 'telegram') return 'Bot ' + (config.botTokenConfigured ? '已配置' : '未配置') + ' · Chat ' + (config.chatIdConfigured ? '已配置' : '未配置') + ' · ' + (config.parseMode || '纯文本') + ' · ' + (config.useProxy ? (config.proxyUrlConfigured ? '代理已配置' : '代理未配置') : '直连')
  if (c.type === 'email') return (config.smtpHost || 'SMTP 未配置') + ':' + (config.smtpPort || '-') + ' · ' + (config.recipientCount || 0) + ' 个收件人 · ' + (config.encryption || 'none')
  return (config.messageType || 'text') + ' · ' + (config.urlConfigured ? 'Webhook 已配置' : 'Webhook 未配置') + (config.secretConfigured ? ' · 签名已配置' : '')
}

</script>

<template>
  <div class="grid w-full grid-cols-1 gap-4 lg:grid-cols-[11rem_minmax(0,1fr)] lg:gap-6 2xl:grid-cols-[12rem_minmax(0,1fr)] 2xl:gap-8">
    <!-- 分组导航：移动端横向滚动，桌面左侧竖排 -->
    <aside class="w-full min-w-0">
      <div class="flex gap-1 overflow-x-auto pb-1 lg:sticky lg:top-0 lg:flex-col lg:space-y-1 lg:overflow-visible lg:pb-0">
        <button
          v-for="t in tabs"
          :key="t.k"
          class="shrink-0 whitespace-nowrap rounded-xl px-3.5 py-2.5 text-left text-sm font-medium transition lg:w-full lg:shrink"
          :class="tab === t.k ? 'bg-emerald-500/15 text-emerald-300 ring-1 ring-emerald-500/30' : 'text-slate-400 hover:bg-ink-800 hover:text-slate-200'"
          @click="tab = t.k"
        >
          {{ t.l }}
        </button>
      </div>
    </aside>

    <!-- 右侧内容 -->
    <div class="min-w-0 w-full space-y-5">
      <!-- 网络默认值 -->
      <section v-if="tab === 'net'" class="panel p-4 sm:p-6 2xl:p-7">
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
      <section v-else-if="tab === 'status'" class="panel p-4 sm:p-6 2xl:p-7">
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
      <section v-else-if="tab === 'collect'" class="panel p-4 sm:p-6 2xl:p-7">
        <h2 class="text-sm font-semibold text-white">数据采集</h2>
        <p class="mt-0.5 text-xs text-slate-500">Agent 上报与连通探测周期</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3 2xl:gap-6">
          <div><label class="label">上报周期（秒）</label><input v-model.number="form.collect.reportSec" type="number" min="5" class="input font-mono" /></div>
          <div><label class="label">探测周期（秒）</label><input v-model.number="form.collect.probeSec" type="number" min="5" class="input font-mono" /></div>
          <div><label class="label">地图刷新周期（秒）</label><input v-model.number="form.collect.mapRefreshSec" type="number" min="5" class="input font-mono" /></div>
        </div>
      </section>

      <!-- 流量保留 -->
      <section v-else-if="tab === 'retention'" class="panel p-4 sm:p-6 2xl:p-7">
        <h2 class="text-sm font-semibold text-white">流量保留</h2>
        <p class="mt-0.5 text-xs text-slate-500">分层保留策略：原始数据用于实时与 24h 曲线，小时聚合用于 7 天，日聚合用于月度</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3 2xl:gap-6">
          <div><label class="label">原始数据（天）</label><input v-model.number="form.retention.rawDays" type="number" min="1" class="input font-mono" /></div>
          <div><label class="label">小时聚合（天）</label><input v-model.number="form.retention.hourlyDays" type="number" min="1" class="input font-mono" /></div>
          <div><label class="label">日聚合（天）</label><input v-model.number="form.retention.dailyDays" type="number" min="1" class="input font-mono" /></div>
        </div>
      </section>

      <!-- GeoIP -->
      <section v-else-if="tab === 'geoip'" class="panel p-4 sm:p-6 2xl:p-7">
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
      <section v-else-if="tab === 'agent'" class="panel p-4 sm:p-6 2xl:p-7">
        <h2 class="text-sm font-semibold text-white">Agent</h2>
        <p class="mt-0.5 text-xs text-slate-500">WireMesh 使用由后端签发的一次性注册令牌，不在浏览器中保存全局 Token。</p>
        <div class="mt-4 rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600">
          <p class="text-sm text-slate-200">前往“节点列表 → 接入新节点”选择真实项目和网络，并生成一键安装接入命令。</p>
          <button class="btn-primary mt-4" @click="router.push({ name: 'nodes' })">前往节点列表</button>
        </div>
      </section>

      <!-- 通知配置 -->
      <section v-else-if="tab === 'notify'">
        <div class="grid grid-cols-1 items-start gap-5 xl:grid-cols-[minmax(22rem,0.9fr)_minmax(0,1.1fr)] 2xl:grid-cols-[minmax(26rem,0.85fr)_minmax(0,1.15fr)]">
          <div class="min-w-0 space-y-5">
            <div class="panel p-4 sm:p-6 2xl:p-7">
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
                  <button class="relative h-5 w-9 shrink-0 rounded-full transition" :class="c.enabled ? 'bg-emerald-500' : 'bg-ink-600'" :title="c.enabled ? '点击停用' : '点击启用'" :disabled="!app.isAdmin" @click="mesh.updateNotifyChannel(c.id, { enabled: !c.enabled }, app.username)">
                    <span class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all" :class="c.enabled ? 'left-[18px]' : 'left-0.5'"></span>
                  </button>
                </div>
                <p class="mt-2 break-all font-mono text-[11px] leading-5 text-slate-500">{{ channelConfigSummary(c) }}</p>
                <p class="mt-1 text-[11px] text-slate-500">范围：<span class="text-slate-300">{{ channelAgentsLabel(c) }}</span><span class="ml-2" :class="c.enabled ? 'text-emerald-400' : 'text-slate-600'">{{ c.enabled ? '· 已启用' : '· 已停用' }}</span></p>
                <div class="mt-3 flex flex-wrap gap-2">
                  <button class="chip bg-slate-500/10 text-slate-300 ring-1 ring-slate-500/30 hover:bg-ink-700" :disabled="!app.isAdmin" @click="editChannel(c)">编辑配置</button>
                  <button class="chip bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30 hover:bg-cyan-500/20" :disabled="!app.isAdmin" @click="mesh.testNotifyChannel(c.id, app.username)">发送测试</button>
                  <button class="chip bg-red-500/10 text-red-300 ring-1 ring-red-500/30 hover:bg-red-500/20" :disabled="!app.isAdmin" @click="confirmDelChannel = c">删除</button>
                </div>
              </div>
              <p v-if="!mesh.notifyChannels.length" class="rounded-xl bg-ink-800/40 py-8 text-center text-xs text-slate-500 ring-1 ring-ink-700">暂无通知渠道，请在右侧创建第一个渠道。</p>
            </div>
          </div>

          <div class="panel p-4 sm:p-6 2xl:p-7">
            <div class="flex items-center justify-between"><div><h2 class="text-sm font-semibold text-white">通知记录</h2><p class="mt-1 text-xs text-slate-500">测试和实际发送结果均由后端记录。</p></div></div>
            <div class="mt-4 max-h-[32rem] space-y-2 overflow-y-auto pr-1">
              <div v-for="log in mesh.notifyLogs" :key="log.id" class="flex items-start gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
                <span class="chip mt-0.5 shrink-0 ring-1" :class="channelTypeMeta[log.channelType]?.c ?? 'bg-slate-500/10 text-slate-400 ring-slate-500/30'">{{ channelTypeMeta[log.channelType]?.l ?? log.channelType }}</span>
                <div class="min-w-0 flex-1"><p class="text-xs text-slate-200">{{ log.message }}</p><p class="mt-1 text-[11px] text-slate-500">{{ log.channelName }} · {{ fmtDateTime(log.time) }}</p></div>
                <span class="chip shrink-0" :class="log.status === 'failed' ? 'bg-red-500/10 text-red-300 ring-1 ring-red-500/30' : log.status === 'test' ? 'bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30' : 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/30'">{{ log.status === 'failed' ? '失败' : log.status === 'test' ? '测试' : '已送达' }}</span>
              </div>
              <p v-if="!mesh.notifyLogs.length" class="py-6 text-center text-xs text-slate-500">暂无通知记录</p>
            </div>
          </div>
          </div>

          <div class="panel p-4 sm:p-6 2xl:p-7">
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
        <div class="panel p-4 sm:p-6 2xl:p-7">
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
        <div class="panel p-4 sm:p-6 2xl:p-7">
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
      <section v-else-if="tab === 'users'" class="panel p-4 sm:p-6 2xl:p-7">
        <h2 class="text-sm font-semibold text-white">用户与权限</h2>
        <p class="mt-0.5 text-xs text-slate-500">Viewer 只读 · Operator 可调整节点与 Peer · Admin 管理系统设置、用户与 IP 库</p>
        <div class="mt-4 space-y-2">
          <div v-for="u in mesh.users" :key="u.id" class="flex items-center gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
            <div class="flex h-8 w-8 items-center justify-center rounded-full bg-cyan-500/15 text-xs font-bold text-cyan-300 ring-1 ring-cyan-500/40">{{ u.name.slice(0, 1).toUpperCase() }}</div>
            <div class="min-w-0 flex-1">
              <p class="text-sm font-medium text-slate-200">{{ u.name }} <span class="ml-1 text-xs text-slate-500">{{ u.email }}</span></p>
              <p class="text-[11px] text-slate-500">{{ u.active ? '已激活' : '已禁用' }} · {{ u.lastLogin ? '最近登录 ' + ago(u.lastLogin) : '从未登录' }}</p>
            </div>
            <span class="chip ring-1" :class="roleMeta[u.role].c">{{ roleMeta[u.role].l }}</span>
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
      <section v-else-if="tab === 'audit'" class="panel p-4 sm:p-6 2xl:p-7">
        <h2 class="text-sm font-semibold text-white">审计日志</h2>
        <div class="mt-4 space-y-2">
          <p v-if="!mesh.audit.length" class="py-8 text-center text-xs text-slate-500">后端未返回审计记录</p>
          <div v-for="e in mesh.audit" :key="e.id" class="flex items-start gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
            <span class="chip mt-0.5 shrink-0 bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">{{ e.user }}</span>
            <div class="min-w-0 flex-1">
              <p class="text-sm text-slate-200">{{ e.action }}</p>
              <p class="text-xs text-slate-500">{{ e.detail }}</p>
            </div>
            <span class="shrink-0 text-[11px] text-slate-600">{{ fmtDateTime(e.time) }}</span>
          </div>
        </div>
      </section>

      <!-- 发布记录 -->
      <section v-else-if="tab === 'publish'" class="space-y-4">
        <div v-if="!mesh.revisions.length" class="panel py-10 text-center text-xs text-slate-500">后端未返回发布记录</div>
        <div v-for="r in mesh.revisions" :key="r.id" class="panel p-4 sm:p-6 2xl:p-7">
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

      <!-- 保存栏（设置类分组显示） -->
      <div v-if="['net', 'status', 'collect', 'retention', 'agent'].includes(tab)" class="flex items-center justify-between">
        <button class="text-sm text-red-400/80 transition hover:text-red-300" @click="confirmReset = true">退出并重置本地界面</button>
        <button class="btn-primary min-w-32" :disabled="app.loading || !app.isAdmin" @click="save">
          <svg v-if="saved" viewBox="0 0 24 24" fill="none" class="h-4 w-4" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" /></svg>
          {{ saved ? '已保存' : '保存设置' }}
        </button>
      </div>
      <p v-else-if="tab === 'publish' && currentRevision" class="text-center text-[11px] text-slate-600">当前生效版本 v{{ currentRevision.version }} · 发布记录来自 WireMesh 后端</p>
    </div>

    <CustomPeerModal v-if="customPeerNetwork" :network-id="customPeerNetwork" @close="customPeerNetwork = null" />

    <!-- 删除渠道确认 -->
    <div v-if="confirmDelChannel" class="fixed inset-0 z-50 flex items-center justify-center bg-ink-950/70 p-4 backdrop-blur-sm" @click.self="confirmDelChannel = null">
      <div class="panel w-full max-w-sm p-6">
        <h3 class="text-base font-semibold text-white">删除通知渠道</h3>
        <p class="mt-2 text-sm text-slate-400">确定删除「{{ confirmDelChannel.name }}」吗？删除后其绑定节点的断开告警将不再推送。</p>
        <div class="mt-5 flex justify-end gap-2.5">
          <button class="btn-ghost" @click="confirmDelChannel = null">取消</button>
          <button class="inline-flex items-center justify-center gap-2 rounded-xl bg-red-500 px-4 py-2.5 text-sm font-semibold text-white transition hover:bg-red-400 active:scale-[0.98]" @click="deleteChannel">删除</button>
        </div>
      </div>
    </div>

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
