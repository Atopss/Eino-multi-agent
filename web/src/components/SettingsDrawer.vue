<script setup lang="ts">
import { ref, watch } from 'vue'
import { X, Database, Bot, SlidersHorizontal, Boxes } from 'lucide-vue-next'
import RagSettings from './settings/RagSettings.vue'
import AgentSettings from './settings/AgentSettings.vue'
import GeneralSettings from './settings/GeneralSettings.vue'
import ProviderSettings from './settings/ProviderSettings.vue'

const props = defineProps<{ open: boolean; jumpNewAgent?: boolean }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'jump-done'): void }>()

const tab = ref<'rag' | 'agent' | 'provider' | 'preferences'>('rag')

// 从侧边栏「新建智能体」进入时，切到智能体页并通知子组件弹出新建表单
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
  { key: 'preferences', label: '偏好', icon: SlidersHorizontal },
  { key: 'agent', label: '智能体', icon: Bot },
  { key: 'provider', label: '模型服务', icon: Boxes },
  { key: 'rag', label: '知识库', icon: Database },
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
          <h2 class="text-sm font-semibold text-white">设置</h2>
          <button class="btn-ghost !p-1.5" aria-label="关闭" @click="emit('close')">
            <X :size="18" />
          </button>
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
          <GeneralSettings v-if="tab === 'preferences'" />
          <AgentSettings v-else-if="tab === 'agent'" :new-nonce="agentNewNonce" />
          <ProviderSettings v-else-if="tab === 'provider'" />
          <RagSettings v-else-if="tab === 'rag'" />
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
