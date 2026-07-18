<script setup lang="ts">
import { ref, computed } from 'vue'
import { Send, Square, Sparkles, Paperclip, X, Image, FileText, File, ClipboardList, ChevronDown, Cpu, Search, SlidersHorizontal } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import { useWorkspaceStore } from '../stores/workspace'
import type { AttachedFile, FileKind } from '../types/api'
import type { AnswerMode } from '../stores/chat'

const MAX_FILE_SIZE = 10 * 1024 * 1024 // 10MB
const IMAGE_MIMES = new Set(['image/jpeg', 'image/png', 'image/gif', 'image/webp', 'image/bmp', 'image/svg+xml'])
const TEXT_MIMES = new Set([
  'text/plain', 'text/markdown', 'text/html', 'text/css', 'text/csv', 'text/xml',
  'text/javascript', 'text/x-python', 'text/x-go', 'text/x-java', 'text/x-c',
  'text/x-c++', 'text/x-rust', 'text/x-ruby', 'text/x-php', 'text/x-sh',
  'text/x-sql', 'text/yaml',
])
const TEXT_EXTENSIONS = new Set([
  '.txt', '.md', '.json', '.csv', '.tsv', '.xml', '.yaml', '.yml', '.toml',
  '.ini', '.cfg', '.conf', '.log', '.env', '.py', '.js', '.ts', '.jsx', '.tsx',
  '.go', '.java', '.c', '.cpp', '.h', '.hpp', '.rs', '.rb', '.php', '.sh',
  '.bat', '.ps1', '.sql', '.html', '.css', '.scss', '.less', '.vue', '.svelte',
  '.gitignore', '.dockerignore', '.editorconfig', '.proto', '.graphql',
])

const chat = useChatStore()
const ws = useWorkspaceStore()
const text = ref('')
const files = ref<AttachedFile[]>([])
const fileInput = ref<HTMLInputElement | null>(null)
const isDragOver = ref(false)

const canSend = computed(() => text.value.trim().length > 0 || files.value.length > 0)

// 模式选择下拉
const modeDropdownOpen = ref(false)
const modes: { value: AnswerMode; label: string; icon: any; desc: string; recommend: boolean; items: string[] }[] = [
  {
    value: 'ask', label: 'Ask', icon: Sparkles,
    desc: '纯对话模式，不执行任何实际操作',
    recommend: false,
    items: ['概念问答与知识讲解', '代码解释与思路讨论', '不检索项目文件，不调用工具，不修改文件'],
  },
  {
    value: 'craft', label: 'Craft', icon: Send,
    desc: '完整执行模式，一站式完成任务',
    recommend: true,
    items: ['自动检索项目文件并获取上下文', '按需调用工具链完成操作', '支持代码编写、文档生成、项目构建'],
  },
  {
    value: 'plan', label: 'Plan', icon: ClipboardList,
    desc: '计划先行，确认后再执行',
    recommend: false,
    items: ['分析需求并生成分步执行计划', '暂停等待用户审阅确认', '确认后自动按计划逐步执行'],
  },
]
const currentMode = computed(() => modes.find(m => m.value === chat.options.answerMode) || modes[1])
function selectMode(mode: AnswerMode) {
  chat.options.answerMode = mode
  modeDropdownOpen.value = false
}
function toggleDropdown() {
  modeDropdownOpen.value = !modeDropdownOpen.value
}
function closeAll() {
  modeDropdownOpen.value = false
  modelDropdownOpen.value = false
  modelManageOpen.value = false
}

// 全局模型选择器（聊天栏处，与智能体解耦；localStorage 持久化）
const modelDropdownOpen = ref(false)
const modelGroups = computed(() => ws.chatModelGroups)
const currentModelLabel = computed(() => ws.activeModelOption?.label || ws.activeModel || '选择模型')
const currentModelNote = computed(() => ws.activeModelOption?.note || '')
function selectModel(value: string) {
  ws.setActiveModel(value)
  modelDropdownOpen.value = false
}
function toggleModelDropdown() {
  modelDropdownOpen.value = !modelDropdownOpen.value
}

// 模型管理面板
const modelManageOpen = ref(false)
const modelSearch = ref('')

/** 把 openrouter/deepseek-v4-flash 这种长名截成短名 */
function shortLabel(label: string): string {
  if (!label) return ''
  const lastSlash = label.lastIndexOf('/')
  if (lastSlash > 0) return label.slice(lastSlash + 1)
  return label
}
const ALL_MODEL_GROUPS = computed(() => ws.allModelGroups)
const filteredModelGroups = computed(() => {
  const q = modelSearch.value.trim().toLowerCase()
  if (!q) return ALL_MODEL_GROUPS.value
  return ALL_MODEL_GROUPS.value
    .map((g) => ({
      ...g,
      options: g.options.filter((o) => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q)),
    }))
    .filter((g) => g.options.length > 0)
})
function openModelManage() {
  modelDropdownOpen.value = false
  modelSearch.value = ''
  modelManageOpen.value = true
}

// 判断文件类型
function classifyFile(file: File): FileKind {
  if (IMAGE_MIMES.has(file.type)) return 'image'
  if (TEXT_MIMES.has(file.type)) return 'text'
  const ext = '.' + (file.name.split('.').pop()?.toLowerCase() || '')
  if (TEXT_EXTENSIONS.has(ext)) return 'text'
  return 'binary'
}

// 图片 → base64 data URL
function fileToImage(file: File): Promise<AttachedFile> {
  return new Promise((resolve, reject) => {
    if (file.size > MAX_FILE_SIZE) {
      reject(new Error(`"${file.name}" 超过 10MB 限制`))
      return
    }
    const reader = new FileReader()
    reader.onload = () => resolve({
      name: file.name, data: reader.result as string,
      kind: 'image', size: file.size, mime: file.type,
    })
    reader.onerror = () => reject(new Error(`读取图片 "${file.name}" 失败`))
    reader.readAsDataURL(file)
  })
}

// 文本文件 → 读取文本内容
function fileToText(file: File): Promise<AttachedFile> {
  return new Promise((resolve, reject) => {
    if (file.size > MAX_FILE_SIZE) {
      reject(new Error(`"${file.name}" 超过 10MB 限制`))
      return
    }
    const reader = new FileReader()
    reader.onload = () => resolve({
      name: file.name, data: reader.result as string,
      kind: 'text', size: file.size, mime: file.type,
    })
    reader.onerror = () => reject(new Error(`读取文件 "${file.name}" 失败`))
    reader.readAsText(file)
  })
}

// 二进制文件 → 只记元信息
function fileToBinary(file: File): AttachedFile {
  if (file.size > MAX_FILE_SIZE) {
    throw new Error(`"${file.name}" 超过 10MB 限制`)
  }
  return { name: file.name, data: '', kind: 'binary', size: file.size, mime: file.type }
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

function addFiles(fileList: FileList | File[]) {
  const arr = Array.from(fileList)
  if (arr.length === 0) return

  const promises: Promise<AttachedFile>[] = []

  for (const f of arr) {
    const kind = classifyFile(f)
    try {
      if (kind === 'image') {
        promises.push(fileToImage(f))
      } else if (kind === 'text') {
        promises.push(fileToText(f))
      } else {
        promises.push(Promise.resolve(fileToBinary(f)))
      }
    } catch (err) {
      ws.showToast('error', (err as Error).message)
    }
  }

  Promise.all(promises)
    .then(results => {
      files.value.push(...results)
    })
    .catch(err => {
      ws.showToast('error', (err as Error).message)
    })
}

function removeFile(index: number) {
  files.value.splice(index, 1)
}

function onFileChange(e: Event) {
  const input = e.target as HTMLInputElement
  if (input.files?.length) addFiles(input.files)
  input.value = ''
}

function onPaste(e: ClipboardEvent) {
  const items = e.clipboardData?.items
  if (!items) return
  const found: File[] = []
  for (let i = 0; i < items.length; i++) {
    const f = items[i].getAsFile()
    if (f) found.push(f)
  }
  if (found.length) {
    e.preventDefault()
    addFiles(found)
  }
}

function onDragOver(e: DragEvent) {
  e.preventDefault()
  isDragOver.value = true
}

function onDragLeave() {
  isDragOver.value = false
}

function onDrop(e: DragEvent) {
  e.preventDefault()
  isDragOver.value = false
  if (e.dataTransfer?.files.length) addFiles(e.dataTransfer.files)
}

function submit() {
  const value = text.value.trim()
  const hasFiles = files.value.length > 0
  if ((!value && !hasFiles) || chat.streaming) return

  // 构建 image 字段：多张图片用 ---IMAGE--- 分隔（兼容后端）
  const imageParts = files.value.filter(f => f.kind === 'image').map(f => f.data)
  const imagePayload = imageParts.length > 0 ? imageParts.join('\n---IMAGE---\n') : undefined

  // 构建旧 attachedImages（向后兼容）
  const attachedImages = imageParts.length > 0
    ? files.value.filter(f => f.kind === 'image').map(f => ({ name: f.name, data: f.data }))
    : undefined

  const textParts = files.value.filter(f => f.kind === 'text')
  const binParts = files.value.filter(f => f.kind === 'binary')
  const allFileNames = files.value.map(f => f.name)

  // 前端展示内容：只显示用户文字 + 文件列表（不含文件内容）
  let displayContent = value || '[附件]'
  if (allFileNames.length > 0) {
    displayContent += `\n\n（附件：${allFileNames.join('、')}）`
  }

  // 后端发送内容：用户文字 + 完整文件内容
  let apiMessage = value || '[附件]'
  if (textParts.length > 0) {
    apiMessage += '\n\n--- 上传文件内容 ---\n'
    for (const tf of textParts) {
      apiMessage += `\n### 文件: ${tf.name} (${formatSize(tf.size)})\n\`\`\`\n${tf.data}\n\`\`\`\n`
    }
  }
  if (binParts.length > 0) {
    apiMessage += `\n\n--- 上传文件（无法直接解析内容） ---\n${binParts.map(f => `- ${f.name} (${f.mime || 'binary'}, ${formatSize(f.size)})`).join('\n')}\n`
  }

  chat.send(displayContent, {
    image: imagePayload,
    attachedImages,
    attachedFiles: [...files.value],
    apiMessage,
  })
  text.value = ''
  files.value = []
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    submit()
  }
}

// 转义 HTML 防止 XSS
function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
}
</script>

<template>
  <div class="border-t border-ink-800 bg-ink-950/60 px-4 py-4">
    <!-- 拖拽提示 -->
    <div
      v-if="isDragOver"
      class="mx-auto mb-2 flex max-w-4xl items-center justify-center gap-2 rounded-card border-2 border-dashed border-brand/50 bg-brand/10 py-3 text-sm text-brand-400"
    >
      <Paperclip :size="18" />
      松开以添加文件（图片/文档/代码…）
    </div>

    <div
      class="mx-auto max-w-4xl rounded-card border border-ink-700 bg-ink-900 px-4 py-3 transition-colors"
      :class="isDragOver ? 'border-brand/50 bg-brand/5' : 'focus-within:border-brand/60'"
      @dragover="onDragOver"
      @dragleave="onDragLeave"
      @drop="onDrop"
    >
      <!-- 附件预览 -->
      <div v-if="files.length > 0" class="mb-2 flex flex-wrap gap-2">
        <div
          v-for="(f, idx) in files"
          :key="idx"
          class="group relative shrink-0 overflow-hidden rounded-control border border-ink-700"
        >
          <!-- 图片预览 -->
          <template v-if="f.kind === 'image'">
            <div class="h-16 w-16">
              <img :src="f.data" :alt="f.name" class="h-full w-full object-cover" />
            </div>
          </template>

          <!-- 文本文件预览 -->
          <template v-else-if="f.kind === 'text'">
            <div class="flex h-16 items-center gap-1.5 px-3 min-w-[100px] max-w-[160px]">
              <FileText :size="20" class="shrink-0 text-slate-400" />
              <div class="min-w-0 text-xs">
                <div class="truncate font-medium text-slate-300" :title="f.name">{{ f.name }}</div>
                <div class="text-2xs text-slate-500">{{ formatSize(f.size) }}</div>
              </div>
            </div>
          </template>

          <!-- 其他二进制文件预览 -->
          <template v-else>
            <div class="flex h-16 items-center gap-1.5 px-3 min-w-[100px] max-w-[160px]">
              <File :size="20" class="shrink-0 text-slate-500" />
              <div class="min-w-0 text-xs">
                <div class="truncate font-medium text-slate-300" :title="f.name">{{ f.name }}</div>
                <div class="text-2xs text-slate-500">{{ formatSize(f.size) }}</div>
              </div>
            </div>
          </template>

          <!-- 删除按钮 -->
          <button
            class="absolute right-0.5 top-0.5 flex h-5 w-5 items-center justify-center rounded-full bg-ink-900/90 text-slate-400 opacity-0 transition-opacity hover:text-danger group-hover:opacity-100"
            @click="removeFile(idx)"
          >
            <X :size="12" />
          </button>
        </div>
      </div>

      <div class="flex items-end gap-2">
        <!-- 附件按钮 -->
        <button
          v-if="!chat.streaming"
          class="flex h-9 w-9 shrink-0 items-center justify-center rounded-control text-slate-500 transition-all duration-200 hover:bg-ink-800 hover:text-slate-300 active:scale-90"
          aria-label="添加文件"
          title="添加文件（支持图片/文档/代码… 也支持粘贴和拖拽）"
          @click="fileInput?.click()"
        >
          <Paperclip :size="17" />
        </button>
        <input
          ref="fileInput"
          type="file"
          multiple
          class="hidden"
          @change="onFileChange"
        />

        <textarea
          v-model="text"
          rows="1"
          class="max-h-44 min-h-[52px] flex-1 resize-none bg-transparent py-2 text-md leading-relaxed text-slate-100 placeholder:text-slate-500 outline-none"
          placeholder="输入问题（Enter 发送 / Shift+Enter 换行）。支持粘贴/拖拽文件"
          @keydown="onKeydown"
          @paste="onPaste"
        ></textarea>

        <button
          v-if="!chat.streaming"
          class="btn-primary h-9 w-9 shrink-0 !px-0"
          :disabled="!canSend"
          aria-label="发送"
          @click="submit"
        >
          <Send :size="17" />
        </button>
        <button
          v-else
          class="btn h-9 w-9 shrink-0 !px-0 border border-danger/50 bg-danger/15 text-danger hover:bg-danger/25 active:scale-90 active:bg-danger/30"
          aria-label="停止"
          @click="chat.stop()"
        >
          <Square :size="16" />
        </button>
      </div>
    </div>
    <p class="mx-auto mt-2 flex max-w-4xl items-center justify-between text-2xs text-slate-500">
      <!-- 模式选择：简洁显示 + 列表式弹窗（每行一个模式，选中打勾，后面有 i 说明） -->
      <div class="relative z-50 inline-flex items-center">
        <!-- 当前模式按钮 -->
        <button
          class="flex items-center gap-1 rounded-md px-2 py-1 text-slate-300 transition-colors hover:bg-ink-800 hover:text-slate-100"
          title="切换交互模式"
          @click="toggleDropdown"
        >
          <component :is="currentMode.icon" :size="12" />
          <span class="text-2xs font-medium">{{ currentMode.label }}</span>
          <ChevronDown :size="10" class="transition-transform" :class="modeDropdownOpen ? 'rotate-180' : ''" />
        </button>

        <!-- 模式选择弹窗 + 遮罩（带入场动画） -->
        <Transition name="drop">
          <div v-if="modeDropdownOpen">
            <div class="fixed inset-0 z-40" @click="closeAll()"></div>
            <div
              class="absolute bottom-full left-0 mb-1 z-50 flex w-64 rounded-card border border-ink-700 bg-ink-900 py-1.5 shadow-2xl shadow-black/40"
            >
              <!-- 左侧模式列表 -->
              <div class="flex flex-1 flex-col">
                <button
                  v-for="m in modes"
                  :key="m.value"
                  class="group flex items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-ink-800"
                  :class="m.value === chat.options.answerMode ? 'text-accent' : 'text-slate-300'"
                  @click="selectMode(m.value)"
                >
                  <component :is="m.icon" :size="14" class="shrink-0" />
                  <span class="font-medium">{{ m.label }}</span>
                  <span
                    v-if="m.recommend"
                    class="ml-0.5 rounded bg-brand/10 px-1.5 py-0.5 text-[10px] font-medium text-brand-400"
                  >推荐</span>
                  <svg
                    v-if="m.value === chat.options.answerMode"
                    class="ml-auto text-accent"
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="3"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  >
                    <polyline points="20 6 9 17 4 12" />
                  </svg>
                </button>
              </div>

              <!-- 右侧详情面板（始终显示当前选中项，或最后悬停项） -->
              <div class="w-52 border-l border-ink-700/60 px-3 py-2">
                <template v-for="m in modes" :key="m.value">
                  <div v-if="m.value === chat.options.answerMode" class="h-full">
                    <h4 class="mb-1 text-sm font-semibold text-slate-100">{{ m.label }}</h4>
                    <p class="mb-2.5 text-2xs leading-relaxed text-slate-400">{{ m.desc }}</p>
                    <ul class="space-y-1.5">
                      <li
                        v-for="(item, idx) in m.items"
                        :key="idx"
                        class="flex items-start gap-1.5 text-2xs text-slate-500"
                      >
                        <span class="mt-0.5 h-1.5 w-1.5 shrink-0 rounded-full bg-slate-600" />
                        <span>{{ item }}</span>
                      </li>
                    </ul>
                  </div>
                </template>
              </div>
            </div>
          </div>
        </Transition>
      </div>

      <!-- 全局模型选择器（聊天栏处，与智能体解耦；localStorage 持久化） -->
      <div class="relative z-50 inline-flex items-center">
        <button
          class="flex max-w-[180px] items-center gap-1 rounded-md px-2 py-1 text-slate-300 transition-colors hover:bg-ink-800 hover:text-slate-100"
          title="切换全局模型（设置里先配置好模型服务）"
          @click="toggleModelDropdown"
        >
          <component :is="Cpu" :size="12" class="shrink-0 text-brand-400" />
          <span class="truncate text-2xs font-medium">{{ currentModelLabel }}</span>
          <ChevronDown :size="10" class="shrink-0 transition-transform" :class="modelDropdownOpen ? 'rotate-180' : ''" />
        </button>

        <Transition name="drop">
          <div v-if="modelDropdownOpen">
            <div class="fixed inset-0 z-40" @click="closeAll()"></div>
            <div
              class="absolute bottom-full left-0 mb-1 z-50 flex w-60 flex-col rounded-card border border-ink-700 bg-ink-900 shadow-2xl shadow-black/40"
            >
              <!-- 模型列表（可滚动） -->
              <div class="max-h-64 overflow-y-auto">
                <template v-for="grp in modelGroups" :key="grp.provider">
                  <p class="px-3 pb-1 pt-1.5 text-[10px] font-medium uppercase tracking-wide text-slate-500">{{ grp.provider }}</p>
                  <button
                    v-for="o in grp.options"
                    :key="o.value"
                    class="flex items-center gap-2 px-3 py-1.5 text-left text-sm transition-colors hover:bg-ink-800"
                  :class="o.value === ws.activeModel ? 'text-accent' : (o.configured || o.free ? 'text-slate-100' : 'text-slate-500')"
                  :title="o.free ? '免费模型，无需配置，可直接使用' : o.configured ? '已配置，可直接使用' : '该商家未配置 API Key，需先到「设置 → 模型服务」配置后再用'"
                    @click="selectModel(o.value)"
                  >
                  <span class="truncate">{{ shortLabel(o.label) }}</span>
                  <span
                    class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium"
                    :class="o.free ? 'bg-emerald-500/10 text-emerald-400' : o.configured ? 'bg-brand/20 text-brand-400' : 'bg-ink-800 text-slate-500'"
                  >{{ o.free ? '免费' : o.configured ? '已配置' : '未配置' }}</span>
                    <svg
                      v-if="o.value === ws.activeModel"
                      class="ml-auto shrink-0 text-accent"
                      width="14"
                      height="14"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="3"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                    >
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  </button>
                </template>
                <p v-if="!modelGroups.length" class="px-3 py-2 text-2xs text-slate-500">
                  暂无可切换模型，请先到「设置 → 模型服务」配置。
                </p>
              </div>
              <!-- 管理模型入口（冻结在底部） -->
              <div class="shrink-0 border-t border-ink-700/60">
                <button
                  class="flex w-full items-center gap-2 px-3 py-2 text-left text-xs text-slate-400 transition-colors hover:bg-ink-800 hover:text-slate-200"
                  @click="openModelManage"
                >
                  <SlidersHorizontal :size="12" class="shrink-0" />
                  <span>管理模型</span>
                </button>
              </div>
            </div>
          </div>
        </Transition>

        <!-- 模型管理面板 -->
        <Transition name="drop">
          <div v-if="modelManageOpen">
            <div class="fixed inset-0 z-50" @click="modelManageOpen = false"></div>
            <div class="absolute bottom-full right-0 mb-2 z-50 w-72 rounded-card border border-ink-700 bg-ink-900 shadow-2xl shadow-black/40">
              <!-- 搜索 -->
              <div class="flex items-center gap-2 border-b border-ink-700/60 px-3 py-2">
                <Search :size="13" class="shrink-0 text-slate-500" />
                <input
                  v-model="modelSearch"
                  type="text"
                  placeholder="搜索模型..."
                  class="flex-1 bg-transparent py-1 text-xs text-slate-200 outline-none placeholder:text-slate-500"
                />
              </div>
              <!-- 模型列表 -->
              <div class="max-h-80 overflow-y-auto">
                <template v-for="grp in filteredModelGroups" :key="grp.provider">
                  <div class="flex items-center justify-between px-3 pt-2.5 pb-1">
                    <span class="text-[10px] font-semibold uppercase tracking-wide text-slate-500">{{ grp.provider }}</span>
                  </div>
                  <button
                    v-for="o in grp.options"
                    :key="o.value"
                    class="flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-sm text-slate-300 transition-colors hover:bg-ink-800"
                    @click="ws.toggleModelVisible(o.value)"
                  >
                    <div
                      class="flex h-4 w-4 shrink-0 items-center justify-center rounded transition-colors"
                      :class="ws.isModelVisible(o.value) ? 'bg-brand-400' : 'bg-ink-700'"
                    >
                      <svg
                        v-if="ws.isModelVisible(o.value)"
                        width="10" height="10" viewBox="0 0 24 24" fill="none"
                        stroke="white" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"
                      >
                        <polyline points="20 6 9 17 4 12" />
                      </svg>
                    </div>
                    <span class="flex-1 truncate">{{ shortLabel(o.label) }}</span>
                    <span
                      class="shrink-0 rounded px-1.5 py-0.5 text-[10px] font-medium"
                      :class="o.free ? 'bg-emerald-500/10 text-emerald-400' : o.configured ? 'bg-brand/20 text-brand-400' : 'bg-ink-800 text-slate-500'"
                    >{{ o.free ? '免费' : o.configured ? '已配置' : '未配置' }}</span>
                  </button>
                </template>
                <p v-if="!filteredModelGroups.length" class="px-3 py-4 text-center text-2xs text-slate-500">无匹配模型</p>
              </div>
            </div>
          </div>
        </Transition>
      </div>

      <span class="flex items-center gap-1">
        支持图片、文档、代码等文件。AI 回答仅供参考。
      </span>
    </p>
  </div>
</template>
