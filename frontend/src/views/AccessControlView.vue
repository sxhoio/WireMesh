<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { api, type ApiAccessPolicy, type ApiAccessResource, type ApiEgressConfig } from '../api'
import { useAppStore } from '../stores/app'
import { useMeshStore } from '../stores/mesh'

const app = useAppStore()
const mesh = useMeshStore()

const selectedNetworkId = ref('')
const resources = ref<ApiAccessResource[]>([])
const policies = ref<ApiAccessPolicy[]>([])
const loading = ref(false)
const error = ref('')
const notice = ref('')

const resourceForm = reactive({
  name: '',
  gateway_node_id: '',
  target: '',
  port: 0,
  protocol: 'any' as 'tcp' | 'udp' | 'any' | '',
  description: '',
})

const policyForm = reactive({
  name: '',
  source_label: '',
  source_node_ids: [] as string[],
  resource_ids: [] as string[],
  enabled: true,
})

const editingPolicyId = ref<string | null>(null)
const savingResource = ref(false)
const savingPolicy = ref(false)

// 出口网关（Egress）
const egress = ref<ApiEgressConfig>({ network_id: '', egress_node_id: '', cidrs: [], updated_at: '' })
const egressNodeId = ref('')
const egressCIDRsText = ref('')
const savingEgress = ref(false)

const networkNodes = computed(() => mesh.agents.filter((agent) => agent.networkId === selectedNetworkId.value))

async function load() {
  if (!selectedNetworkId.value) return
  loading.value = true
  error.value = ''
  try {
    const [resourceResult, policyResult, egressResult] = await Promise.allSettled([
      api.accessResources(selectedNetworkId.value),
      api.accessPolicies(selectedNetworkId.value),
      api.egress(selectedNetworkId.value),
    ])
    if (resourceResult.status === 'fulfilled') resources.value = resourceResult.value
    else error.value = resourceResult.reason instanceof Error ? resourceResult.reason.message : '加载资源失败'
    if (policyResult.status === 'fulfilled') policies.value = policyResult.value
    if (egressResult.status === 'fulfilled') {
      egress.value = egressResult.value
      egressNodeId.value = egressResult.value.egress_node_id
      egressCIDRsText.value = (egressResult.value.cidrs || []).join(', ')
    }
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function saveEgress() {
  if (!selectedNetworkId.value || savingEgress.value) return
  savingEgress.value = true
  error.value = ''
  const cidrs = egressCIDRsText.value.split(',').map((item) => item.trim()).filter(Boolean)
  try {
    egress.value = await api.updateEgress(selectedNetworkId.value, { egress_node_id: egressNodeId.value, cidrs })
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '保存出口网关失败'
  } finally {
    savingEgress.value = false
  }
}

watch(selectedNetworkId, () => {
  resetResourceForm()
  resetPolicyForm()
  void load()
})

function resetResourceForm() {
  resourceForm.name = ''
  resourceForm.gateway_node_id = ''
  resourceForm.target = ''
  resourceForm.port = 0
  resourceForm.protocol = 'any'
  resourceForm.description = ''
}

function resetPolicyForm() {
  editingPolicyId.value = null
  policyForm.name = ''
  policyForm.source_label = ''
  policyForm.source_node_ids = []
  policyForm.resource_ids = []
  policyForm.enabled = true
}

function editPolicy(policy: ApiAccessPolicy) {
  editingPolicyId.value = policy.id
  policyForm.name = policy.name
  policyForm.source_label = policy.source_label || ''
  policyForm.source_node_ids = [...policy.source_node_ids]
  policyForm.resource_ids = [...policy.resource_ids]
  policyForm.enabled = policy.enabled
}

function toggleSourceNode(id: string) {
  const index = policyForm.source_node_ids.indexOf(id)
  if (index >= 0) policyForm.source_node_ids.splice(index, 1)
  else policyForm.source_node_ids.push(id)
}

function toggleResource(id: string) {
  const index = policyForm.resource_ids.indexOf(id)
  if (index >= 0) policyForm.resource_ids.splice(index, 1)
  else policyForm.resource_ids.push(id)
}

async function saveResource() {
  if (!selectedNetworkId.value || savingResource.value) return
  if (!resourceForm.name.trim()) {
    error.value = '请输入资源名称'
    return
  }
  if (!resourceForm.gateway_node_id) {
    error.value = '请选择网关节点'
    return
  }
  savingResource.value = true
  error.value = ''
  try {
    await api.createAccessResource(selectedNetworkId.value, {
      name: resourceForm.name.trim(),
      gateway_node_id: resourceForm.gateway_node_id,
      target: resourceForm.target.trim(),
      port: resourceForm.port,
      protocol: resourceForm.protocol,
      description: resourceForm.description.trim(),
    })
    resetResourceForm()
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '创建资源失败'
  } finally {
    savingResource.value = false
  }
}

async function removeResource(resource: ApiAccessResource) {
  try {
    await api.deleteAccessResource(selectedNetworkId.value, resource.id)
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '删除资源失败'
  }
}

async function savePolicy() {
  if (!selectedNetworkId.value || savingPolicy.value) return
  if (!policyForm.name.trim()) {
    error.value = '请输入策略名称'
    return
  }
  if (!policyForm.source_label.trim() && !policyForm.source_node_ids.length) {
    error.value = '请选择源节点或填写标签选择器'
    return
  }
  if (!policyForm.resource_ids.length) {
    error.value = '请至少选择一个资源'
    return
  }
  savingPolicy.value = true
  error.value = ''
  const payload = {
    name: policyForm.name.trim(),
    source_label: policyForm.source_label.trim(),
    source_node_ids: policyForm.source_node_ids,
    resource_ids: policyForm.resource_ids,
    enabled: policyForm.enabled,
  }
  try {
    if (editingPolicyId.value) await api.updateAccessPolicy(selectedNetworkId.value, editingPolicyId.value, payload)
    else await api.createAccessPolicy(selectedNetworkId.value, payload)
    resetPolicyForm()
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '保存策略失败'
  } finally {
    savingPolicy.value = false
  }
}

async function removePolicy(policy: ApiAccessPolicy) {
  try {
    await api.deleteAccessPolicy(selectedNetworkId.value, policy.id)
    if (editingPolicyId.value === policy.id) resetPolicyForm()
    await load()
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '删除策略失败'
  }
}

async function publishChanges() {
  if (!selectedNetworkId.value) return
  try {
    const result = await api.publish(selectedNetworkId.value)
    await mesh.waitForDeliveryResult(result)
    notice.value = '访问策略已发布到节点配置'
    setTimeout(() => (notice.value = ''), 3000)
  } catch (reason) {
    error.value = reason instanceof Error ? reason.message : '发布失败'
  }
}
</script>

<template>
  <div class="flex h-full flex-col gap-5">
    <div class="flex flex-wrap items-center gap-3">
      <div>
        <label class="label">网络</label>
        <select v-model="selectedNetworkId" class="input !w-64">
          <option value="">选择网络</option>
          <option v-for="n in mesh.networks" :key="n.id" :value="n.id">{{ n.name }}（{{ n.cidr }}）</option>
        </select>
      </div>
      <p class="mb-2 text-xs text-slate-500">访问策略在发布网络配置时生效：策略允许的资源目标 CIDR 会加入源节点对应网关 Peer 的 AllowedIPs（IP 级路由控制），端口作为元数据保存。</p>
      <div class="ml-auto flex gap-2">
        <p v-if="notice" class="mb-2 self-end text-xs text-emerald-300">{{ notice }}</p>
        <button class="btn-primary" :disabled="!app.canOperate || !selectedNetworkId" @click="publishChanges">保存并发布</button>
      </div>
    </div>

    <p v-if="error" class="rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-300 ring-1 ring-red-500/30">{{ error }}</p>

    <div class="grid min-h-0 flex-1 grid-cols-1 gap-5 xl:grid-cols-2">
      <!-- 资源 -->
      <div class="panel min-h-0 overflow-y-auto p-5">
        <div class="mb-3 flex items-center justify-between">
          <div>
            <h2 class="text-sm font-semibold text-white">资源（{{ resources.length }}）</h2>
            <p class="mt-0.5 text-xs text-slate-500">节点上的可访问服务：目标 CIDR + 可选端口。</p>
          </div>
        </div>
        <p v-if="!selectedNetworkId" class="py-6 text-center text-xs text-slate-500">请先选择网络</p>
        <template v-else>
          <div class="space-y-2">
            <div v-for="resource in resources" :key="resource.id" class="rounded-xl bg-ink-800/60 p-3.5 ring-1 ring-ink-600">
              <div class="flex items-center gap-2.5">
                <p class="min-w-0 flex-1 truncate text-sm font-medium text-slate-200">{{ resource.name }}</p>
                <span class="chip shrink-0 bg-cyan-500/10 font-mono text-cyan-300 ring-1 ring-cyan-500/30">{{ resource.target }}{{ resource.port ? ':' + resource.port + '/' + (resource.protocol || 'any') : '' }}</span>
                <button v-if="app.canOperate" class="chip shrink-0 bg-red-500/10 text-red-300 ring-1 ring-red-500/30" @click="removeResource(resource)">删除</button>
              </div>
              <p class="mt-1 truncate text-[11px] text-slate-500">网关：{{ mesh.agentById(resource.gateway_node_id)?.name || resource.gateway_node_id }}<span v-if="resource.description"> · {{ resource.description }}</span></p>
            </div>
            <p v-if="!resources.length" class="py-5 text-center text-xs text-slate-500">暂无资源</p>
          </div>
          <div class="mt-4 rounded-xl border border-ink-700 bg-ink-900/50 p-4">
            <p class="mb-3 text-xs font-semibold text-slate-300">新建资源</p>
            <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div class="sm:col-span-2"><label class="label">资源名称</label><input v-model="resourceForm.name" class="input" placeholder="如：数据库 PostgreSQL" /></div>
              <div class="sm:col-span-2">
                <label class="label">网关节点（资源所在节点）</label>
                <select v-model="resourceForm.gateway_node_id" class="input">
                  <option value="">选择节点</option>
                  <option v-for="a in networkNodes" :key="a.id" :value="a.id">{{ a.name }}（{{ a.address }}）</option>
                </select>
              </div>
              <div><label class="label">目标 CIDR（留空 = 节点地址/32）</label><input v-model="resourceForm.target" class="input font-mono" placeholder="10.0.0.0/24" /></div>
              <div><label class="label">端口（可选）</label><input v-model.number="resourceForm.port" type="number" min="0" max="65535" class="input" /></div>
              <div>
                <label class="label">协议</label>
                <select v-model="resourceForm.protocol" class="input">
                  <option value="any">任意</option>
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                </select>
              </div>
              <div><label class="label">描述（可选）</label><input v-model="resourceForm.description" class="input" /></div>
            </div>
            <div class="mt-3 flex justify-end"><button class="btn-secondary" :disabled="!app.canOperate || savingResource" @click="saveResource">{{ savingResource ? '创建中…' : '创建资源' }}</button></div>
          </div>
        </template>
      </div>

      <!-- 策略 -->
      <div class="panel min-h-0 overflow-y-auto p-5">
        <div class="mb-3 flex items-center justify-between">
          <div>
            <h2 class="text-sm font-semibold text-white">策略（{{ policies.length }}）</h2>
            <p class="mt-0.5 text-xs text-slate-500">定义哪些源节点可以访问哪些资源。</p>
          </div>
        </div>
        <p v-if="!selectedNetworkId" class="py-6 text-center text-xs text-slate-500">请先选择网络</p>
        <template v-else>
          <div class="space-y-2">
            <div v-for="policy in policies" :key="policy.id" class="rounded-xl bg-ink-800/60 p-3.5 ring-1 ring-ink-600">
              <div class="flex flex-wrap items-center gap-2.5">
                <p class="min-w-0 flex-1 truncate text-sm font-medium text-slate-200">{{ policy.name }}</p>
                <span class="chip shrink-0 ring-1" :class="policy.enabled ? 'bg-emerald-500/10 text-emerald-300 ring-emerald-500/30' : 'bg-slate-500/10 text-slate-400 ring-slate-500/30'">{{ policy.enabled ? '已启用' : '已停用' }}</span>
                <button v-if="app.canOperate" class="chip shrink-0 bg-slate-500/10 text-slate-300 ring-1 ring-slate-500/30" @click="editPolicy(policy)">编辑</button>
                <button v-if="app.canOperate" class="chip shrink-0 bg-red-500/10 text-red-300 ring-1 ring-red-500/30" @click="removePolicy(policy)">删除</button>
              </div>
              <p class="mt-1.5 text-[11px] text-slate-500">
                源：{{ policy.source_label || (policy.source_node_ids.length ? policy.source_node_ids.map((id) => mesh.agentById(id)?.name || id).join('、') : '全部节点') }}
                · 资源：{{ policy.resource_ids.length }} 个
              </p>
            </div>
            <p v-if="!policies.length" class="py-5 text-center text-xs text-slate-500">暂无策略</p>
          </div>
          <div class="mt-4 rounded-xl border border-ink-700 bg-ink-900/50 p-4">
            <div class="mb-3 flex items-center justify-between">
              <p class="text-xs font-semibold text-slate-300">{{ editingPolicyId ? '编辑策略' : '新建策略' }}</p>
              <button v-if="editingPolicyId" class="text-xs text-cyan-300" @click="resetPolicyForm">取消编辑</button>
            </div>
            <div class="grid grid-cols-1 gap-3">
              <div><label class="label">策略名称</label><input v-model="policyForm.name" class="input" placeholder="如：运维组访问数据库" /></div>
              <div>
                <label class="label">源标签选择器（key=value，可选）</label>
                <input v-model="policyForm.source_label" class="input font-mono" placeholder="如：team=ops（与节点选择二选一）" />
              </div>
              <div>
                <label class="label">源节点（可多选）</label>
                <div class="flex max-h-28 flex-wrap gap-1.5 overflow-y-auto rounded-lg bg-ink-950/50 p-2 ring-1 ring-ink-600">
                  <button
                    v-for="a in networkNodes"
                    :key="a.id"
                    class="chip transition"
                    :class="policyForm.source_node_ids.includes(a.id) ? 'bg-cyan-500/15 text-cyan-300 ring-1 ring-cyan-500/40' : 'bg-ink-800 text-slate-400 ring-1 ring-ink-600 hover:text-slate-200'"
                    @click="toggleSourceNode(a.id)"
                  >
                    {{ a.name }}
                  </button>
                  <span v-if="!networkNodes.length" class="text-[11px] text-slate-500">该网络暂无节点</span>
                </div>
              </div>
              <div>
                <label class="label">允许访问的资源（可多选）</label>
                <div class="flex max-h-28 flex-wrap gap-1.5 overflow-y-auto rounded-lg bg-ink-950/50 p-2 ring-1 ring-ink-600">
                  <button
                    v-for="resource in resources"
                    :key="resource.id"
                    class="chip transition"
                    :class="policyForm.resource_ids.includes(resource.id) ? 'bg-emerald-500/15 text-emerald-300 ring-1 ring-emerald-500/40' : 'bg-ink-800 text-slate-400 ring-1 ring-ink-600 hover:text-slate-200'"
                    @click="toggleResource(resource.id)"
                  >
                    {{ resource.name }}
                  </button>
                  <span v-if="!resources.length" class="text-[11px] text-amber-400">请先在左侧创建资源</span>
                </div>
              </div>
              <label class="flex items-center gap-2 text-xs text-slate-400"><input v-model="policyForm.enabled" type="checkbox" class="accent-emerald-500" />启用策略</label>
            </div>
            <div class="mt-3 flex justify-end"><button class="btn-secondary" :disabled="!app.canOperate || savingPolicy" @click="savePolicy">{{ savingPolicy ? '保存中…' : editingPolicyId ? '保存修改' : '创建策略' }}</button></div>
          </div>
        </template>
      </div>
    </div>

    <!-- 出口网关（Egress） -->
    <div v-if="selectedNetworkId" class="panel shrink-0 p-5">
      <h2 class="text-sm font-semibold text-white">出口网关（Egress）</h2>
      <p class="mt-0.5 text-xs text-slate-500">指定一个节点作为对外出口，其他节点到该节点的 AllowedIPs 会加入下面的 CIDR（如 0.0.0.0/0 表示全部流量经出口网关转发）。保存后需「保存并发布」生效。</p>
      <div class="mt-4 flex flex-wrap items-end gap-3">
        <div>
          <label class="label">出口网关节点</label>
          <select v-model="egressNodeId" class="input !w-56">
            <option value="">不启用出口网关</option>
            <option v-for="a in networkNodes" :key="a.id" :value="a.id">{{ a.name }}（{{ a.address }}）</option>
          </select>
        </div>
        <div class="min-w-64 flex-1"><label class="label">转发 CIDR（逗号分隔）</label><input v-model="egressCIDRsText" class="input font-mono" placeholder="0.0.0.0/0" /></div>
        <button class="btn-secondary" :disabled="!app.canOperate || savingEgress" @click="saveEgress">{{ savingEgress ? '保存中…' : '保存出口配置' }}</button>
      </div>
    </div>
  </div>
</template>
