<script setup lang="ts">
import { ref, watch } from 'vue'
import { X, Database, Bot, SlidersHorizontal, Cpu } from 'lucide-vue-next'
import RagSettings from './settings/RagSettings.vue'
import AgentSettings from './settings/AgentSettings.vue'
import GeneralSettings from './settings/GeneralSettings.vue'
import SystemSettings from './settings/SystemSettings.vue'

const props = defineProps<{ open: boolean; jumpNewAgent?: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'jump-done'): void }>()

const tab = ref<'rag' | 'agent' | 'settings' | 'system'>('rag')
// 普通/高级：默认普通，只暴露日常操作；高级项（扫描目录、检索测试、知识库调参）需切到高级才显示
const mode = ref<'normal' | 'advanced'>('normal')

// 从侧边栏「新建智能体」进入时，切到智能体页并通知子组件展开新建表单
const agentNewNonce = ref(0)
watch(
  () => [props.open, props.jumpNewAgent],
  ([open, jump]) => {
    if (open && jump) {
      tab.value = 'agent'
      agentNewNonce.value++
      emit('jump-done')
    }
  },
)

const tabs = [
  { key: 'rag', label: '知识库', icon: Database },
  { key: 'agent', label: '智能体', icon: Bot },
  { key: 'settings', label: '设置', icon: SlidersHorizontal },
  { key: 'system', label: '系统', icon: Cpu },
] as const
</script>

<template>
  <transition name="drawer">
    <div v-if="props.open" class="fixed inset-0 z-40 flex justify-end">
      <!-- 遮罩 -->
      <div class="absolute inset-0 bg-black/55 backdrop-blur-sm" @click="emit('close')"></div>

      <!-- 抽屉 -->
      <div class="relative flex h-full w-full max-w-xl flex-col border-l border-ink-800 bg-ink-950 shadow-panel">
        <header class="flex h-14 shrink-0 items-center justify-between border-b border-ink-800 px-4">
          <h2 class="text-sm font-semibold text-white">工作台设置</h2>
          <div class="flex items-center gap-2">
            <div class="flex rounded-lg bg-ink-900 p-0.5 text-xs">
              <button
                class="rounded-md px-3 py-1 font-medium transition-colors"
                :class="mode === 'normal' ? 'bg-ink-700 text-white' : 'text-slate-400 hover:text-slate-200'"
                @click="mode = 'normal'"
              >普通</button>
              <button
                class="rounded-md px-3 py-1 font-medium transition-colors"
                :class="mode === 'advanced' ? 'bg-ink-700 text-white' : 'text-slate-400 hover:text-slate-200'"
                @click="mode = 'advanced'"
              >高级</button>
            </div>
            <button class="btn-ghost !p-1.5" aria-label="关闭" @click="emit('close')">
              <X :size="18" />
            </button>
          </div>
        </header>

        <!-- 页签 -->
        <nav class="flex shrink-0 gap-1 border-b border-ink-800 px-3 py-2">
          <button
            v-for="t in tabs"
            :key="t.key"
            class="flex flex-1 items-center justify-center gap-1.5 rounded-lg px-2 py-2 text-xs font-medium transition-colors"
            :class="tab === t.key ? 'bg-ink-800 text-white' : 'text-slate-400 hover:text-slate-200'"
            @click="tab = t.key"
          >
            <component :is="t.icon" :size="14" />
            {{ t.label }}
          </button>
        </nav>

        <div class="min-h-0 flex-1 overflow-y-auto p-4">
          <RagSettings v-if="tab === 'rag'" :mode="mode" />
          <AgentSettings v-else-if="tab === 'agent'" :new-nonce="agentNewNonce" />
          <GeneralSettings v-else-if="tab === 'settings'" :mode="mode" />
          <SystemSettings v-else-if="tab === 'system'" />
        </div>
      </div>
    </div>
  </transition>
</template>

<style scoped>
.drawer-enter-active,
.drawer-leave-active {
  transition: opacity 0.2s ease;
}
.drawer-enter-from,
.drawer-leave-to {
  opacity: 0;
}
</style>
