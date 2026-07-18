import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../api/client'
import type {
  AgentInfo,
  RagStatus,
  SettingsData,
  ModelsData,
  ToolInfo,
  SessionMeta,
  ProvidersData,
  ModelOption,
} from '../types/api'
import { uid, truncate, formatTime } from '../utils/format'

const SESSIONS_KEY = 'eino.sessions.v1'
const ACTIVE_MODEL_KEY = 'eino.activeModel.v1'
const MODEL_VISIBILITY_KEY = 'eino.modelVisibility.v1'

// 解析 localStorage 中保存的全局所选模型（可能为脏值，需与可选列表校验）。
function loadActiveModel(): string {
  try {
    return localStorage.getItem(ACTIVE_MODEL_KEY) || ''
  } catch {
    return ''
  }
}

function loadLocalSessions(): SessionMeta[] {
  try {
    const raw = localStorage.getItem(SESSIONS_KEY)
    if (!raw) return []
    const arr = JSON.parse(raw)
    return Array.isArray(arr) ? (arr as SessionMeta[]) : []
  } catch {
    return []
  }
}

export const useWorkspaceStore = defineStore('workspace', () => {
  const agents = ref<AgentInfo[]>([])
  const activeAgent = ref<string>('')
  const ragStatus = ref<RagStatus | null>(null)
  const settings = ref<SettingsData | null>(null)
  const models = ref<ModelsData | null>(null)
  const providers = ref<ProvidersData | null>(null)
  const tools = ref<ToolInfo[]>([])

  // 全局所选模型（聊天栏处切换，与具体智能体解耦）。localStorage 持久化。
  const activeModel = ref<string>(loadActiveModel())

  // 按商家分组的可用模型（仅可见模型，供聊天栏选择器使用）。
  const chatModelGroups = computed<{ provider: string; options: ModelOption[] }[]>(() => {
    const opts = models.value?.chatOptions ?? []
    const map = new Map<string, ModelOption[]>()
    for (const o of opts) {
      if (!isModelVisible(o.value)) continue
      const key = o.provider || '其他'
      if (!map.has(key)) map.set(key, [])
      map.get(key)!.push(o)
    }
    return Array.from(map, ([provider, options]) => ({ provider, options }))
  })

  // 当前所选模型对象（含 provider/kind/note 等元信息）。
  const activeModelOption = computed<ModelOption | null>(() => {
    const cur = activeModel.value
    if (!cur) return null
    return models.value?.chatOptions.find((o) => o.value === cur) ?? null
  })

  // 设置/切换全局模型，并持久化；传入空串表示回退到默认。
  function setActiveModel(value: string) {
    const groups = chatModelGroups.value
    const all = groups.flatMap((g) => g.options.map((o) => o.value))
    // 仅接受可选列表中的值，避免脏数据
    activeModel.value = all.includes(value) ? value : all[0] ?? ''
    try {
      localStorage.setItem(ACTIVE_MODEL_KEY, activeModel.value)
    } catch {
      /* ignore */
    }
  }

  // 加载完成后确保 activeModel 落在可选列表内，否则回退首个。
  function ensureActiveModel() {
    const groups = chatModelGroups.value
    const all = groups.flatMap((g) => g.options.map((o) => o.value))
    if (all.length === 0) return
    if (!all.includes(activeModel.value)) {
      activeModel.value = all[0]
      try {
        localStorage.setItem(ACTIVE_MODEL_KEY, activeModel.value)
      } catch {
        /* ignore */
      }
    }
  }

  // --- 模型显隐偏好（localStorage 持久化）---
  function loadModelVisibility(): Record<string, boolean> {
    try {
      const raw = localStorage.getItem(MODEL_VISIBILITY_KEY)
      return raw ? JSON.parse(raw) : {}
    } catch { return {} }
  }
  const modelVisibility = ref<Record<string, boolean>>(loadModelVisibility())

  function persistModelVisibility() {
    try { localStorage.setItem(MODEL_VISIBILITY_KEY, JSON.stringify(modelVisibility.value)) } catch { /* ignore */ }
  }

  /** 某个模型值是否可见（未记录过则默认可见） */
  function isModelVisible(value: string): boolean {
    if (value in modelVisibility.value) return modelVisibility.value[value]
    return true
  }

  /** 切换某个模型的显隐 */
  function toggleModelVisible(value: string) {
    modelVisibility.value[value] = !isModelVisible(value)
    persistModelVisibility()
  }

  /** 一键开关某个提供商下的所有模型 */
  function setProviderVisible(provider: string, models: string[], on: boolean) {
    for (const m of models) modelVisibility.value[m] = on
    persistModelVisibility()
  }

  /** 所有模型（未过滤），供管理面板使用 */
  const allModelGroups = computed<{ provider: string; options: ModelOption[] }[]>(() => {
    const opts = models.value?.chatOptions ?? []
    const map = new Map<string, ModelOption[]>()
    for (const o of opts) {
      const key = o.provider || '其他'
      if (!map.has(key)) map.set(key, [])
      map.get(key)!.push(o)
    }
    return Array.from(map, ([provider, options]) => ({ provider, options }))
  })

  const sessions = ref<SessionMeta[]>(loadLocalSessions())
  const activeSessionId = ref<string>('')

  const busying = ref(false)
  const toast = ref<{ kind: 'info' | 'error' | 'success'; text: string } | null>(null)
  let toastTimer: number | undefined

  const activeAgentInfo = computed(() =>
    agents.value.find((a) => a.name === activeAgent.value),
  )

  // 侧边栏：每个智能体的会话列表展开状态（按智能体名索引）。
  const expandedAgents = ref<Record<string, boolean>>({})

  // 点击智能体标题：设为当前智能体并切换其会话列表展开。
  function toggleAgentGroup(name: string) {
    activeAgent.value = name
    expandedAgents.value = { ...expandedAgents.value, [name]: !expandedAgents.value[name] }
  }

  // 按智能体分组的会话列表（用于侧边栏折叠展示）。
  const sessionsByAgent = computed<Record<string, SessionMeta[]>>(() => {
    const map: Record<string, SessionMeta[]> = {}
    for (const a of agents.value) map[a.name] = []
    const fallback = agents.value[0]?.name ?? ''
    for (const s of sessions.value) {
      const key = s.agent || fallback
      ;(map[key] ??= []).push(s)
    }
    return map
  })

  // 将无 agent 字段的历史会话按当前智能体归并，避免分组时丢失。
  function backfillSessionAgents() {
    if (!activeAgent.value) return
    let changed = false
    for (const s of sessions.value) {
      if (!s.agent) {
        s.agent = activeAgent.value
        changed = true
      }
    }
    if (changed) persistSessions()
  }

  function showToast(kind: 'info' | 'error' | 'success', text: string) {
    toast.value = { kind, text }
    if (toastTimer) clearTimeout(toastTimer)
    toastTimer = window.setTimeout(() => (toast.value = null), 3200)
  }

  // 亮/暗主题（默认暗色，偏好存 localStorage）
  const theme = ref<'light' | 'dark'>(
    localStorage.getItem('eino-theme') === 'dark'
      ? 'dark'
      : localStorage.getItem('eino-theme') === 'light'
        ? 'light'
        : 'dark',
  )

  let currentHlTheme = ''
  async function applyTheme() {
    const isDark = theme.value === 'dark'
    document.documentElement.classList.toggle('dark', isDark)

    // 动态加载 highlight.js 主题（亮色用 github，暗色用 github-dark）
    const themeName = isDark ? 'github-dark' : 'github'
    if (currentHlTheme === themeName) return
    currentHlTheme = themeName

    // 清除旧的 hljs 样式标签
    document.querySelectorAll('style[data-hljs]').forEach((el) => el.remove())

    // 记录 import 前已有的 style 标签，避免误标记 Vue scoped 样式或 style.css
    const before = new Set(document.querySelectorAll('style'))

    try {
      if (isDark) {
        await import('highlight.js/styles/github-dark.css')
      } else {
        await import('highlight.js/styles/github.css')
      }
    } catch {
      // 导入失败时重置状态，下次重试
      currentHlTheme = ''
      return
    }

    // 只标记 import 后新注入的 style 标签（Vite 动态 CSS 注入）
    document.querySelectorAll('style').forEach((el) => {
      if (before.has(el)) return
      const text = el.textContent || ''
      if (text.includes('.hljs')) {
        el.setAttribute('data-hljs', themeName)
      }
    })
  }
  function toggleTheme() {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
    localStorage.setItem('eino-theme', theme.value)
    applyTheme()
  }

  function persistSessions() {
    try {
      localStorage.setItem(SESSIONS_KEY, JSON.stringify(sessions.value))
    } catch {
      /* ignore quota */
    }
  }

  async function loadAgents() {
    try {
      agents.value = await api.getAgents()
      if (!activeAgent.value && agents.value.length) {
        activeAgent.value = agents.value[0].name
      }
      if (activeAgent.value) {
        expandedAgents.value = { [activeAgent.value]: true }
        backfillSessionAgents()
      }
    } catch (e) {
      showToast('error', '加载智能体失败：' + (e as Error).message)
    }
  }

  async function loadRagStatus() {
    try {
      ragStatus.value = await api.ragStatus()
    } catch {
      ragStatus.value = null
    }
  }

  async function loadSettings() {
    try {
      settings.value = await api.getSettings()
    } catch {
      /* ignore */
    }
  }

  /** 清除 localStorage 中引用已不存在的模型的脏数据 */
  function cleanStaleModelData() {
    const validValues = new Set((models.value?.chatOptions ?? []).map((o) => o.value))
    // 清理 modelVisibility 中的无效条目
    let visChanged = false
    for (const key of Object.keys(modelVisibility.value)) {
      if (!validValues.has(key)) {
        delete modelVisibility.value[key]
        visChanged = true
      }
    }
    if (visChanged) persistModelVisibility()
    // 清理 activeModel 脏值
    if (activeModel.value && !validValues.has(activeModel.value)) {
      activeModel.value = ''
      try { localStorage.removeItem(ACTIVE_MODEL_KEY) } catch { /* ignore */ }
    }
  }

  async function loadModels() {
    try {
      models.value = await api.getModels()
      cleanStaleModelData()
    } catch {
      /* ignore */
    }
  }

  async function loadProviders() {
    try {
      providers.value = await api.getProviders()
    } catch {
      /* ignore */
    }
  }

  async function loadTools() {
    try {
      tools.value = (await api.getTools()).tools
    } catch {
      tools.value = []
    }
  }

  async function init() {
    applyTheme()
    await Promise.all([
      loadAgents(),
      loadRagStatus(),
      loadSettings(),
      loadModels(),
      loadProviders(),
      loadTools(),
    ])
    ensureActiveModel()
    await refreshSessions()
  }

  /** 与后端会话列表对齐，剔除已不存在的本地元数据。 */
  async function refreshSessions() {
    try {
      const { sessions: serverIds } = await api.getSessionList()
      const local = sessions.value
      const existing = new Set(serverIds)
      const kept = local.filter((s) => existing.has(s.id))
      if (kept.length !== local.length) {
        sessions.value = kept
        persistSessions()
      }
    } catch {
      /* 忽略：本地元数据仍可用 */
    }
  }

  function createSessionMeta(title = '新对话', agent = activeAgent.value): string {
    const id = uid('sess')
    sessions.value.unshift({
      id,
      title,
      preview: '',
      updatedAt: Date.now(),
      agent,
    })
    persistSessions()
    activeSessionId.value = id
    return id
  }

  function touchSession(id: string, patch: Partial<SessionMeta>) {
    const idx = sessions.value.findIndex((s) => s.id === id)
    if (idx === -1) return
    const cur = sessions.value[idx]
    const updated: SessionMeta = {
      ...cur,
      ...patch,
      updatedAt: Date.now(),
    }
    sessions.value.splice(idx, 1)
    sessions.value.unshift(updated)
    persistSessions()
  }

  function setActiveSession(id: string) {
    activeSessionId.value = id
  }

  async function deleteSession(id: string) {
    try {
      await api.deleteSession(id)
    } catch (e) {
      showToast('error', '删除会话失败：' + (e as Error).message)
    }
    const idx = sessions.value.findIndex((s) => s.id === id)
    if (idx !== -1) {
      sessions.value.splice(idx, 1)
      persistSessions()
    }
    if (activeSessionId.value === id) {
      activeSessionId.value = sessions.value[0]?.id ?? ''
    }
  }

  function sessionTitleFor(firstUserText: string): string {
    return truncate(firstUserText.replace(/\s+/g, ' ').trim(), 18) || '新对话'
  }

  function sessionPreviewFor(text: string): string {
    const clean = text.replace(/\s+/g, ' ').trim()
    return truncate(clean, 42)
  }

  function formatUpdated(ts: number): string {
    return formatTime(ts)
  }

  return {
    agents,
    activeAgent,
    ragStatus,
    settings,
    models,
    providers,
    tools,
    activeModel,
    chatModelGroups,
    allModelGroups,
    modelVisibility,
    isModelVisible,
    toggleModelVisible,
    setProviderVisible,
    activeModelOption,
    setActiveModel,
    ensureActiveModel,
    sessions,
    activeSessionId,
    expandedAgents,
    sessionsByAgent,
    busying,
    toast,
    activeAgentInfo,
    showToast,
    theme,
    toggleTheme,
    persistSessions,
    loadAgents,
    loadRagStatus,
    loadSettings,
    loadModels,
    loadProviders,
    loadTools,
    init,
    refreshSessions,
    createSessionMeta,
    touchSession,
    setActiveSession,
    deleteSession,
    toggleAgentGroup,
    sessionTitleFor,
    sessionPreviewFor,
    formatUpdated,
  }
})
