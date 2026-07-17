import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api, streamChat } from '../api/client'
import type {
  AgentStep,
  AttachedFile,
  AttachedImage,
  ChatMessage,
  ExecutionTraceItem,
  RagPanelState,
  SettingsData,
  SubTaskInfo,
  OrchestrationStep,
} from '../types/api'
import { uid } from '../utils/format'
import { useWorkspaceStore } from './workspace'

export type AnswerMode = 'ask' | 'craft' | 'plan' | 'balanced' | 'strict'

export interface ChatOptions {
  ragTopK: number
  answerMode: AnswerMode
  strictContextOnly: boolean
  sourceFilter: string
  ragMinScore: number
  ragMaxPerSource: number
}

export const useChatStore = defineStore('chat', () => {
  const messages = ref<ChatMessage[]>([])

  // ---- 执行过程时间线（实时步骤合并） ----
  // 按 kind+name 合并 running/done：已有进行中的同名步骤则就地更新为完成。
  function applyStep(steps: AgentStep[], s: AgentStep): AgentStep[] {
    const key = s.kind + ':' + s.name
    const idx = steps.findIndex(
      (x) => x.kind + ':' + x.name === key && x.status !== 'done',
    )
    if (idx >= 0) {
      const ex = steps[idx]
      const merged: AgentStep = {
        ...ex,
        status: s.status,
        input: s.input ?? ex.input,
        output: s.output ?? ex.output,
        title: s.title || ex.title,
      }
      const copy = steps.slice()
      copy[idx] = merged
      return copy
    }
    return [...steps, s]
  }

  // 把后端 traceItems 映射为时间线步骤（tool_call/tool_result、rag_search/rag_result 各自合并）。
  function traceItemToStep(it: ExecutionTraceItem): AgentStep | null {
    switch (it.type) {
      case 'tool_call':
        return { kind: 'tool', name: it.name || '', title: it.message || '', status: 'running', input: it.arguments }
      case 'tool_result':
        return { kind: 'tool', name: it.name || '', title: '', status: 'done', output: it.result }
      case 'rag_search':
        return { kind: 'rag', name: 'rag', title: it.message || '', status: 'running', input: it.result }
      case 'rag_result':
        return { kind: 'rag', name: 'rag', title: '', status: 'done', output: it.result }
    }
    return null
  }

  function traceItemsToSteps(items?: ExecutionTraceItem[]): AgentStep[] {
    if (!items) return []
    let steps: AgentStep[] = []
    for (const it of items) {
      const s = traceItemToStep(it)
      if (s) steps = applyStep(steps, s)
    }
    return steps
  }

  const streaming = ref(false)
  const rag = ref<RagPanelState>({
    query: '',
    references: [],
    toolCalls: [],
    traceItems: [],
  })
  const options = ref<ChatOptions>({
    ragTopK: 5,
    answerMode: 'craft',
    strictContextOnly: false,
    sourceFilter: '',
    ragMinScore: 0,
    ragMaxPerSource: 0,
  })
  const error = ref<string | null>(null)
  // 多智能体编排状态（拓扑与参与智能体由智能体数量自动判定，无需手动选择）
  const orchestrationPlan = ref<SubTaskInfo[]>([])
  const orchestrationSteps = ref<OrchestrationStep[]>([])
  let abort: AbortController | null = null

  function resetRag() {
    rag.value = { query: '', references: [], toolCalls: [], traceItems: [] }
  }

  function resetOrchestration() {
    orchestrationPlan.value = []
    orchestrationSteps.value = []
  }

  function applySettings(s: SettingsData | null) {
    if (!s) return
    const ragCfg = s.rag as Record<string, unknown>
    const topK = Number(ragCfg['chatTopK'] ?? 0)
    if (topK > 0) options.value.ragTopK = topK
    const minScore = Number(ragCfg['minScore'] ?? 0)
    if (minScore > 0) options.value.ragMinScore = minScore
  }

  function newSession(agent?: string) {
    const ws = useWorkspaceStore()
    abort?.abort()
    ws.createSessionMeta(undefined, agent ?? ws.activeAgent)
    messages.value = []
    resetRag()
    resetOrchestration()
    error.value = null
  }

  async function loadSession(id: string) {
    const ws = useWorkspaceStore()
    abort?.abort()
    ws.setActiveSession(id)
    messages.value = []
    resetRag()
    resetOrchestration()
    error.value = null
    try {
      const { messages: hist } = await api.getSessionHistory(id)
      messages.value = hist
        .filter((m) => m.role === 'user' || m.role === 'assistant')
        .map((m) => {
          const msg: ChatMessage = {
            id: uid('msg'),
            role: m.role as ChatMessage['role'],
            content: m.content,
            streaming: false,
          }
          // 还原历史附件（后端透出的图片/文件）为前端 AttachedFile 结构。
          if (m.role === 'user' && m.attachments && m.attachments.length) {
            const files: AttachedFile[] = m.attachments.map((a) => ({
              name: a.name,
              data: a.data,
              kind: (a.kind as AttachedFile['kind']) || 'image',
              size: a.size || 0,
              mime: a.mime || '',
            }))
            msg.files = files
            // 兼容旧渲染逻辑：把图片也填入 images。
            msg.images = files
              .filter((f) => f.kind === 'image')
              .map((f) => ({ name: f.name, data: f.data }))
          }
          return msg
        })
    } catch {
      messages.value = []
    }
  }

  function lastAssistant(): ChatMessage | undefined {
    for (let i = messages.value.length - 1; i >= 0; i--) {
      if (messages.value[i].role === 'assistant') return messages.value[i]
    }
    return undefined
  }

  async function send(text: string, attachments?: { image?: string; attachedImages?: AttachedImage[]; attachedFiles?: AttachedFile[]; apiMessage?: string }) {
    const ws = useWorkspaceStore()
    const content = (text || '').trim()
    const apiMessage = (attachments?.apiMessage || content || '[附件]')
    const hasFiles = attachments?.attachedFiles && attachments.attachedFiles.length > 0
    const hasImages = attachments?.attachedImages && attachments.attachedImages.length > 0
    const hasAnyAttach = hasFiles || hasImages
    if ((!content && !hasAnyAttach) || streaming.value) return
    if (!ws.activeAgent) {
      ws.showToast('error', '没有可用的智能体，请先在设置中添加。')
      return
    }

    const sessionTitle = content || '[附件]'

    let sessionId = ws.activeSessionId
    if (!sessionId) {
      sessionId = ws.createSessionMeta(ws.sessionTitleFor(sessionTitle), ws.activeAgent)
    } else if (!ws.sessions.find((s) => s.id === sessionId)) {
      sessionId = ws.createSessionMeta(ws.sessionTitleFor(sessionTitle), ws.activeAgent)
    } else {
      ws.touchSession(sessionId, {
        title: ws.sessions.find((s) => s.id === sessionId)?.title || ws.sessionTitleFor(sessionTitle),
      })
    }

    const userMsg: ChatMessage = {
      id: uid('msg'),
      role: 'user',
      content: content || '[附件]',
      streaming: false,
      files: attachments?.attachedFiles,
      images: attachments?.attachedImages,
    }
    const botMsg: ChatMessage = {
      id: uid('msg'),
      role: 'assistant',
      content: '',
      streaming: true,
      statusStage: 'start',
      statusMessage: '准备中…',
      steps: [],
    }
    messages.value.push(userMsg, botMsg)

    streaming.value = true
    error.value = null
    resetRag()

    abort = new AbortController()
    const opts = options.value

    try {
      // 把前端 AttachedFile 映射为后端结构化附件（images / files）。
      const toPayload = (f: AttachedFile) => ({
        name: f.name,
        data: f.data,
        kind: f.kind,
        size: f.size,
        mime: f.mime,
      })
      const imagesPayload = attachments?.attachedFiles
        ?.filter((f) => f.kind === 'image')
        .map(toPayload)
      const filesPayload = attachments?.attachedFiles
        ?.filter((f) => f.kind !== 'image')
        .map(toPayload)

      await streamChat(
        {
          agent: ws.activeAgent,
          sessionId,
          message: apiMessage,
          image: attachments?.image,
          images: imagesPayload,
          files: filesPayload,
          ragTopK: opts.ragTopK,
          answerMode: opts.answerMode,
          strictContextOnly: opts.strictContextOnly,
          ragSourceFilter: opts.sourceFilter || undefined,
          ragMinScore: opts.ragMinScore,
          ragMaxPerSource: opts.ragMaxPerSource,
          // 自动判定：仅 1 个智能体→单智能体直答；≥2 个→自动走 supervisor 编排（默认全部参与）。
          topology: ws.agents.length >= 2 ? 'supervisor' : undefined,
          agents: ws.agents.length >= 2 ? ws.agents.map((a) => a.name) : undefined,
        },
        (ev) => {
          const bot = lastAssistant()
          if (!bot) return
          switch (ev.type) {
            case 'status':
              if (ev.message) bot.statusMessage = ev.message
              if (ev.stage) bot.statusStage = ev.stage
              if (ev.ragQuery) rag.value.query = ev.ragQuery
              if (ev.ragReferences) rag.value.references = ev.ragReferences
              break
            case 'meta':
              rag.value.query = ev.ragQuery ?? rag.value.query
              rag.value.references = ev.ragReferences ?? rag.value.references
              rag.value.toolCalls = ev.toolCalls ?? rag.value.toolCalls
              rag.value.traceItems = ev.traceItems ?? rag.value.traceItems
              break
            case 'plan': {
              bot.plan = ev.plan
              bot.plannedSteps = ev.plannedSteps
              bot.planStatus = ev.planStatus
              break
            }
            case 'orchestration': {
              if (ev.phase === 'dispatch' && ev.subTasks) {
                orchestrationPlan.value = ev.subTasks
                bot.orchestrationPlan = ev.subTasks
              } else if (ev.phase === 'agent' || ev.phase === 'synthesize') {
                const step: OrchestrationStep = {
                  phase: ev.phase as OrchestrationStep['phase'],
                  agent: ev.agent,
                  subTask: ev.subTask,
                  message: ev.message,
                  step: ev.step,
                  status:
                    ev.phase === 'synthesize'
                      ? undefined
                      : ev.message?.includes('完成')
                        ? 'end'
                        : 'start',
                }
                orchestrationSteps.value.push(step)
                bot.orchestration = [...orchestrationSteps.value]
              }
              break
            }
            case 'delta':
              if (ev.delta) bot.content += ev.delta
              break
            case 'step':
              if (ev.agentStep) {
                bot.steps = applyStep(bot.steps ?? [], ev.agentStep)
              }
              break
            case 'done':
              bot.content = ev.reply ?? bot.content
              bot.streaming = false
              rag.value.query = ev.ragQuery ?? rag.value.query
              rag.value.references = ev.ragReferences ?? rag.value.references
              rag.value.toolCalls = ev.toolCalls ?? rag.value.toolCalls
              rag.value.traceItems = ev.traceItems ?? rag.value.traceItems
              bot.ragQuery = rag.value.query
              bot.ragReferences = rag.value.references
              bot.toolCalls = rag.value.toolCalls
              bot.traceItems = rag.value.traceItems
              // 由最终 traceItems 重建步骤时间线，保证与历史一致（覆盖流式增量）。
              bot.steps = traceItemsToSteps(rag.value.traceItems)
              bot.answerMode = ev.answerMode
              bot.orchestrationPlan = orchestrationPlan.value
              bot.orchestration = [...orchestrationSteps.value]
              bot.statusMessage = undefined
              ws.touchSession(sessionId, {
                preview: ws.sessionPreviewFor(bot.content),
              })
              break
            case 'error':
              bot.streaming = false
              bot.error = true
              bot.statusMessage = undefined
              bot.content =
                bot.content ||
                '请求出错：' + (ev.error || '未知错误')
              ws.showToast('error', '对话出错：' + (ev.error || '未知错误'))
              break
          }
        },
        abort.signal,
      )
    } catch (e) {
      if ((e as Error).name === 'AbortError') {
        // 用户主动停止（stop() 已把 bot 标记为已停止），不视为失败
        return
      }
      const bot = lastAssistant()
      if (bot && bot.streaming) {
        bot.streaming = false
        bot.error = true
        if (!bot.content) bot.content = '请求失败：' + (e as Error).message
        bot.statusMessage = undefined
      }
      ws.showToast('error', '对话失败：' + (e as Error).message)
    } finally {
      streaming.value = false
      abort = null
      ws.refreshSessions()
    }
  }

  function stop() {
    if (abort) abort.abort()
    const bot = lastAssistant()
    if (bot && bot.streaming) {
      bot.streaming = false
      bot.statusMessage = '已停止'
    }
    streaming.value = false
  }

  // executePlan 在用户确认 Plan 后执行：重新用 Craft 模式发送同一问题，不追加新的用户消息。
  async function executePlan() {
    // 找到最后一条有计划的助手消息
    let planMsg: ChatMessage | undefined
    let userMsg: ChatMessage | undefined
    for (let i = messages.value.length - 1; i >= 0; i--) {
      const m = messages.value[i]
      if (m.role === 'assistant' && m.planStatus === 'done' && !planMsg) {
        planMsg = m
      } else if (m.role === 'user' && planMsg && !userMsg) {
        userMsg = m
        break
      }
    }
    if (!planMsg || !userMsg) {
      ws.showToast('error', '未找到待执行的计划')
      return
    }

    planMsg.planStatus = 'executing'
    planMsg.streaming = false

    const content = userMsg.content || '[执行计划]'
    const botMsg: ChatMessage = {
      id: uid('msg'),
      role: 'assistant',
      content: '',
      streaming: true,
      statusStage: 'start',
      statusMessage: '执行中…',
      steps: [],
    }
    messages.value.push(botMsg)

    streaming.value = true
    error.value = null
    abort = new AbortController()
    const opts = options.value

    try {
      await streamChat(
        {
          agent: ws.activeAgent,
          sessionId: ws.activeSessionId,
          message: content,
          ragTopK: opts.ragTopK,
          answerMode: 'craft',
          strictContextOnly: opts.strictContextOnly,
          ragSourceFilter: opts.sourceFilter || undefined,
          ragMinScore: opts.ragMinScore,
          ragMaxPerSource: opts.ragMaxPerSource,
        },
        (ev) => {
          const bot = lastAssistant()
          if (!bot) return
          switch (ev.type) {
            case 'status':
              if (ev.message) bot.statusMessage = ev.message
              if (ev.stage) bot.statusStage = ev.stage
              if (ev.ragQuery) rag.value.query = ev.ragQuery
              if (ev.ragReferences) rag.value.references = ev.ragReferences
              break
            case 'meta':
              rag.value.query = ev.ragQuery ?? rag.value.query
              rag.value.references = ev.ragReferences ?? rag.value.references
              rag.value.toolCalls = ev.toolCalls ?? rag.value.toolCalls
              rag.value.traceItems = ev.traceItems ?? rag.value.traceItems
              break
            case 'delta':
              if (ev.delta) bot.content += ev.delta
              break
            case 'step':
              if (ev.agentStep) bot.steps = applyStep(bot.steps ?? [], ev.agentStep)
              break
            case 'done':
              bot.content = ev.reply ?? bot.content
              bot.streaming = false
              rag.value.query = ev.ragQuery ?? rag.value.query
              rag.value.references = ev.ragReferences ?? rag.value.references
              rag.value.toolCalls = ev.toolCalls ?? rag.value.toolCalls
              rag.value.traceItems = ev.traceItems ?? rag.value.traceItems
              bot.ragQuery = rag.value.query
              bot.ragReferences = rag.value.references
              bot.toolCalls = rag.value.toolCalls
              bot.traceItems = rag.value.traceItems
              bot.steps = traceItemsToSteps(rag.value.traceItems)
              bot.answerMode = ev.answerMode
              bot.orchestrationPlan = orchestrationPlan.value
              bot.orchestration = [...orchestrationSteps.value]
              bot.statusMessage = undefined
              ws.touchSession(ws.activeSessionId, { preview: ws.sessionPreviewFor(bot.content) })
              break
            case 'error':
              bot.streaming = false
              bot.error = true
              bot.statusMessage = undefined
              bot.content = bot.content || '执行出错：' + (ev.error || '未知错误')
              ws.showToast('error', '执行出错：' + (ev.error || '未知错误'))
              break
          }
        },
        abort.signal,
      )
    } catch (e) {
      if ((e as Error).name === 'AbortError') return
      const bot = lastAssistant()
      if (bot && bot.streaming) {
        bot.streaming = false
        bot.error = true
        if (!bot.content) bot.content = '执行失败：' + (e as Error).message
        bot.statusMessage = undefined
      }
      ws.showToast('error', '执行失败：' + (e as Error).message)
    } finally {
      streaming.value = false
      abort = null
      ws.refreshSessions()
    }
  }

  function clampNum(v: unknown, min: number, max: number, fallback: number): number {
    const n = Number(v)
    if (!Number.isFinite(n)) return fallback
    return Math.max(min, Math.min(max, Math.round(n)))
  }

  function setOption<K extends keyof ChatOptions>(key: K, value: ChatOptions[K]) {
    switch (key) {
      case 'ragTopK':
        options.value.ragTopK = clampNum(value, 1, 20, 5)
        break
      case 'ragMinScore':
        options.value.ragMinScore = clampNum(value, 0, 1, 0)
        break
      case 'ragMaxPerSource':
        options.value.ragMaxPerSource = clampNum(value, 0, 50, 0)
        break
      default:
        options.value[key] = value
    }
  }

  return {
    messages,
    streaming,
    rag,
    options,
    error,
    orchestrationPlan,
    orchestrationSteps,
    applySettings,
    newSession,
    loadSession,
    send,
    stop,
    executePlan,
    setOption,
    resetOrchestration,
  }
})
