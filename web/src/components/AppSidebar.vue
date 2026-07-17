<script setup lang="ts">
import {
  Plus,
  MessageSquare,
  Bot,
  Settings,
  Trash2,
  Database,
  PanelRight,
  Sun,
  Moon,
  LogOut,
  ChevronDown,
  ChevronRight,
  FolderTree,
} from 'lucide-vue-next'
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useWorkspaceStore } from '../stores/workspace'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import type { SessionMeta } from '../types/api'
import FileBrowser from './FileBrowser.vue'

const ws = useWorkspaceStore()
const chat = useChatStore()
const auth = useAuthStore()
const router = useRouter()

// 协调者 = 智能体列表首位（固定置顶），由系统自动担任，可调度其余所有智能体。
const coordinatorName = computed(() => (ws.agents.length ? ws.agents[0].name : ''))

const sidebarTab = ref<'sessions' | 'files'>('sessions')

const emit = defineEmits<{
  (e: 'open-settings'): void
  (e: 'new-agent'): void
  (e: 'toggle-rag'): void
  (e: 'navigate'): void
}>()

function selectSession(s: SessionMeta) {
  if (s.id === ws.activeSessionId) return
  // 进入某会话时同步切到其归属的智能体，并展开对应分组。
  if (s.agent) {
    ws.activeAgent = s.agent
    ws.expandedAgents = { ...ws.expandedAgents, [s.agent]: true }
  }
  chat.loadSession(s.id)
  emit('navigate')
}

function logout() {
  auth.logout()
  router.push('/login')
}
</script>

<template>
  <aside class="flex h-full w-72 shrink-0 flex-col border-r border-ink-800 bg-ink-950">
    <!-- 品牌 -->
    <div class="flex h-14 items-center gap-2.5 border-b border-ink-800 px-4">
      <div class="flex h-8 w-8 items-center justify-center rounded-lg bg-brand/15 text-brand-400 shadow-glow">
        <Bot :size="18" />
      </div>
      <div class="leading-tight flex-1">
        <p class="text-sm font-semibold text-white">Eino 工作台</p>
        <p class="text-2xs text-slate-500">智能体 · 知识库 · 多智能体</p>
      </div>
      <button
        class="btn-ghost !p-1.5"
        :title="ws.theme === 'dark' ? '切换到亮色' : '切换到暗色'"
        @click="ws.toggleTheme()"
      >
        <component :is="ws.theme === 'dark' ? Sun : Moon" :size="16" />
      </button>
    </div>

    <!-- 操作区 -->
    <div class="flex flex-col gap-2 p-3">
      <button class="btn-primary w-full" @click="chat.newSession(); emit('navigate')">
        <Plus :size="16" /> 新建对话
      </button>

      <button class="btn-outline w-full !justify-start" @click="emit('new-agent')">
        <Plus :size="15" /> 新建智能体
      </button>

      <button class="btn-outline w-full !justify-start" @click="emit('toggle-rag')">
        <PanelRight :size="15" /> 引用来源
      </button>
    </div>

    <!-- 智能体 / 会话分组 / 文件 Tab 切换 -->
    <div class="flex items-center justify-between border-b border-ink-800 px-4 py-1">
      <div class="flex items-center gap-1">
        <button
          class="rounded-md px-3 py-1.5 text-2xs font-medium transition-colors"
          :class="sidebarTab === 'sessions' ? 'bg-brand/15 text-brand-400' : 'text-slate-500 hover:text-slate-300'"
          @click="sidebarTab = 'sessions'"
        >
          <MessageSquare :size="12" class="inline mr-1" />
          会话
        </button>
        <button
          class="rounded-md px-3 py-1.5 text-2xs font-medium transition-colors"
          :class="sidebarTab === 'files' ? 'bg-accent/15 text-accent' : 'text-slate-500 hover:text-slate-300'"
          @click="sidebarTab = 'files'"
        >
          <FolderTree :size="12" class="inline mr-1" />
          文件
        </button>
      </div>
      <span class="text-2xs text-slate-600">{{ ws.sessions.length }} 对话</span>
    </div>

    <!-- 文件浏览器 -->
    <div v-if="sidebarTab === 'files'" class="min-h-0 flex-1">
      <FileBrowser />
    </div>

    <!-- 会话列表 -->
    <div v-else class="min-h-0 flex-1 overflow-y-auto px-3 pb-3">
      <p v-if="!ws.agents.length" class="px-1 py-6 text-center text-xs text-slate-600">
        还没有智能体，点上方"新建智能体"。
      </p>
      <div v-for="a in ws.agents" :key="a.name" class="mb-1">
        <button
          class="group flex w-full items-center gap-2 rounded-lg border border-transparent px-3 py-2 text-left transition-colors"
          :class="a.name === coordinatorName ? 'border-brand/50 bg-brand/10 shadow-glow' : (a.name === ws.activeAgent ? 'bg-ink-900' : 'hover:bg-ink-900')"
          @click="ws.toggleAgentGroup(a.name)"
        >
          <component
            :is="ws.expandedAgents[a.name] ? ChevronDown : ChevronRight"
            :size="14"
            class="shrink-0 text-slate-500"
          />
          <Bot
            :size="15"
            class="shrink-0"
            :class="a.name === coordinatorName ? 'text-brand-400' : (a.name === ws.activeAgent ? 'text-brand-400' : 'text-slate-500')"
          />
          <span class="flex min-w-0 flex-1 items-center gap-1.5 truncate">
            <span class="truncate text-sm font-medium text-slate-100">{{ a.name }}</span>
            <span v-if="a.name === coordinatorName" class="shrink-0 rounded bg-brand/15 px-1.5 py-0.5 text-[10px] font-medium text-brand-400">协调者</span>
          </span>
          <span class="text-[10px] text-slate-600">{{ (ws.sessionsByAgent[a.name] || []).length }}</span>
        </button>

        <div v-if="ws.expandedAgents[a.name]" class="ml-3.5 space-y-1 border-l border-ink-800 pl-2">
          <p v-if="!(ws.sessionsByAgent[a.name] || []).length" class="px-1 py-2 text-2xs text-slate-600">
            暂无对话
          </p>
          <button
            v-for="s in ws.sessionsByAgent[a.name] || []"
            :key="s.id"
            class="group flex w-full items-start gap-2 rounded-lg border px-3 py-2 text-left transition-colors"
            :class="
              s.id === ws.activeSessionId
                ? 'border-brand/40 bg-brand/10'
                : 'border-transparent hover:bg-ink-900'
            "
            @click="selectSession(s)"
          >
            <MessageSquare :size="15" class="mt-0.5 shrink-0 text-slate-500" />
            <div class="min-w-0 flex-1">
              <p class="truncate text-sm font-medium text-slate-100">{{ s.title }}</p>
              <p class="truncate text-2xs text-slate-500">
                {{ s.preview || '（空对话）' }}
              </p>
            </div>
            <span
              class="shrink-0 text-[10px] text-slate-600 group-hover:hidden"
              >{{ ws.formatUpdated(s.updatedAt) }}</span
            >
            <button
              class="hidden shrink-0 text-slate-500 hover:text-danger group-hover:block"
              aria-label="删除会话"
              @click.stop="ws.deleteSession(s.id)"
            >
              <Trash2 :size="14" />
            </button>
          </button>
          <button
            class="flex w-full items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs text-slate-500 hover:bg-ink-900 hover:text-slate-300"
            @click.stop="chat.newSession(a.name); ws.activeAgent = a.name; emit('navigate')"
          >
            <Plus :size="13" /> 在该智能体下新建
          </button>
        </div>
        <div v-if="a.name === coordinatorName && ws.agents.length > 1" class="my-1.5 border-t border-brand/20"></div>
      </div>
    </div>

    <!-- 底部 -->
    <div class="flex items-center justify-between border-t border-ink-800 px-3 py-2">
      <div class="flex items-center gap-1.5 text-2xs">
        <span
          class="h-2 w-2 rounded-full"
          :class="ws.ragStatus && ws.ragStatus.initialized ? 'bg-brand shadow-glow' : 'bg-slate-600'"
        ></span>
        <span class="flex items-center gap-1 text-slate-400">
          <Database :size="12" />
          {{ ws.ragStatus && ws.ragStatus.initialized ? ws.ragStatus.sourceCount + ' 资料源' : '知识库未启用' }}
        </span>
      </div>
      <button class="btn-ghost !p-1.5" aria-label="设置" @click="emit('open-settings')">
        <Settings :size="16" />
      </button>
      <button class="btn-ghost !p-1.5" aria-label="退出登录" title="退出登录" @click="logout">
        <LogOut :size="16" />
      </button>
    </div>
  </aside>
</template>
