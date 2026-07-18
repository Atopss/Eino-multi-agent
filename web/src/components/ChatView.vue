<script setup lang="ts">
import { ref, watch, nextTick, onMounted } from 'vue'
import { Bot, PanelRight, Sparkles, Menu, ChevronDown, Boxes, Check } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import { useWorkspaceStore } from '../stores/workspace'
import ChatMessage from './ChatMessage.vue'
import ChatComposer from './ChatComposer.vue'

const chat = useChatStore()
const ws = useWorkspaceStore()
const emit = defineEmits<{
  (e: 'toggle-rag'): void
  (e: 'toggle-sidebar'): void
}>()

const scroll = ref<HTMLElement | null>(null)

// 全局模型选择器：聊天栏顶栏常驻，按商家分组，切换即持久化（ws.setActiveModel 写 localStorage）。
const modelMenuOpen = ref(false)
function selectModel(v: string) {
  ws.setActiveModel(v)
  modelMenuOpen.value = false
}

const suggestions = [
  '用通俗的话解释什么是知识库检索，以及它和微调的区别',
  '帮我基于本地资料总结本周要点',
  '这份文档里提到的关键流程是什么？定位到具体章节',
]

function scrollToBottom() {
  nextTick(() => {
    if (scroll.value) scroll.value.scrollTop = scroll.value.scrollHeight
  })
}

watch(
  () => [chat.messages.length, chat.messages[chat.messages.length - 1]?.content],
  scrollToBottom,
)
onMounted(scrollToBottom)
</script>

<template>
  <section class="flex h-full min-w-0 flex-1 flex-col bg-ink-950">
    <!-- 顶栏 -->
    <header
      class="flex h-auto min-h-14 shrink-0 flex-wrap items-center justify-between gap-2 border-b border-ink-800 px-4 py-2"
    >
      <div class="flex min-w-0 items-center gap-2.5">
        <button
          class="btn-ghost !p-1.5 md:hidden"
          aria-label="菜单"
          @click="emit('toggle-sidebar')"
        >
          <Menu :size="18" />
        </button>
        <div
          class="flex h-8 w-8 items-center justify-center rounded-lg bg-ink-800 text-accent"
        >
          <Bot :size="17" />
        </div>
        <div class="min-w-0 leading-tight">
          <p class="truncate text-sm font-semibold text-white">
            {{ ws.activeAgentInfo?.name || '未选择智能体' }}
          </p>
          <div class="relative">
            <button
              class="flex items-center gap-1 truncate text-[11px] text-slate-400 transition-colors hover:text-brand-400"
              title="选择当前对话使用的模型（全局切换，与智能体无关）"
              @click="modelMenuOpen = !modelMenuOpen"
            >
              <Boxes :size="12" class="shrink-0 text-accent" />
              <span class="truncate">{{ ws.activeModelOption?.label || ws.activeModel || '选择模型' }}</span>
              <ChevronDown :size="12" class="shrink-0" />
            </button>
            <!-- 点击遮罩关闭下拉 -->
            <div
              v-if="modelMenuOpen"
              class="fixed inset-0 z-20"
              @click="modelMenuOpen = false"
            ></div>
            <div
              v-if="modelMenuOpen"
              class="absolute left-0 top-full z-30 mt-1 max-h-80 w-72 overflow-y-auto rounded-lg border border-ink-800 bg-ink-900/95 p-1.5 shadow-glow backdrop-blur"
            >
              <template v-if="ws.chatModelGroups.length">
                <div
                  v-for="g in ws.chatModelGroups"
                  :key="g.provider"
                  class="mb-1 last:mb-0"
                >
                  <p class="px-2 py-1 text-[10px] font-medium uppercase tracking-wide text-slate-500">
                    {{ g.provider }}
                  </p>
                  <button
                    v-for="o in g.options"
                    :key="o.value"
                    class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-[12px] transition-colors"
                    :class="o.value === ws.activeModel ? 'bg-brand/15 text-brand-400' : 'text-slate-300 hover:bg-ink-800'"
                    @click="selectModel(o.value)"
                  >
                    <Check v-if="o.value === ws.activeModel" :size="13" class="shrink-0" />
                    <span v-else class="w-[13px] shrink-0"></span>
                    <span class="truncate">{{ o.label || o.value }}</span>
                  </button>
                </div>
              </template>
              <p v-else class="px-2 py-3 text-center text-[11px] text-slate-500">
                还没有可用模型，请到「设置 → 模型服务」配置。
              </p>
            </div>
          </div>
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button
          class="btn-outline !py-1.5"
          title="打开右侧面板：实时查看回答所引用的本地资料、调用的工具与执行过程"
          @click="emit('toggle-rag')"
        >
          <PanelRight :size="15" /> 引用来源
        </button>
      </div>
    </header>

    <!-- 消息流 -->
    <div ref="scroll" class="min-h-0 flex-1 overflow-y-auto">
      <!-- 空态 -->
      <div
        v-if="!chat.messages.length"
        class="mx-auto flex max-w-2xl flex-col items-center gap-5 px-6 pt-20 text-center"
      >
        <div
          class="flex h-14 w-14 items-center justify-center rounded-2xl bg-brand/15 text-brand-400 shadow-glow"
        >
          <Sparkles :size="26" />
        </div>
        <div>
          <h2 class="text-lg font-semibold text-white">
            开始与 {{ ws.activeAgentInfo?.name || '智能体' }} 对话
          </h2>
          <p class="mt-1 text-sm text-slate-400">
            支持流式回答、本地知识库检索与工具调用，右侧面板实时展示依据。
          </p>
        </div>
        <div class="flex w-full flex-col gap-2">
          <button
            v-for="s in suggestions"
            :key="s"
            class="rounded-card border border-ink-800 bg-ink-900/60 px-4 py-2 text-left text-sm text-slate-300 transition-colors hover:border-brand/40 hover:bg-ink-900"
            @click="chat.send(s)"
          >
            {{ s }}
          </button>
        </div>
      </div>

      <!-- 消息列表 -->
      <div v-else class="mx-auto flex max-w-4xl flex-col gap-5 px-4 py-6">
        <ChatMessage
          v-for="m in chat.messages"
          :key="m.id"
          :message="m"
          @show-rag="emit('toggle-rag')"
        />
      </div>
    </div>

    <ChatComposer />
  </section>
</template>
