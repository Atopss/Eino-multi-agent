<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useWorkspaceStore } from '../stores/workspace'
import { useChatStore } from '../stores/chat'
import AppSidebar from '../components/AppSidebar.vue'
import ChatView from '../components/ChatView.vue'
import RagPanel from '../components/RagPanel.vue'
import SettingsDrawer from '../components/SettingsDrawer.vue'
import Onboarding from '../components/Onboarding.vue'

const ws = useWorkspaceStore()
const chat = useChatStore()
const showRag = ref(localStorage.getItem('eino.ragPanel') === '1')
const showSettings = ref(false)
const sidebarOpen = ref(false)
const settingsJumpNewAgent = ref(false)
const showOnboarding = ref(false)

function dismissOnboarding() {
  showOnboarding.value = false
  // 已引导过，后续不再自动弹出（仍可手动在设置里操作）
  localStorage.setItem('eino.onboarded', '1')
}
function createFirstAgent() {
  showOnboarding.value = false
  localStorage.setItem('eino.onboarded', '1')
  openNewAgent()
}

function toggleRag() {
  showRag.value = !showRag.value
  localStorage.setItem('eino.ragPanel', showRag.value ? '1' : '0')
}
function toggleSidebar() {
  sidebarOpen.value = !sidebarOpen.value
}
function openSettings() {
  showSettings.value = true
}
function openNewAgent() {
  // 打开设置抽屉并直接跳到「智能体」页、展开新建表单
  settingsJumpNewAgent.value = true
  showSettings.value = true
}
function onSettingsJumpDone() {
  settingsJumpNewAgent.value = false
}
function closeSidebar() {
  sidebarOpen.value = false
}

onMounted(async () => {
  await ws.init()
  chat.applySettings(ws.settings)
  if (!ws.activeSessionId) chat.newSession()
  // 首次进入且无智能体时，弹出引导
  if (!localStorage.getItem('eino.onboarded') && ws.agents.length === 0) {
    showOnboarding.value = true
  }
})
</script>

<template>
  <div class="flex h-screen w-screen overflow-hidden bg-ink-950 text-slate-200">
    <!-- 移动端侧栏遮罩 -->
    <transition name="fade">
      <div
        v-if="sidebarOpen"
        class="fixed inset-0 z-20 bg-black/55 md:hidden"
        @click="closeSidebar"
      ></div>
    </transition>

    <!-- 侧栏：移动端为抽屉，桌面端常驻 -->
    <div
      class="fixed inset-y-0 left-0 z-30 transform transition-transform duration-200 md:static md:translate-x-0"
      :class="sidebarOpen ? 'translate-x-0' : '-translate-x-full'"
    >
      <AppSidebar
        @open-settings="openSettings"
        @new-agent="openNewAgent"
        @toggle-rag="toggleRag"
        @navigate="closeSidebar"
      />
    </div>

    <!-- 主区 + 引用面板 -->
    <div class="flex min-w-0 flex-1">
      <ChatView @toggle-rag="toggleRag" @toggle-sidebar="toggleSidebar" />
      <RagPanel v-if="showRag" :open="showRag" @close="toggleRag" />
    </div>

    <SettingsDrawer
      :open="showSettings"
      :jump-new-agent="settingsJumpNewAgent"
      @close="showSettings = false"
      @jump-done="onSettingsJumpDone"
    />

    <!-- 首次引导 -->
    <Onboarding
      v-if="showOnboarding"
      @create-agent="createFirstAgent"
      @dismiss="dismissOnboarding"
    />

    <!-- 全局提示 -->
    <transition name="toast">
      <div
        v-if="ws.toast"
        class="fixed bottom-5 left-1/2 z-50 max-w-[90vw] -translate-x-1/2 rounded-card border px-4 py-2 text-sm shadow-panel"
        :class="
          ws.toast.kind === 'error'
            ? 'border-danger/50 bg-danger/15 text-danger'
            : 'border-brand/40 bg-ink-900 text-slate-100'
        "
      >
        {{ ws.toast.text }}
      </div>
    </transition>
  </div>
</template>

<style scoped>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.18s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
.toast-enter-active,
.toast-leave-active {
  transition: all 0.22s ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translate(-50%, 8px);
}
</style>
