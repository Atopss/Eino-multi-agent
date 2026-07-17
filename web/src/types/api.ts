// 与后端（eino/server）API 契约对应的前端类型定义。
// 形状严格对齐 eino/agent/types.go 与 server.go 的 JSON 编解码标签。

export interface AgentInfo {
  name: string
  model: string
  systemPrompt: string
  needTools: boolean
}

export interface SessionMeta {
  id: string
  title: string
  preview: string
  updatedAt: number
  // 该会话归属的智能体（用于侧边栏按智能体分组）。旧数据可能不存在，需回填。
  agent?: string
}

export interface ChatMessagePayload {
  role: string
  content: string
  // 后端历史接口透出的附件（图片 base64 dataURL / 文件元信息）。
  attachments?: Array<{
    name: string
    data: string
    kind: 'image' | 'text' | 'binary'
    size: number
    mime: string
  }>
}

export interface RAGReference {
  id: string
  fileName: string
  source: string
  chunkIndex: number
  score: number
  chunk: string
  matchType: string
}

export interface ToolCallTrace {
  name: string
  arguments: string
  result: string
}

export interface ExecutionTraceItem {
  type: string
  stage?: string
  agent?: string
  target?: string
  name?: string
  arguments?: string
  result?: string
  message?: string
}

// 执行过程时间线的一个步骤节点（对齐 eino/agent/types.go 的 AgentStep）。
// 同一动作会发两条：running（开始）→ done（完成），前端按 kind+name 合并。
export interface AgentStep {
  kind: 'rag' | 'tool' | 'model' | 'agent'
  name: string
  title: string
  status: 'running' | 'done' | 'error'
  input?: string
  output?: string
}

// 多智能体编排相关类型（对齐 eino/agent/types.go 的 JSON 结构）。

// 编排拓扑：单智能体 / Router（规划-并行-汇聚）/ Supervisor（主管循环）。
export type Topology = 'single' | 'supervisor'

// 规划阶段产出的子任务（前端时间线展示）。
export interface SubTaskInfo {
	agent: string
	task: string
}

// 编排实时时间线的一个节点。
export interface OrchestrationStep {
	phase: 'plan' | 'dispatch' | 'agent' | 'synthesize' | 'done'
	agent?: string
	subTask?: string
	message?: string
	step?: number
	status?: 'start' | 'end'
}

export interface StreamEvent {
	type: string
	stage?: string
	message?: string
	delta?: string
	reply?: string
	ragQuery?: string
	ragReferences?: RAGReference[]
	toolCalls?: ToolCallTrace[]
	traceItems?: ExecutionTraceItem[]
	answerMode?: string
	error?: string

	// 执行过程时间线步骤（实时流出）
	agentStep?: AgentStep

	// 以下字段用于多智能体编排的实时时间线。
	topology?: string
	phase?: string
	subTask?: string
	agent?: string
	step?: number
	subTasks?: SubTaskInfo[]

	// Plan 模式
	plan?: string
	plannedSteps?: string[]
	planStatus?: string // "generating" | "done" | "executing"
}

export interface RunResult {
  reply: string
  ragQuery?: string
  ragReferences?: RAGReference[]
  toolCalls?: ToolCallTrace[]
  traceItems?: ExecutionTraceItem[]
  answerMode?: string
}

export interface RagSearchResult {
  id: string
  fileName: string
  source: string
  chunkIndex: number
  chunk: string
  score: number
  matchType: string
  neighborOffset?: number
  parentId?: string
  metadata?: Record<string, string>
}

export interface RagStatus {
  initialized: boolean
  count: number
  chunkCount: number
  sourceCount: number
  dataDir?: string
  embeddingEP?: string
  embeddingModel?: string
  embeddingStatus?: string
  embeddingWarning?: string
  sourceFiles?: string[]
  sourceFileDetails?: Array<Record<string, unknown>>
  failedFiles?: Array<Record<string, unknown>>
  [key: string]: unknown
}

export interface SettingsData {
  provider: Record<string, string>
  embedding: Record<string, string>
  rag: Record<string, unknown>
  computer: Record<string, unknown>
  runtime: Record<string, number>
  embeddingStatus: string
  storage: string
  effective: Record<string, unknown>
  [key: string]: unknown
}

export interface ModelOption {
  value: string
  label: string
  kind: string
  provider: string
  note: string
}

export interface ModelsData {
  chatModels: string[]
  embeddingModels: string[]
  chatOptions: ModelOption[]
  embeddingOptions: ModelOption[]
}

export interface ToolInfo {
  name: string
  description: string
}

export interface DirEntry {
  name: string
  path: string
  isDir: boolean
}

export interface SkillInfo {
  name: string
  description: string
}

export type FileKind = 'image' | 'text' | 'binary'

export interface AttachedFile {
  name: string
  data: string // base64 data URL (image) 或纯文本 (text) 或空 (binary)
  kind: FileKind
  size: number
  mime: string
}

export interface AttachedImage {
  name: string
  data: string // base64 data URL (向后兼容，等同于 kind=image 的 AttachedFile)
}

// 前端本地维护的"一条对话消息"
export interface ChatMessage {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  streaming: boolean
  statusStage?: string
  statusMessage?: string
  error?: boolean
	answerMode?: string
	ragQuery?: string
	ragReferences?: RAGReference[]
	toolCalls?: ToolCallTrace[]
	traceItems?: ExecutionTraceItem[]
	// 执行过程时间线步骤（实时渲染，完成后折叠为摘要）
	steps?: AgentStep[]
	// 多智能体编排：该助手消息承载的编排过程。
	topology?: Topology
	orchestrationPlan?: SubTaskInfo[]
	orchestration?: OrchestrationStep[]
	// 用户上传的附件（图片+文件，v2）
	files?: AttachedFile[]
	// @deprecated 旧字段，保留向后兼容
	images?: AttachedImage[]
	// Plan 模式
	plan?: string
	plannedSteps?: string[]
	planStatus?: string
}

export interface RagPanelState {
  query: string
  references: RAGReference[]
  toolCalls: ToolCallTrace[]
  traceItems: ExecutionTraceItem[]
}
