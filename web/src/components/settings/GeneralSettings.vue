<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { Save } from 'lucide-vue-next'
import { useWorkspaceStore } from '../../stores/workspace'
import { useChatStore } from '../../stores/chat'
import { api } from '../../api/client'

const props = defineProps<{ mode: 'normal' | 'advanced' }>()
const ws = useWorkspaceStore()
const chat = useChatStore()
const busy = ref(false)

const cfg = reactive({
  apiKey: '',
  chatTopK: 5,
  minScore: 0,
  sourceFilter: '',
  strictContextOnly: false,
  computerEnabled: false,
})
function loadCfgFromSettings() {
  const s = ws.settings
  if (!s) return
  const rag = (s.rag || {}) as Record<string, unknown>
  cfg.chatTopK = Number(rag['chatTopK'] ?? 5) || 5
  cfg.minScore = Number(rag['minScore'] ?? 0) || 0
  cfg.sourceFilter = String(rag['sourceFilter'] ?? '')
  cfg.strictContextOnly = Boolean(rag['strictContextOnly'])
  cfg.computerEnabled = Boolean((s.computer || {})['enabled'])
}
async function saveSettings() {
  busy.value = true
  try {
    const chatTopK = Math.max(1, Math.min(20, Math.round(Number(cfg.chatTopK) || 5)))
    const minScore = Math.max(0, Math.min(1, Number(cfg.minScore) || 0))
    await api.saveSettings({
      provider: { apiKey: cfg.apiKey },
      embedding: {},
      rag: {
        chatTopK,
        minScore,
        sourceFilter: cfg.sourceFilter,
        strictContextOnly: cfg.strictContextOnly,
      },
      runtime: {},
      computer: { enabled: cfg.computerEnabled },
    })
    ws.showToast('success', '设置已保存，服务已热重载')
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
  loadCfgFromSettings()
})
</script>

<template>
  <div class="space-y-3">
    <section v-if="props.mode === 'advanced'" class="panel space-y-3 p-3">
      <h3 class="text-sm font-medium text-slate-200">模型与知识库</h3>
      <label class="block text-xs text-slate-400">API Key（仅写入本地 .env，不回显）</label>
      <input v-model="cfg.apiKey" type="password" placeholder="留空表示不修改" class="input" />
      <div class="grid grid-cols-2 gap-3">
        <label class="block text-xs text-slate-400">对话 TopK
          <input v-model.number="cfg.chatTopK" type="number" min="1" max="20" class="input mt-1" />
        </label>
        <label class="block text-xs text-slate-400">最低相似分
          <input v-model.number="cfg.minScore" type="number" step="0.01" min="0" class="input mt-1" />
        </label>
      </div>
      <label class="block text-xs text-slate-400">来源筛选（如 .md,.pdf）
        <input v-model="cfg.sourceFilter" class="input mt-1" />
      </label>
      <label class="flex cursor-pointer items-center gap-2 text-sm text-slate-300">
        <input v-model="cfg.strictContextOnly" type="checkbox" class="h-4 w-4 accent-brand" />
        严格仅用资料回答
      </label>
    </section>

    <section class="panel space-y-2 rounded-xl p-3">
      <h3 class="text-sm font-medium text-slate-200">计算机/命令工具</h3>
      <label class="flex cursor-pointer items-center gap-2 text-sm text-slate-300">
        <input v-model="cfg.computerEnabled" type="checkbox" class="h-4 w-4 accent-brand" />
        启用本地命令执行工具（默认关闭，谨慎开启）
      </label>
    </section>

    <button class="btn-primary w-full" :disabled="busy" @click="saveSettings">
      <Save :size="15" /> 保存设置
    </button>
    <p class="text-2xs text-slate-500">
      保存后会热重载服务；未填写的字段将沿用当前配置。
    </p>
  </div>
</template>
