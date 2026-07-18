<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { Upload, ShieldCheck, Save } from 'lucide-vue-next'
import { useWorkspaceStore } from '../../stores/workspace'
import { useChatStore } from '../../stores/chat'
import { api } from '../../api/client'

const ws = useWorkspaceStore()
const chat = useChatStore()
const busy = ref(false)

// 回答范围：仅保留一个高频开关（严格仅用资料回答）。其余检索调参已收起，聊天时直接上传资料即可。
const ragCfg = reactive({
  chatTopK: 5,
  minScore: 0.6,
  sourceFilter: '',
  strictContextOnly: false,
})
function loadRagCfg() {
  const s = ws.settings
  if (!s) return
  const rag = (s.rag || {}) as Record<string, unknown>
  ragCfg.chatTopK = Number(rag['chatTopK'] ?? 5) || 5
  ragCfg.minScore = Number(rag['minScore'] ?? 0.6) || 0.6
  ragCfg.sourceFilter = String(rag['sourceFilter'] ?? '')
  ragCfg.strictContextOnly = Boolean(rag['strictContextOnly'])
}
async function saveRag() {
  busy.value = true
  try {
    await api.saveSettings({
      provider: {},
      embedding: {},
      rag: {
        chatTopK: ragCfg.chatTopK,
        minScore: ragCfg.minScore,
        sourceFilter: ragCfg.sourceFilter,
        strictContextOnly: ragCfg.strictContextOnly,
      },
      runtime: {},
      computer: {},
    })
    ws.showToast('success', '知识库设置已保存，服务已热重载')
    await ws.loadSettings()
    await ws.loadRagStatus()
    chat.applySettings(ws.settings)
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}
onMounted(() => {
  loadRagCfg()
})

// 上传
const ID_WHITELIST = /^[a-zA-Z0-9._-]+$/
const MAX_FILE_BYTES = 20 * 1024 * 1024
const ACCEPTED_EXT = '.txt,.md,.json,.csv,.xml,.html,.log,.docx,.pdf,.xlsx,.xls,.pptx,.ppt'

const uploadId = ref('')
const uploadText = ref('')
const uploadFile = ref<HTMLInputElement | null>(null)

async function doUploadText() {
  const id = uploadId.value.trim()
  if (!ID_WHITELIST.test(id)) {
    ws.showToast('error', '资料标识仅允许字母、数字、. _ - 字符')
    return
  }
  if (!uploadText.value) return
  busy.value = true
  try {
    await api.ragUpload(id, uploadText.value)
    ws.showToast('success', '文本已上传并建立索引')
    uploadId.value = ''
    uploadText.value = ''
    await ws.loadRagStatus()
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}

async function onFilePicked(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  if (f.size > MAX_FILE_BYTES) {
    ws.showToast('error', `文件过大，上限 ${MAX_FILE_BYTES / 1024 / 1024}MB`)
    ;(e.target as HTMLInputElement).value = ''
    return
  }
  busy.value = true
  try {
    await api.ragUploadFile(f)
    ws.showToast('success', '文件已上传并索引：' + f.name)
    await ws.loadRagStatus()
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
    ;(e.target as HTMLInputElement).value = ''
  }
}
</script>

<template>
  <div class="space-y-4">
    <!-- 概览 -->
    <div class="grid grid-cols-3 gap-2">
      <div class="panel rounded-lg p-3">
        <p class="text-2xs text-slate-500">资料块</p>
        <p class="mt-0.5 text-lg font-semibold text-white">{{ ws.ragStatus?.chunkCount ?? 0 }}</p>
      </div>
      <div class="panel rounded-lg p-3">
        <p class="text-2xs text-slate-500">来源文件</p>
        <p class="mt-0.5 text-lg font-semibold text-white">{{ ws.ragStatus?.sourceCount ?? 0 }}</p>
      </div>
      <div class="panel rounded-lg p-3">
        <p class="text-2xs text-slate-500">状态</p>
        <p
          class="mt-0.5 text-sm font-semibold"
          :class="ws.ragStatus?.initialized ? 'text-brand-400' : 'text-danger'"
        >
          {{ ws.ragStatus?.initialized ? '已启用' : '未启用' }}
        </p>
      </div>
    </div>

    <!-- 上传资料：拆分为文件上传和文本粘贴两个独立区块，避免认知混乱 -->
    <section class="panel space-y-4 p-3">
      <h3 class="flex items-center gap-1.5 text-sm font-medium text-slate-200">
        <Upload :size="14" class="text-accent" /> 上传资料
      </h3>
      <p class="text-2xs text-slate-500">上传后任何对话都会自动检索引用。支持 .txt / .md / .pdf / .docx 等格式。</p>

      <!-- 方式1：上传文件（最常用，放在前面） -->
      <div class="space-y-2">
        <div class="flex items-center gap-2">
          <span class="h-px flex-1 bg-slate-700/50"></span>
          <span class="text-2xs text-slate-500">上传文件</span>
          <span class="h-px flex-1 bg-slate-700/50"></span>
        </div>
        <div class="flex gap-2">
          <button class="btn-outline flex-1" @click="uploadFile?.click()">
            <Upload :size="14" /> 选择文件
          </button>
          <input ref="uploadFile" type="file" :accept="ACCEPTED_EXT" class="hidden" @change="onFilePicked" />
        </div>
      </div>

      <!-- 方式2：粘贴文本（需要自定义标识） -->
      <div class="space-y-2">
        <div class="flex items-center gap-2">
          <span class="h-px flex-1 bg-slate-700/50"></span>
          <span class="text-2xs text-slate-500">粘贴文本</span>
          <span class="h-px flex-1 bg-slate-700/50"></span>
        </div>
        <input v-model="uploadId" placeholder="资料标识（如 readme.md）" class="input" />
        <textarea v-model="uploadText" rows="3" placeholder="粘贴文本内容…" class="input resize-y"></textarea>
        <button class="btn-primary w-full" :disabled="busy || !uploadId || !uploadText" @click="doUploadText">
          上传文本
        </button>
      </div>
    </section>

    <!-- 回答范围 -->
    <section class="panel flex items-center justify-between gap-4 p-3">
      <div class="min-w-0">
        <h3 class="flex items-center gap-1.5 text-sm font-medium text-slate-100">
          <ShieldCheck :size="14" class="text-accent" /> 严格仅用资料回答
        </h3>
        <p class="mt-0.5 text-2xs text-slate-500">勾选后不基于模型自身知识编造，只依据已上传资料。</p>
      </div>
      <button
        type="button"
        role="switch"
        :aria-checked="ragCfg.strictContextOnly"
        class="relative h-6 w-11 shrink-0 rounded-full transition-colors duration-200"
        :class="ragCfg.strictContextOnly ? 'bg-brand' : 'bg-ink-700'"
        @click="ragCfg.strictContextOnly = !ragCfg.strictContextOnly"
      >
        <span
          class="absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform duration-200"
          :class="ragCfg.strictContextOnly ? 'translate-x-5' : ''"
        />
      </button>
    </section>

    <button class="btn-primary w-full" :disabled="busy" @click="saveRag">
      <Save :size="15" /> 保存知识库设置
    </button>
  </div>
</template>
