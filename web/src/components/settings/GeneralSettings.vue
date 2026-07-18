<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { Save, KeyRound, Terminal, Palette } from 'lucide-vue-next'
import { useWorkspaceStore } from '../../stores/workspace'
import { useChatStore } from '../../stores/chat'
import { api } from '../../api/client'

const ws = useWorkspaceStore()
const chat = useChatStore()
const busy = ref(false)

const cfg = reactive({
  apiKey: '',
  computerEnabled: false,
})

function loadCfgFromSettings() {
  const s = ws.settings
  if (!s) return
  cfg.computerEnabled = Boolean((s.computer || {})['enabled'])
}

// 主题开关（复用 store 既有逻辑，偏好与侧边栏同步）
const isDark = ref(ws.theme === 'dark')
function toggleTheme() {
  ws.toggleTheme()
  isDark.value = ws.theme === 'dark'
}

async function saveSettings() {
  busy.value = true
  try {
    await api.saveSettings({
      provider: { apiKey: cfg.apiKey },
      embedding: {},
      rag: {},
      runtime: {},
      computer: { enabled: cfg.computerEnabled },
    })
    ws.showToast('success', '设置已保存，服务已热重载')
    await ws.loadSettings()
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
  <div class="space-y-4">
    <!-- 通用 -->
    <section class="panel divide-y divide-ink-800 overflow-hidden rounded-card">
      <header class="flex items-center gap-1.5 px-4 py-3">
        <KeyRound :size="14" class="text-accent" />
        <h3 class="text-sm font-medium text-slate-200">通用</h3>
      </header>

      <div class="flex items-center justify-between gap-4 px-4 py-3">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-100">模型 API Key</p>
          <p class="mt-0.5 text-2xs text-slate-500">仅写入本地 .env，不回显、不落盘</p>
        </div>
        <input
          v-model="cfg.apiKey"
          type="password"
          placeholder="留空表示不修改"
          class="input !w-52 shrink-0"
        />
      </div>

      <div class="flex items-center justify-between gap-4 px-4 py-3">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-100">计算机 / 命令工具</p>
          <p class="mt-0.5 text-2xs text-slate-500">允许智能体在本地执行命令（默认关闭，谨慎开启）</p>
        </div>
        <button
          type="button"
          role="switch"
          :aria-checked="cfg.computerEnabled"
          class="relative h-6 w-11 shrink-0 rounded-full transition-colors duration-200"
          :class="cfg.computerEnabled ? 'bg-brand' : 'bg-ink-700'"
          @click="cfg.computerEnabled = !cfg.computerEnabled"
        >
          <span
            class="absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform duration-200"
            :class="cfg.computerEnabled ? 'translate-x-5' : ''"
          />
        </button>
      </div>
    </section>

    <!-- 界面 -->
    <section class="panel divide-y divide-ink-800 overflow-hidden rounded-card">
      <header class="flex items-center gap-1.5 px-4 py-3">
        <Palette :size="14" class="text-accent" />
        <h3 class="text-sm font-medium text-slate-200">界面</h3>
      </header>

      <div class="flex items-center justify-between gap-4 px-4 py-3">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-100">主题</p>
          <p class="mt-0.5 text-2xs text-slate-500">跟随系统的暗色，或切换为亮色</p>
        </div>
        <button
          type="button"
          role="switch"
          :aria-checked="isDark"
          class="relative h-6 w-11 shrink-0 rounded-full transition-colors duration-200"
          :class="isDark ? 'bg-brand' : 'bg-ink-700'"
          @click="toggleTheme"
        >
          <span
            class="absolute top-0.5 left-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform duration-200"
            :class="isDark ? 'translate-x-5' : ''"
          />
        </button>
      </div>
    </section>

    <button class="btn-primary w-full" :disabled="busy" @click="saveSettings">
      <Save :size="15" /> 保存设置
    </button>
    <p class="text-2xs text-slate-500">
      保存后会热重载服务；未填写的字段将沿用当前配置。
    </p>
  </div>
</template>
