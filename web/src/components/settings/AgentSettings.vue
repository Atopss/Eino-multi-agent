<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { Bot, Plus, Trash2, Copy, SlidersHorizontal, X } from 'lucide-vue-next'
import { useWorkspaceStore } from '../../stores/workspace'
import { api } from '../../api/client'

const props = defineProps<{ newNonce: number }>()
const ws = useWorkspaceStore()
const busy = ref(false)

const agentForm = reactive({
  oldName: '',
  name: '',
  modelID: '',
  systemPrompt: '',
  role: '',
  task: '',
  needTools: true,
})
const agentEditing = ref(false)

// 角色模板：选中后自动填充系统提示词
const roleTemplates = [
  { label: '自定义', prompt: '' },
  { label: '资深后端工程师', prompt: '你是一名资深后端工程师，精通 Go / 分布式系统与高并发设计。回答时优先给出可运行的代码与架构权衡。' },
  { label: '产品经理', prompt: '你是一名产品经理，擅长需求拆解、用户故事与优先级排序。回答聚焦价值、场景与取舍。' },
  { label: '翻译官', prompt: '你是一名专业翻译，精通中英互译，保持术语准确与语气一致，仅输出译文与必要说明。' },
  { label: '数据分析师', prompt: '你是一名数据分析师，擅长用数据讲故事。给出指标定义、分析思路与可视化建议。' },
  { label: '文案写手', prompt: '你是一名文案写手，擅长小红书 / 公众号风格，标题吸睛、结构清晰、语气亲切。' },
]
const selectedTemplate = ref('')

function applyTemplate() {
  const t = roleTemplates.find((x) => x.label === selectedTemplate.value)
  if (t && t.prompt) agentForm.systemPrompt = t.prompt
}

// 重名即时校验：新建或改名成已有名字时标红并禁用保存
const nameConflict = computed(() => {
  const n = agentForm.name.trim()
  if (!n) return false
  if (agentForm.oldName && n === agentForm.oldName) return false
  return ws.agents.some((a) => a.name === n)
})

function resetAgentForm() {
  agentForm.oldName = ''
  agentForm.name = ''
  agentForm.modelID = ''
  agentForm.systemPrompt = ''
  agentForm.role = ''
  agentForm.task = ''
  agentForm.needTools = true
  selectedTemplate.value = ''
}
function newAgentForm() {
  agentEditing.value = true
  resetAgentForm()
}
function editAgent(name: string) {
  const a = ws.agents.find((x) => x.name === name)
  if (!a) return
  agentEditing.value = true
  agentForm.oldName = a.name
  agentForm.name = a.name
  agentForm.modelID = a.model
  agentForm.systemPrompt = a.systemPrompt
  agentForm.role = ''
  agentForm.task = ''
  agentForm.needTools = a.needTools
  selectedTemplate.value = ''
}
function cloneAgent(name: string) {
  const a = ws.agents.find((x) => x.name === name)
  if (!a) return
  agentEditing.value = true
  agentForm.oldName = ''
  agentForm.name = name + '-副本'
  agentForm.modelID = a.model
  agentForm.systemPrompt = a.systemPrompt
  agentForm.role = ''
  agentForm.task = ''
  agentForm.needTools = a.needTools
  selectedTemplate.value = ''
}
// 系统提示词留空时，用「角色 + 任务」自动合成（后端仅存 systemPrompt）
function buildSystemPrompt(): string {
  const sp = agentForm.systemPrompt.trim()
  if (sp) return sp
  const role = agentForm.role.trim()
  const task = agentForm.task.trim()
  if (!role && !task) return ''
  const parts: string[] = []
  if (role) parts.push(`你是一名${role}。`)
  if (task) parts.push(`你的主要任务是：${task}`)
  return parts.join('')
}

async function saveAgent() {
  if (!agentForm.name || nameConflict.value) return
  busy.value = true
  try {
    const payload = {
      oldName: agentForm.oldName,
      name: agentForm.name,
      modelID: agentForm.modelID,
      systemPrompt: buildSystemPrompt(),
      needTools: agentForm.needTools,
    }
    if (agentForm.oldName) {
      await api.updateAgent(payload)
      ws.showToast('success', '智能体已更新：' + agentForm.name)
    } else {
      await api.createAgent(payload)
      ws.showToast('success', '智能体已创建：' + agentForm.name)
    }
    await ws.loadAgents()
    if (!ws.activeAgent) ws.activeAgent = ws.agents[0]?.name ?? ''
    agentEditing.value = false
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}

// 删除：内联二次确认，替代原生 confirm
const pendingDelete = ref<string | null>(null)
function askRemove(name: string) {
  pendingDelete.value = name
}
function cancelRemove() {
  pendingDelete.value = null
}
async function confirmRemove() {
  const name = pendingDelete.value
  pendingDelete.value = null
  if (!name) return
  busy.value = true
  try {
    await api.deleteAgent({ name })
    ws.showToast('success', '已删除：' + name)
    await ws.loadAgents()
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}

// 来自侧边栏「新建智能体」：收到 nonce 后切片到本页并展开新建表单
watch(
  () => props.newNonce,
  (n) => {
    if (n > 0) newAgentForm()
  },
)
</script>

<template>
  <div class="space-y-3">
    <div class="flex items-center justify-between">
      <h3 class="text-sm font-medium text-slate-200">智能体列表</h3>
      <button class="btn-outline !py-1.5 text-xs" @click="newAgentForm"><Plus :size="13" /> 新建</button>
    </div>
    <p v-if="!ws.agents.length" class="rounded-card border border-dashed border-ink-700 px-4 py-6 text-center text-sm text-slate-500">
      还没有智能体。点击「新建」创建第一个，或在上方引导里一键开始。
    </p>
    <div
      v-for="a in ws.agents"
      :key="a.name"
      class="panel flex items-center gap-2 rounded-lg p-2.5"
    >
      <Bot :size="15" class="shrink-0 text-accent" />
      <div class="min-w-0 flex-1">
        <p class="truncate text-sm font-medium text-slate-100">{{ a.name }}</p>
        <div class="mt-0.5 flex flex-wrap items-center gap-1">
          <span class="text-2xs text-slate-500">{{ a.model }}</span>
          <span v-if="a.name === ws.agents[0]?.name" class="rounded bg-brand/15 px-1.5 py-0.5 text-[10px] font-medium text-brand-400">协调者</span>
          <span v-if="!a.needTools" class="rounded bg-slate-500/15 px-1.5 py-0.5 text-[10px] font-medium text-slate-300">纯聊天</span>
        </div>
      </div>
      <template v-if="pendingDelete === a.name">
        <span class="text-2xs text-danger">确认删除？</span>
        <button class="btn-outline !px-2 !py-1 text-xs hover:!border-danger/50 hover:!text-danger" @click="confirmRemove">删除</button>
        <button class="btn-ghost !p-1.5" title="取消" @click="cancelRemove"><X :size="14" /></button>
      </template>
      <template v-else>
        <button class="btn-ghost !p-1.5" title="复制" @click="cloneAgent(a.name)"><Copy :size="14" /></button>
        <button class="btn-ghost !p-1.5" title="编辑" @click="editAgent(a.name)"><SlidersHorizontal :size="14" /></button>
        <button class="btn-ghost !p-1.5 hover:!text-danger" title="删除" @click="askRemove(a.name)"><Trash2 :size="14" /></button>
      </template>
    </div>

    <div v-if="agentEditing" class="panel space-y-2 p-3">
      <input
        v-model="agentForm.name"
        placeholder="名称（必填）"
        class="input"
        :class="{ '!border-danger focus:!ring-danger/30': nameConflict }"
      />
      <p v-if="nameConflict" class="text-2xs text-danger">已存在同名智能体，请换一个名称</p>
      <select v-model="agentForm.modelID" class="input">
        <option value="">模型（默认）</option>
        <option v-for="o in (ws.models?.chatOptions ?? [])" :key="o.value" :value="o.value">{{ o.label }}</option>
      </select>
      <select v-model="selectedTemplate" class="input" @change="applyTemplate">
        <option v-for="t in roleTemplates" :key="t.label" :value="t.label">
          {{ t.label === '自定义' ? '角色模板（可选）' : t.label }}
        </option>
      </select>
      <textarea v-model="agentForm.systemPrompt" rows="3" placeholder="系统提示词（可选，留空则用下方角色/任务自动生成；选模板可自动填充）" class="input resize-y"></textarea>
      <input v-model="agentForm.role" placeholder="角色（如：资深后端工程师，仅在提示词留空时生效）" class="input" />
      <input v-model="agentForm.task" placeholder="主要任务（仅在提示词留空时生效）" class="input" />
      <div class="space-y-2 rounded-lg border border-ink-800 bg-ink-900/40 p-2.5">
        <label class="flex items-center justify-between gap-2 text-sm">
          <span class="text-slate-300">需要工具</span>
          <input v-model="agentForm.needTools" type="checkbox" class="h-4 w-4 accent-accent" />
        </label>
        <p class="-mt-1 text-2xs text-slate-500">关闭后该智能体不挂载任何工具（纯聊天）。</p>
      </div>
      <div class="flex gap-2">
        <button class="btn-primary flex-1" :disabled="busy || !agentForm.name || nameConflict" @click="saveAgent">保存</button>
        <button class="btn-ghost" @click="agentEditing = false">取消</button>
      </div>
    </div>
  </div>
</template>
