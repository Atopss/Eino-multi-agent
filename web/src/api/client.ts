import type {
  AgentInfo,
  RagStatus,
  SettingsData,
  ModelsData,
  ProvidersData,
  ToolInfo,
  DirEntry,
  RagSearchResult,
  SkillInfo,
  StreamEvent,
  ChatMessagePayload,
} from '../types/api'

const BASE = ''

// SSE 解析缓冲上限（1MB）：防止后端/代理持续发送不含终止符的畸形流导致前端内存无限膨胀。
const MAX_SSE_BUFFER = 1024 * 1024

// 本地部署工具无需登录鉴权，仅声明 JSON 内容类型即可。
function defaultHeaders(): Record<string, string> {
  return { 'Content-Type': 'application/json' }
}

export async function request<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const resp = await fetch(BASE + path, {
    headers: defaultHeaders(),
    ...init,
  })
  if (!resp.ok) {
    let detail = resp.statusText
    try {
      const body = await resp.json()
      if (body && body.error) detail = body.error
    } catch {
      /* ignore */
    }
    throw new Error(detail || `请求失败 (${resp.status})`)
  }
  return (await resp.json()) as T
}

function postJSON<T>(path: string, body: unknown): Promise<T> {
  return request<T>(path, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export interface ChatStreamPayload {
	agent: string
	sessionId: string
	message: string
	image?: string
	// 结构化附件（与后端 reqAttachment 对齐）：图片与文件分别上传。
	images?: Array<{ name: string; data: string; kind: string; size: number; mime: string }>
	files?: Array<{ name: string; data: string; kind: string; size: number; mime: string }>
	ragTopK?: number
	ragSourceFiles?: string[]
	ragSourceFilter?: string
	ragMaxPerSource?: number
	ragMinScore?: number
	strictContextOnly?: boolean
	answerMode?: string
	// 全局所选模型（聊天栏处切换）：覆盖智能体内置默认模型。
	model?: string
	provider?: string
	// 多智能体编排
	topology?: string
	agents?: string[]
}

/**
 * 调用 /api/chat/stream，逐条解析 SSE 事件并回调 onEvent。
 * SSE 格式（后端 chat_handlers.go）：`event: <type>\ndata: <json>\n\n`
 */
export async function streamChat(
  payload: ChatStreamPayload,
  onEvent: (ev: StreamEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const resp = await fetch(BASE + '/api/chat/stream', {
    method: 'POST',
    headers: defaultHeaders(),
    body: JSON.stringify(payload),
    signal,
  })
  if (!resp.ok || !resp.body) {
    throw new Error(`流式请求失败 (${resp.status})`)
  }
  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    if (buffer.length > MAX_SSE_BUFFER) {
      throw new Error('SSE 流数据过长，已中止解析')
    }
    let sep: number
    // 以空行（\n\n）切分每个 SSE 事件块
    while ((sep = buffer.indexOf('\n\n')) !== -1) {
      const raw = buffer.slice(0, sep)
      buffer = buffer.slice(sep + 2)
      let data = ''
      for (const line of raw.split('\n')) {
        if (line.startsWith('data:')) {
          data += line.slice(5).trim()
        }
      }
      if (!data) continue
      try {
        onEvent(JSON.parse(data) as StreamEvent)
      } catch {
        /* 忽略无法解析的片段 */
      }
    }
  }
}

export const api = {
  // ---- 智能体 ----
  getAgents: () => request<AgentInfo[]>('/api/agents'),
  createAgent: (body: unknown) => postJSON<{ name: string; message: string }>('/api/agent/create', body),
  updateAgent: (body: unknown) => postJSON<{ message: string }>('/api/agent/update', body),
  deleteAgent: (body: { name: string }) => postJSON<{ message: string }>('/api/agent/delete', body),

  // ---- 会话 ----
  getSessionList: () => request<{ sessions: string[] }>('/api/session/list'),
  getSessionHistory: (sessionId: string) =>
    request<{ sessionId: string; messages: ChatMessagePayload[] }>(
      `/api/session/history?sessionId=${encodeURIComponent(sessionId)}`,
    ),
  createSession: (sessionId: string) =>
    postJSON<{ sessionId: string; message: string }>('/api/session/create', { sessionId }),
  deleteSession: (sessionId: string) => postJSON<{ message: string }>('/api/session/delete', { sessionId }),
  saveSession: (sessionId: string) => postJSON<{ message: string }>('/api/session/save', { sessionId }),

  // ---- RAG ----
  ragCount: () => request<{ count: number; chunkCount: number; sourceCount: number; initialized: boolean }>('/api/rag/count'),
  ragStatus: () => request<RagStatus>('/api/rag/status'),
  ragUpload: (id: string, content: string) =>
    postJSON<Record<string, unknown>>('/api/rag/upload', { id, content }),
  ragUploadFile: async (file: File) => {
    const form = new FormData()
    form.append('file', file)
    const resp = await fetch(BASE + '/api/rag/upload-file', {
      method: 'POST',
      body: form,
    })
    if (!resp.ok) {
      let detail = resp.statusText
      try {
        const b = await resp.json()
        if (b && b.error) detail = b.error
      } catch {
        /* ignore */
      }
      throw new Error(detail)
    }
    return (await resp.json()) as Record<string, unknown>
  },
  ragSearch: (body: unknown) =>
    postJSON<{ results: RagSearchResult[]; count: number }>('/api/rag/search', body),
  ragScan: (dirPath: string) => postJSON<Record<string, unknown>>('/api/rag/scan', { dirPath }),
  ragTestEmbedding: () => postJSON<Record<string, unknown>>('/api/rag/test-embedding', {}),

  // ---- 设置 / 元数据 ----
  getSettings: () => request<SettingsData>('/api/settings'),
  saveSettings: (body: unknown) => postJSON<{ message: string }>('/api/settings', body),
  getModels: () => request<ModelsData>('/api/models'),
  getProviders: () => request<ProvidersData>('/api/providers'),
  saveProviders: (body: { providers: unknown[] }) =>
    postJSON<{ message: string }>('/api/providers', body),
  discoverModels: (baseURL: string, apiKey: string) =>
    postJSON<{ models: string[]; baseURL: string }>('/api/providers/discover-models', { baseURL, apiKey }),
  getTools: () => request<{ tools: ToolInfo[] }>('/api/tools'),
  browse: (path: string) =>
    request<{ current: string; dirs: DirEntry[] }>(`/api/browse?path=${encodeURIComponent(path)}`),
  getMemory: () => request<Record<string, unknown>>('/api/runtime/memory'),
  gc: () => postJSON<Record<string, unknown>>('/api/runtime/gc', {}),

  // ---- 技能 ----
  getSkills: (agent: string) =>
    request<{ agent: string; skills: SkillInfo[] }>(`/api/skills?agent=${encodeURIComponent(agent)}`),
  addSkill: (body: { agent: string; name: string; content: string }) =>
    postJSON<{ message: string }>('/api/skill/add', body),
  deleteSkill: (body: { agent: string; name: string }) =>
    postJSON<{ message: string }>('/api/skill/delete', body),

  // ---- 权限审批 ----
  permissionsPending: () =>
    request<{ permissions: Array<Record<string, unknown>> }>('/api/permissions/pending'),
  permissionsResolve: (id: string, decision: string) =>
    postJSON<{ permission: Record<string, unknown> }>('/api/permissions/resolve', {
      id,
      decision,
    }),
}
