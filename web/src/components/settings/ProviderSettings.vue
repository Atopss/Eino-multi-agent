<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { Plus, Trash2, KeyRound, Check, Server, Globe, Boxes, Search, X, Network, Download } from 'lucide-vue-next'
import { useWorkspaceStore } from '../../stores/workspace'
import { api } from '../../api/client'
import type { Provider } from '../../types/api'
import AppModal from '../ui/AppModal.vue'

const ws = useWorkspaceStore()
const busy = ref(false)
const discovering = ref(false)
const discoverError = ref('')
const discoverSuccess = ref('')

// 「拉取可用模型」：调用后端中转站发现接口，把返回的模型列表暂存到 discoveredModels。
async function fetchModels() {
  const url = draft.baseURL.trim()
  if (!url) {
    discoverError.value = '请先填写 BaseURL 地址'
    return
  }
  discoverError.value = ''
  discoverSuccess.value = ''
  discovering.value = true
  try {
    const result = await api.discoverModels(url, draft.keyInput.trim())
    discoveredModels.value = result.models
    // 自动把拉取到的全部模型写入草稿（无需手动点「一键全部加入」），
    // 之后即可在聊天栏随时切换，无需重复添加商家。
    const set = new Set(draft.models)
    for (const m of result.models) set.add(m)
    draft.models = Array.from(set)
    // 若尚未选定默认模型，且只发现一个则自动选为默认
    if (!draft.chatModel && result.models.length === 1) {
      draft.chatModel = result.models[0]
    } else if (!draft.chatModel && result.models.length > 1) {
      draft.chatModel = result.models[0]
    }
    ws.showToast('success', `已自动加入 ${result.models.length} 个模型，可在聊天栏随时切换`)
    discoverSuccess.value = `检测到 ${result.models.length} 个可用模型` + (result.models.length <= 5 ? '：' + result.models.join(', ') : '')
  } catch (e) {
    discoverError.value = (e as Error).message || '模型发现失败'
  } finally {
    discovering.value = false
  }
}

// 最近一次从中转站拉取到的模型清单（未写入 draft.models 前先暂存）。
const discoveredModels = ref<string[]>([])

// 一键把拉取到的全部模型加入该中转站（可随时在聊天栏切换，无需重复添加）。
function addAllModels() {
  const set = new Set(draft.models)
  for (const m of discoveredModels.value) set.add(m)
  draft.models = Array.from(set)
  ws.showToast('success', `已加入 ${draft.models.length} 个模型，可在聊天栏随时切换`)
}

// 勾选 / 取消单个模型
function toggleModel(m: string) {
  if (draft.models.includes(m)) {
    draft.models = draft.models.filter((x) => x !== m)
  } else {
    draft.models = [...draft.models, m]
  }
}

// 已配置的商家列表（与后端 /api/providers 对齐）
const items = ref<Provider[]>([])
const draft = reactive<Provider & { keyInput: string }>({
  name: '',
  type: 'openai',
  baseURL: '',
  chatModel: '',
  endpointId: '',
  apiKey: '',
  keyInput: '',
  models: [],
})
const editing = ref(false)
const editingName = ref<string | null>(null) // null 表示新增
const editingHasKey = ref(false) // 编辑已有 Key 的商家时，提示用户"留空则保留"
const customModelInput = ref('')

// 内置商家搜索过滤
const query = ref('')

const allModels = computed(() => {
  const set = new Set(draft.models)
  if (draft.chatModel && draft.chatModel !== '__custom__') set.add(draft.chatModel)
  return Array.from(set)
})

function syncFromStore() {
  items.value = (ws.providers?.providers ?? []).map((p) => ({ ...p }))
}
watch(() => ws.providers, syncFromStore, { immediate: true })

const presets = computed(() => ws.providers?.presets ?? [])

// 按商家名分组（国际 / 国产 / 本地），并按搜索词过滤
const presetGroups = computed(() => {
  const q = query.value.trim().toLowerCase()
  const all = q ? presets.value.filter((p) => p.name.toLowerCase().includes(q)) : presets.value
  const intl = [
    'OpenAI', 'Anthropic (Claude)', 'xAI (Grok)',
  ]
  const local = ['Ark (火山方舟)']
  const intlSet = new Set(intl)
  const localSet = new Set(local)
  return {
    international: all.filter((p) => intlSet.has(p.name)),
    domestic: all.filter((p) => !intlSet.has(p.name) && !localSet.has(p.name)),
    local: all.filter((p) => localSet.has(p.name)),
  }
})

function groupLabel(key: string) {
  return key === 'international' ? '国际' : key === 'domestic' ? '国产' : '本地 / 其他'
}

function isConfigured(name: string): boolean {
  return items.value.some((p) => p.name === name)
}

function openRelayTemplate() {
  resetDraft()
  draft.name = '中转站网关'
  draft.type = 'openai'
  draft.baseURL = ''
  draft.chatModel = ''
  editing.value = true
  editingName.value = null
}

function resetDraft() {
  draft.name = ''
  draft.type = 'openai'
  draft.baseURL = ''
  draft.chatModel = ''
  draft.endpointId = ''
  draft.apiKey = ''
  draft.keyInput = ''
  draft.models = []
  discoveredModels.value = []
  editingHasKey.value = false
  customModelInput.value = ''
  discoverError.value = ''
  discoverSuccess.value = ''
  editing.value = false
  editingName.value = null
}

// 点击内置预设：已配置则编辑，否则以预设填充新建
function usePreset(p: Provider) {
  const existing = items.value.find((x) => x.name === p.name)
  editing.value = true
  editingName.value = existing ? existing.name : null
  draft.name = p.name
  draft.type = p.type
  draft.baseURL = p.baseURL
  draft.chatModel = existing?.chatModel || p.chatModel
  draft.endpointId = existing?.endpointId || p.endpointId || ''
  draft.apiKey = existing?.apiKey || ''
  // 优先用已保存的模型清单（含用户之前拉取发现并加入的），回退到预设
  draft.models = [...new Set([...(existing?.models ?? p.models ?? []), draft.chatModel])]
  draft.keyInput = ''
  editingHasKey.value = !!existing?.hasKey
  customModelInput.value = ''
}

function editProvider(p: Provider) {
  editing.value = true
  editingName.value = p.name
  draft.name = p.name
  draft.type = p.type
  draft.baseURL = p.baseURL
  draft.chatModel = p.chatModel
  draft.endpointId = p.endpointId || ''
  draft.apiKey = p.apiKey || ''
  const preset = presets.value.find((x) => x.name === p.name)
  // 用该商家已保存的全量模型清单（用户拉取发现并加入的），回退预设
  draft.models = [...new Set([...(p.models ?? preset?.models ?? []), p.chatModel])]
  draft.keyInput = ''
  editingHasKey.value = !!p.hasKey
  customModelInput.value = ''
}

function removeProvider(name: string) {
  items.value = items.value.filter((p) => p.name !== name)
  if (editingName.value === name) resetDraft()
  saveAll() // 同步到后端，让删除真正生效
}

async function commitDraft() {
  const name = draft.name.trim()
  if (!name) {
    ws.showToast('error', '请填写商家名称')
    return
  }
  // 改名时检查新名称是否与已有商家冲突（排除自身）
  if (editingName.value && editingName.value !== name && items.value.some((p) => p.name === name)) {
    ws.showToast('error', `名称 "${name}" 已被使用，请换个名称`)
    return
  }
  if (draft.type !== 'ark') draft.type = 'openai'
  const chatModel =
    draft.chatModel === '__custom__' ? customModelInput.value.trim() : draft.chatModel.trim()
  if (!chatModel) {
    ws.showToast('error', '请填写模型名')
    return
  }
  const entry: Provider = {
    name,
    type: draft.type,
    baseURL: draft.baseURL.trim(),
    chatModel,
    endpointId: draft.endpointId?.trim() || '',
    apiKey: draft.keyInput.trim(),
    // 该令牌支持的全部模型（拉取 / 勾选所得），写入后可在聊天栏自由切换
    models: draft.models,
  }
  if (editingName.value && editingName.value !== name) {
    // 改名：删除旧条目再添加新条目
    items.value = items.value.filter((p) => p.name !== editingName.value)
    items.value.push(entry)
  } else {
    const idx = items.value.findIndex((p) => p.name === name)
    if (idx >= 0) items.value[idx] = entry
    else items.value.push(entry)
  }
  await saveAll()
  resetDraft()
}

async function saveAll() {
  busy.value = true
  try {
    await api.saveProviders({ providers: items.value })
    ws.showToast('success', '模型服务已保存，服务已热重载')
    await ws.loadProviders()
    await ws.loadModels()
    await ws.loadAgents()
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}

function typeBadgeClass(t: string) {
  return t === 'ark'
    ? 'bg-amber-400/15 text-amber-300'
    : 'bg-brand/15 text-brand-400'
}
</script>

<template>
  <div class="space-y-4">
    <!-- 我的服务 -->
    <section class="panel space-y-2 p-3">
      <div class="flex items-center justify-between">
        <h3 class="flex items-center gap-1.5 text-sm font-medium text-slate-200">
          <Server :size="14" class="text-accent" /> 我的服务
        </h3>
        <button class="btn-outline !py-1.5 text-xs" @click="usePreset({ name: '自定义', type: 'openai', baseURL: '', chatModel: '' })">
          <Plus :size="13" /> 新增
        </button>
        <button class="btn-outline !py-1.5 text-xs" @click="openRelayTemplate">
          <Network :size="13" /> 中转站网关
        </button>
      </div>
      <p v-if="!items.length" class="rounded-card border border-dashed border-ink-700 px-4 py-6 text-center text-sm text-slate-500">
        还没有配置任何服务。从下方「内置商家」点选，或点右上角「新增」自定义。
      </p>
      <div
        v-for="p in items"
        :key="p.name"
        class="flex items-center gap-2 rounded-lg border border-ink-800 bg-ink-900/40 p-2.5"
      >
        <component :is="p.type === 'ark' ? Server : Globe" :size="15" class="shrink-0 text-accent" />
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium text-slate-100">{{ p.name }}</p>
          <div class="mt-0.5 flex flex-wrap items-center gap-1">
            <span v-if="p.type === 'ark'" class="rounded px-1.5 py-0.5 text-[10px] font-medium bg-amber-400/15 text-amber-300">Ark</span>
            <span class="text-2xs text-slate-500">{{ p.chatModel || p.baseURL }}</span>
          </div>
        </div>
        <button class="btn-ghost !p-1.5" title="编辑" @click="editProvider(p)"><KeyRound :size="14" /></button>
        <button class="btn-ghost !p-1.5 hover:!text-danger" title="删除" @click="removeProvider(p.name)"><Trash2 :size="14" /></button>
      </div>
    </section>

    <!-- 内置商家 -->
    <section class="panel space-y-2 p-3">
      <h3 class="flex items-center gap-1.5 text-sm font-medium text-slate-200">
        <Boxes :size="14" class="text-accent" /> 内置商家
      </h3>
      <p class="text-2xs text-slate-500">选择商家后只需填写 API Key，地址与默认模型已为你预填。</p>
      <div class="relative">
        <Search :size="14" class="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-slate-500" />
        <input v-model="query" placeholder="搜索商家…" class="input !pl-8" />
      </div>
      <div class="space-y-3">
        <div v-for="(group, key) in presetGroups" :key="key">
          <template v-if="group.length">
            <h4 class="mb-1 text-xs font-medium text-slate-400">{{ groupLabel(key) }}</h4>
            <div class="grid grid-cols-1 gap-1">
              <button
                v-for="p in group"
                :key="p.name"
                class="group flex w-full items-center justify-between rounded-lg border border-ink-800 bg-ink-900/40 px-3 py-2.5 text-left transition-colors hover:border-brand/40 hover:bg-ink-800/60"
                @click="usePreset(p)"
              >
                <div class="min-w-0">
                  <p class="truncate text-sm font-medium text-slate-100">{{ p.name }}</p>
                  <p class="truncate text-2xs text-slate-500">{{ p.chatModel }}</p>
                </div>
                <span
                  class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium"
                  :class="isConfigured(p.name) ? 'bg-brand/15 text-brand-400' : 'bg-ink-700 text-slate-400'"
                >
                  {{ isConfigured(p.name) ? '已配置' : '添加' }}
                </span>
              </button>
            </div>
          </template>
        </div>
        <p v-if="!query || Object.values(presetGroups).every((g) => !g.length)" class="text-2xs text-slate-500">
          没有匹配的商家。
        </p>
      </div>
    </section>

    <!-- 编辑弹窗 -->
    <AppModal :open="editing" :title="editingName ? '编辑服务' : '新增服务'" @close="resetDraft">
      <div class="space-y-3">
        <input v-model="draft.name" placeholder="名称（如 OpenAI、我的网关）" class="input" />
        <div class="grid grid-cols-2 gap-2">
          <label class="block text-xs text-slate-400">
            类型
            <select v-model="draft.type" class="input mt-1">
              <option value="openai">OpenAI 兼容</option>
              <option value="ark">Ark (火山方舟)</option>
            </select>
          </label>
          <label class="block text-xs text-slate-400">
            默认模型名
            <select v-model="draft.chatModel" class="input mt-1">
              <option v-for="m in allModels" :key="m" :value="m">{{ m }}</option>
              <option value="__custom__">自定义（手动输入）</option>
            </select>
            <input
              v-if="draft.chatModel === '__custom__'"
              v-model="customModelInput"
              placeholder="输入模型名"
              class="input mt-1"
            />
          </label>
        </div>
        <label class="block text-xs text-slate-400">
          BaseURL（OpenAI 兼容地址；Ark 可留空）
          <input v-model="draft.baseURL" placeholder="OpenAI 兼容网关地址，如 https://your-relay.com/v1" class="input mt-1" />
        </label>
        <label class="block text-xs text-slate-400">
          API Key
          <input v-model="draft.keyInput" type="password" placeholder="仅写入本地 .env，不回显、不落盘" class="input mt-1" />
          <p v-if="editingHasKey && !draft.keyInput" class="mt-1 text-2xs text-emerald-400">已保存 Key（留空则保留不变）</p>
          <p v-if="editingHasKey && draft.keyInput" class="mt-1 text-2xs text-amber-400">已输入新 Key，保存后将覆盖旧值</p>
        </label>
        <button
          class="btn-outline w-full !py-2 text-xs"
          :disabled="discovering"
          @click="fetchModels"
          title="调用中转站/网关的 /models 接口，自动拉取可用模型列表"
        >
          <Download :size="14" class="mr-1" />
          {{ discovering ? '正在拉取模型列表…' : '拉取可用模型' }}
        </button>

        <!-- 拉取结果：一键把该令牌支持的所有模型加入，之后可在聊天栏自由切换 -->
        <div v-if="discoveredModels.length" class="rounded-lg border border-ink-800 bg-ink-900/40 p-2.5">
          <div class="flex items-center justify-between">
            <span class="text-2xs text-slate-400">检测到 {{ discoveredModels.length }} 个可用模型</span>
            <button
              class="btn-outline !py-1 !px-2 text-2xs"
              :disabled="discoveredModels.every((m) => draft.models.includes(m))"
              @click="addAllModels"
            >
              <Download :size="12" class="mr-1" /> 一键全部加入 ({{ discoveredModels.length }})
            </button>
          </div>
          <div class="mt-2 flex flex-wrap gap-1">
            <button
              v-for="m in discoveredModels"
              :key="m"
              class="rounded px-2 py-1 text-2xs transition-colors"
              :class="draft.models.includes(m)
                ? 'bg-brand/20 text-brand-300'
                : 'bg-ink-800 text-slate-400 hover:text-slate-200'"
              @click="toggleModel(m)"
            >
              {{ m }}<span v-if="m === draft.chatModel" class="ml-0.5 opacity-70">（默认）</span>
            </button>
          </div>
          <p class="mt-2 text-2xs text-slate-500">
            勾选的模型将全部写入该中转站，保存后可在聊天栏随时切换，无需重复添加。
          </p>
        </div>

        <p v-if="discoverError" class="mt-1 text-[11px] text-danger-400">{{ discoverError }}</p>
        <p v-if="discoverSuccess" class="mt-1 text-[11px] text-emerald-400">{{ discoverSuccess }}</p>
      </div>
      <template #footer>
        <button class="btn-ghost" @click="resetDraft"><X :size="15" /> 取消</button>
        <button class="btn-primary" :disabled="busy || !draft.name.trim()" @click="commitDraft">
          <Check :size="15" /> 保存
        </button>
      </template>
    </AppModal>
  </div>
</template>
