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
} from '../types/api'
import { uid, truncate, formatTime } from '../utils/format'

const SESSIONS_KEY = 'eino.sessions.v1'

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
  const tools = ref<ToolInfo[]>([])

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

  async function loadModels() {
    try {
      models.value = await api.getModels()
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
      loadTools(),
    ])
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
    tools,
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
