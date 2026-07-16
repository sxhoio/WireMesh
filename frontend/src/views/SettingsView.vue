<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import CustomPeerModal from '../components/CustomPeerModal.vue'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'
import type { NotifyChannel, NotifyChannelType } from '../types'
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

const tab = ref<(typeof tabs)[number]['k']>('net')
const saved = ref(false)
const confirmReset = ref(false)
const customPeerNetwork = ref<string | null>(null)

const form = reactive(JSON.parse(JSON.stringify(app.settings)) as typeof app.settings)

const geoTestIP = ref('')
const geoTestResult = ref<string | null>(null)

const roleMeta = { admin: { l: 'Admin', c: 'bg-red-500/10 text-red-300 ring-red-500/30' }, operator: { l: 'Operator', c: 'bg-cyan-500/10 text-cyan-300 ring-cyan-500/30' }, viewer: { l: 'Viewer', c: 'bg-slate-500/10 text-slate-400 ring-slate-500/30' } }
const topologyLabel = { 'full-mesh': 'Full Mesh', 'hub-spoke': 'Hub-Spoke', custom: 'Custom' }

const newUser = reactive({ name: '', email: '', role: 'viewer' as const })

function save() {
  app.updateSettings(JSON.parse(JSON.stringify(form)))
  saved.value = true
  setTimeout(() => (saved.value = false), 1600)
}


function testGeoIP() {
  geoTestResult.value = 'WireMesh 后端当前未提供 GeoIP 查询 API。'
  mesh.unsupported('GeoIP 查询')
}

function addUser() {
  mesh.unsupported('用户管理')
}

function doReset() {
  app.resetAll()
  confirmReset.value = false
  router.push({ name: 'login' })
}

const currentRevision = computed(() => mesh.revisions[0])

// ---- 通知配置 ----
const channelTypeMeta: Record<NotifyChannelType, { l: string; c: string; placeholder: string }> = {
  webhook: { l: 'Webhook', c: 'bg-cyan-500/10 text-cyan-300 ring-cyan-500/30', placeholder: 'https://example.com/hook/xxx' },
  dingtalk: { l: '钉钉', c: 'bg-blue-500/10 text-blue-300 ring-blue-500/30', placeholder: 'https://oapi.dingtalk.com/robot/send?access_token=…' },
  wecom: { l: '企业微信', c: 'bg-emerald-500/10 text-emerald-300 ring-emerald-500/30', placeholder: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…' },
  feishu: { l: '飞书', c: 'bg-violet-500/10 text-violet-300 ring-violet-500/30', placeholder: 'https://open.feishu.cn/open-apis/bot/v2/hook/…' },
  telegram: { l: 'Telegram', c: 'bg-sky-500/10 text-sky-300 ring-sky-500/30', placeholder: 'bot_token:chat_id' },
  email: { l: '邮件', c: 'bg-amber-500/10 text-amber-300 ring-amber-500/30', placeholder: 'ops@example.com' },
}

const editingChannelId = ref<string | null>(null)
const channelForm = reactive({
  name: '',
  type: 'dingtalk' as NotifyChannelType,
  target: '',
  scope: 'all' as 'all' | 'custom',
  agents: [] as string[],
})
const channelFormError = ref('')
const confirmDelChannel = ref<NotifyChannel | null>(null)

function resetChannelForm() {
  editingChannelId.value = null
  channelForm.name = ''
  channelForm.type = 'dingtalk'
  channelForm.target = ''
  channelForm.scope = 'all'
  channelForm.agents = []
  channelFormError.value = ''
}

function editChannel(c: NotifyChannel) {
  editingChannelId.value = c.id
  channelForm.name = c.name
  channelForm.type = c.type
  channelForm.target = c.target
  channelForm.scope = c.agents === 'all' ? 'all' : 'custom'
  channelForm.agents = c.agents === 'all' ? [] : [...c.agents]
}

function toggleChannelAgent(id: string) {
  const i = channelForm.agents.indexOf(id)
  if (i >= 0) channelForm.agents.splice(i, 1)
  else channelForm.agents.push(id)
}

function saveChannel() {
  channelFormError.value = 'WireMesh 后端当前未提供通知渠道 API。'
  mesh.unsupported('通知渠道')
}

function channelAgentsLabel(c: NotifyChannel) {
  if (c.agents === 'all') return '全部节点'
  return c.agents.map((id) => mesh.agentById(id)?.name ?? id).join('、')
}
</script>

<template>
  <div class="flex flex-col gap-4 lg:flex-row lg:gap-6">
    <!-- 分组导航：移动端横向滚动，桌面左侧竖排 -->
    <aside class="w-full shrink-0 lg:w-44">
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
    <div class="min-w-0 max-w-3xl flex-1 space-y-5">
      <!-- 网络默认值 -->
      <section v-if="tab === 'net'" class="panel p-4 sm:p-6">
        <h2 class="text-sm font-semibold text-white">网络默认值</h2>
        <p class="mt-0.5 text-xs text-slate-500">新建网络/接口时的默认参数</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
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
      <section v-else-if="tab === 'status'" class="panel p-4 sm:p-6">
        <h2 class="text-sm font-semibold text-white">状态判定</h2>
        <p class="mt-0.5 text-xs text-slate-500">链路绿/黄/红/灰的判定阈值</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3">
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
      <section v-else-if="tab === 'collect'" class="panel p-4 sm:p-6">
        <h2 class="text-sm font-semibold text-white">数据采集</h2>
        <p class="mt-0.5 text-xs text-slate-500">Agent 上报与连通探测周期</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div><label class="label">上报周期（秒）</label><input v-model.number="form.collect.reportSec" type="number" min="5" class="input font-mono" /></div>
          <div><label class="label">探测周期（秒）</label><input v-model.number="form.collect.probeSec" type="number" min="5" class="input font-mono" /></div>
          <div><label class="label">地图刷新周期（秒）</label><input v-model.number="form.collect.mapRefreshSec" type="number" min="5" class="input font-mono" /></div>
        </div>
      </section>

      <!-- 流量保留 -->
      <section v-else-if="tab === 'retention'" class="panel p-4 sm:p-6">
        <h2 class="text-sm font-semibold text-white">流量保留</h2>
        <p class="mt-0.5 text-xs text-slate-500">分层保留策略：原始数据用于实时与 24h 曲线，小时聚合用于 7 天，日聚合用于月度</p>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div><label class="label">原始数据（天）</label><input v-model.number="form.retention.rawDays" type="number" min="1" class="input font-mono" /></div>
          <div><label class="label">小时聚合（天）</label><input v-model.number="form.retention.hourlyDays" type="number" min="1" class="input font-mono" /></div>
          <div><label class="label">日聚合（天）</label><input v-model.number="form.retention.dailyDays" type="number" min="1" class="input font-mono" /></div>
        </div>
      </section>

      <!-- GeoIP -->
      <section v-else-if="tab === 'geoip'" class="panel p-4 sm:p-6">
        <h2 class="text-sm font-semibold text-white">GeoIP</h2>
        <p class="mt-0.5 text-xs text-slate-500">通过 Endpoint 公网 IP 解析地理位置；隧道私网地址不用于定位</p>
        <dl class="mt-4 space-y-3">
          <div class="flex items-center justify-between rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
            <span class="text-xs text-slate-500">数据库路径</span>
            <span class="font-mono text-xs text-slate-300">{{ mesh.geoip.dbPath }}</span>
          </div>
          <div class="grid grid-cols-3 gap-3">
            <div class="rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600"><p class="text-xs text-slate-500">版本</p><p class="mt-0.5 font-mono text-sm text-slate-200">{{ mesh.geoip.version }}</p></div>
            <div class="rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600"><p class="text-xs text-slate-500">条目数</p><p class="mt-0.5 font-mono text-sm text-slate-200">{{ mesh.geoip.entryCount.toLocaleString() }}</p></div>
            <div class="rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600"><p class="text-xs text-slate-500">更新时间</p><p class="mt-0.5 text-sm text-slate-200">{{ ago(mesh.geoip.updatedAt) }}</p></div>
          </div>
        </dl>
        <div class="mt-4 flex gap-2.5">
          <button class="btn-ghost" @click="mesh.reloadGeoIP(app.username)">重新加载数据库</button>
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
      <section v-else-if="tab === 'agent'" class="panel p-4 sm:p-6">
        <h2 class="text-sm font-semibold text-white">Agent</h2>
        <p class="mt-0.5 text-xs text-slate-500">WireMesh 使用由后端签发的一次性注册令牌，不在浏览器中保存全局 Token。</p>
        <div class="mt-4 rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600">
          <p class="text-sm text-slate-200">前往“节点列表 → 接入新节点”选择真实项目和网络，并生成一次性注册令牌。</p>
          <button class="btn-primary mt-4" @click="router.push({ name: 'nodes' })">前往节点列表</button>
        </div>
      </section>

      <!-- 通知配置 -->
      <section v-else-if="tab === 'notify'" class="space-y-5">
        <div class="panel p-4 sm:p-6">
          <h2 class="text-sm font-semibold text-white">通知渠道</h2>
          <p class="mt-0.5 text-xs text-slate-500">可配置多个渠道并绑定节点；当绑定节点断开（离线）时立即推送告警通知</p>

          <!-- 渠道列表 -->
          <div class="mt-4 space-y-2.5">
            <div v-for="c in mesh.notifyChannels" :key="c.id" class="rounded-xl bg-ink-800/60 p-4 ring-1 ring-ink-600">
              <div class="flex items-center gap-3">
                <span class="chip ring-1" :class="channelTypeMeta[c.type].c">{{ channelTypeMeta[c.type].l }}</span>
                <div class="min-w-0 flex-1">
                  <p class="truncate text-sm font-medium text-slate-200">{{ c.name }}</p>
                  <p class="truncate font-mono text-[11px] text-slate-500" :title="c.target">{{ c.target }}</p>
                </div>
                <button
                  class="relative h-5 w-9 shrink-0 rounded-full transition"
                  :class="c.enabled ? 'bg-emerald-500' : 'bg-ink-600'"
                  :title="c.enabled ? '点击停用' : '点击启用'"
                  @click="mesh.updateNotifyChannel(c.id, { enabled: !c.enabled }, app.username)"
                >
                  <span class="absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all" :class="c.enabled ? 'left-[18px]' : 'left-0.5'"></span>
                </button>
                <button class="chip shrink-0 bg-slate-500/10 text-slate-400 ring-1 ring-slate-500/30 transition hover:bg-ink-700" @click="editChannel(c)">编辑</button>
                <button class="chip shrink-0 bg-slate-500/10 text-slate-400 ring-1 ring-slate-500/30 transition hover:bg-cyan-500/10 hover:text-cyan-300" @click="mesh.testNotifyChannel(c.id, app.username)">测试</button>
                <button class="chip shrink-0 bg-slate-500/10 text-slate-400 ring-1 ring-slate-500/30 transition hover:bg-red-500/10 hover:text-red-400" @click="confirmDelChannel = c">删除</button>
              </div>
              <p class="mt-2 text-[11px] text-slate-500">
                绑定范围：<span class="text-slate-300">{{ channelAgentsLabel(c) }}</span>
                <span class="ml-2" :class="c.enabled ? 'text-emerald-400' : 'text-slate-600'">{{ c.enabled ? '· 节点断开时立即通知' : '· 已停用' }}</span>
              </p>
            </div>
            <p v-if="!mesh.notifyChannels.length" class="rounded-xl bg-ink-800/40 py-6 text-center text-xs text-slate-500 ring-1 ring-ink-700">后端未返回通知渠道；当前版本不提供通知渠道 API</p>
          </div>

          <!-- 新增/编辑表单 -->
          <div class="mt-5 border-t border-ink-700 pt-5">
            <p class="mb-3 text-xs font-semibold text-slate-300">{{ editingChannelId ? '编辑渠道' : '新增渠道' }}</p>
            <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <label class="label">渠道名称</label>
                <input v-model="channelForm.name" class="input" placeholder="如 运维值班群" />
              </div>
              <div>
                <label class="label">渠道类型</label>
                <select v-model="channelForm.type" class="input">
                  <option v-for="(m, t) in channelTypeMeta" :key="t" :value="t">{{ m.l }}</option>
                </select>
              </div>
            </div>
            <div class="mt-4">
              <label class="label">Webhook 地址 / 接收目标</label>
              <input v-model="channelForm.target" class="input font-mono" :placeholder="channelTypeMeta[channelForm.type].placeholder" />
            </div>
            <div class="mt-4">
              <label class="label">绑定节点（断开时通知）</label>
              <div class="flex gap-2">
                <button
                  class="rounded-xl px-4 py-2 text-xs font-medium ring-1 transition"
                  :class="channelForm.scope === 'all' ? 'bg-emerald-500/15 text-emerald-300 ring-emerald-500/40' : 'bg-ink-800 text-slate-400 ring-ink-600 hover:text-slate-200'"
                  @click="channelForm.scope = 'all'"
                >
                  全部节点
                </button>
                <button
                  class="rounded-xl px-4 py-2 text-xs font-medium ring-1 transition"
                  :class="channelForm.scope === 'custom' ? 'bg-emerald-500/15 text-emerald-300 ring-emerald-500/40' : 'bg-ink-800 text-slate-400 ring-ink-600 hover:text-slate-200'"
                  @click="channelForm.scope = 'custom'"
                >
                  指定节点
                </button>
              </div>
              <div v-if="channelForm.scope === 'custom'" class="mt-2.5 grid max-h-36 grid-cols-2 gap-1.5 overflow-y-auto rounded-xl bg-ink-950/50 p-3 ring-1 ring-ink-600 sm:grid-cols-3">
                <label v-for="a in mesh.agents" :key="a.id" class="flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-xs text-slate-300 transition hover:bg-ink-800">
                  <input type="checkbox" class="h-3.5 w-3.5 rounded border-ink-600 bg-ink-800 accent-emerald-500" :checked="channelForm.agents.includes(a.id)" @change="toggleChannelAgent(a.id)" />
                  <span class="h-1.5 w-1.5 shrink-0 rounded-full" :class="a.status === 'online' ? 'bg-emerald-400' : 'bg-slate-600'"></span>
                  <span class="truncate">{{ a.name }}</span>
                </label>
              </div>
            </div>
            <p v-if="channelFormError" class="mt-3 text-xs text-red-400">{{ channelFormError }}</p>
            <div class="mt-4 flex gap-2.5">
              <button v-if="editingChannelId" class="btn-ghost" @click="resetChannelForm">取消编辑</button>
              <button class="btn-primary" @click="saveChannel">{{ editingChannelId ? '保存修改' : '添加渠道' }}</button>
            </div>
          </div>
        </div>

        <!-- 通知记录 -->
        <div class="panel p-4 sm:p-6">
          <h2 class="text-sm font-semibold text-white">通知记录</h2>
          <div class="mt-4 space-y-2">
            <div v-for="log in mesh.notifyLogs" :key="log.id" class="flex items-start gap-3 rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
              <span class="chip mt-0.5 shrink-0 ring-1" :class="channelTypeMeta[log.channelType]?.c ?? 'bg-slate-500/10 text-slate-400 ring-slate-500/30'">{{ channelTypeMeta[log.channelType]?.l ?? log.channelType }}</span>
              <div class="min-w-0 flex-1">
                <p class="text-xs text-slate-200">{{ log.message }}</p>
                <p class="text-[11px] text-slate-500">渠道：{{ log.channelName }}<template v-if="log.agentName !== '—'"> · 节点：{{ log.agentName }}</template></p>
              </div>
              <span class="chip shrink-0" :class="log.status === 'test' ? 'bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30' : 'bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/30'">
                {{ log.status === 'test' ? '测试' : '已送达' }}
              </span>
              <span class="shrink-0 text-[11px] text-slate-600">{{ fmtDateTime(log.time) }}</span>
            </div>
            <p v-if="!mesh.notifyLogs.length" class="py-6 text-center text-xs text-slate-500">暂无通知记录</p>
          </div>
        </div>
      </section>

      <!-- 项目与网络 -->
      <section v-else-if="tab === 'project'" class="space-y-5">
        <div class="panel p-4 sm:p-6">
          <h2 class="text-sm font-semibold text-white">项目</h2>
          <div class="mt-3 space-y-2">
            <div v-for="p in mesh.projects" :key="p.id" class="flex items-center justify-between rounded-xl bg-ink-800/60 px-4 py-3 ring-1 ring-ink-600">
              <div>
                <p class="text-sm font-medium text-slate-200">{{ p.name }}</p>
                <p class="text-xs text-slate-500">{{ p.desc }}</p>
              </div>
              <span class="chip bg-cyan-500/10 text-cyan-300 ring-1 ring-cyan-500/30">{{ mesh.networks.filter((n) => n.projectId === p.id).length }} 个网络</span>
            </div>
          </div>
        </div>
        <div class="panel p-4 sm:p-6">
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
          </div>
        </div>
      </section>

      <!-- 用户与权限 -->
      <section v-else-if="tab === 'users'" class="panel p-4 sm:p-6">
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
        <div class="mt-4 flex items-end gap-2.5 border-t border-ink-700 pt-4">
          <div class="flex-1"><label class="label">用户名</label><input v-model="newUser.name" class="input" /></div>
          <div class="flex-1"><label class="label">邮箱</label><input v-model="newUser.email" class="input" /></div>
          <div class="w-32"><label class="label">角色</label>
            <select v-model="newUser.role" class="input">
              <option value="viewer">Viewer</option>
              <option value="operator">Operator</option>
              <option value="admin">Admin</option>
            </select>
          </div>
          <button class="btn-primary shrink-0" @click="addUser">添加</button>
        </div>
      </section>

      <!-- 审计日志 -->
      <section v-else-if="tab === 'audit'" class="panel p-4 sm:p-6">
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
        <div v-for="r in mesh.revisions" :key="r.id" class="panel p-4 sm:p-6">
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
        <button class="btn-primary min-w-32" @click="save">
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
          <button class="inline-flex items-center justify-center gap-2 rounded-xl bg-red-500 px-4 py-2.5 text-sm font-semibold text-white transition hover:bg-red-400 active:scale-[0.98]" @click="mesh.removeNotifyChannel(confirmDelChannel.id, app.username); confirmDelChannel = null">删除</button>
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
