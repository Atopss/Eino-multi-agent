<script setup lang="ts">
import { ref, computed } from 'vue'
import { X, FileSearch, Wrench, ListTree, Database, ArrowRight } from 'lucide-vue-next'
import { useChatStore } from '../stores/chat'
import RagReferenceItem from './RagReferenceItem.vue'
import ToolCallItem from './ToolCallItem.vue'
import TraceItem from './TraceItem.vue'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const chat = useChatStore()
const tab = ref<'refs' | 'tools' | 'trace'>('refs')

const refN = computed(() => chat.rag.references.length)
const toolN = computed(() => chat.rag.toolCalls.length)
const traceN = computed(() => chat.rag.traceItems.length)
const hasData = computed(
  () => refN.value > 0 || toolN.value > 0 || traceN.value > 0 || !!chat.rag.query,
)
</script>

<template>
  <aside
    class="flex h-full w-[360px] shrink-0 flex-col border-l border-ink-800 bg-ink-950/70"
  >
    <header class="flex h-14 shrink-0 items-center justify-between border-b border-ink-800 px-4">
      <div class="flex items-center gap-2 text-sm font-semibold text-slate-100">
        <Database :size="16" class="text-accent" />
        引用与追踪
      </div>
      <button class="btn-ghost !p-1.5" aria-label="关闭面板" @click="emit('close')">
        <X :size="16" />
      </button>
    </header>

    <div class="flex-1 overflow-y-auto px-3 py-3">
      <!-- 空态 -->
      <div
        v-if="!hasData && !chat.streaming"
        class="mt-10 flex flex-col items-center gap-2 px-6 text-center text-sm text-slate-500"
      >
        <FileSearch :size="30" class="text-ink-700" />
        <p>对话时，这里会实时展示<br />检索到的资料、工具调用与执行过程。</p>
      </div>

      <template v-else>
        <!-- 检索问题 -->
        <div v-if="chat.rag.query" class="panel mb-3 rounded-lg p-3">
          <p class="text-2xs uppercase tracking-wide text-slate-500">检索问题</p>
          <p class="mt-1 flex items-start gap-1.5 text-sm text-slate-200">
            <ArrowRight :size="14" class="mt-0.5 shrink-0 text-accent" />
            {{ chat.rag.query }}
          </p>
        </div>

        <!-- 流式进行中提示 -->
        <p
          v-if="chat.streaming"
          class="mb-3 flex items-center gap-2 rounded-lg border border-ink-800 bg-ink-900/50 px-3 py-2 text-xs text-slate-400"
        >
          <span class="h-2 w-2 animate-pulse-dot rounded-full bg-accent"></span>
          正在生成，结果将实时出现在这里…
        </p>

        <!-- 分段切换 -->
        <div class="mb-3 flex gap-1 rounded-lg bg-ink-900 p-1 text-xs">
          <button
            class="flex-1 rounded-md px-2 py-1.5 font-medium transition-colors"
            :class="tab === 'refs' ? 'bg-ink-800 text-white' : 'text-slate-400 hover:text-slate-200'"
            title="本轮回答实际引用的本地资料片段，以及它们的出处（来自哪个文件/章节）。"
            @click="tab = 'refs'"
          >
            <FileSearch :size="12" class="mr-1 inline" />引用 {{ refN }}
          </button>
          <button
            class="flex-1 rounded-md px-2 py-1.5 font-medium transition-colors"
            :class="tab === 'tools' ? 'bg-ink-800 text-white' : 'text-slate-400 hover:text-slate-200'"
            title="本轮回答过程中调用的工具（如搜索、计算、读文件等）。"
            @click="tab = 'tools'"
          >
            <Wrench :size="12" class="mr-1 inline" />工具 {{ toolN }}
          </button>
          <button
            class="flex-1 rounded-md px-2 py-1.5 font-medium transition-colors"
            :class="tab === 'trace' ? 'bg-ink-800 text-white' : 'text-slate-400 hover:text-slate-200'"
            title="底层执行轨迹与时间线，偏调试用途；普通对话看不懂也无需理会。"
            @click="tab = 'trace'"
          >
            <ListTree :size="12" class="mr-1 inline" />追踪 {{ traceN }}
          </button>
        </div>

        <!-- 引用 -->
        <div v-if="tab === 'refs'" class="flex flex-col gap-2">
          <RagReferenceItem v-for="(r, i) in chat.rag.references" :key="r.id || i" :item="r" />
          <p v-if="!refN" class="px-1 py-4 text-center text-xs text-slate-500">本轮未命中本地资料。</p>
        </div>

        <!-- 工具 -->
        <div v-if="tab === 'tools'" class="flex flex-col gap-2">
          <ToolCallItem v-for="(t, i) in chat.rag.toolCalls" :key="i" :call="t" />
          <p v-if="!toolN" class="px-1 py-4 text-center text-xs text-slate-500">本次未调用工具。</p>
        </div>

        <!-- 追踪 -->
        <div v-if="tab === 'trace'" class="flex flex-col gap-2">
          <TraceItem v-for="(t, i) in chat.rag.traceItems" :key="i" :item="t" />
          <p v-if="!traceN" class="px-1 py-4 text-center text-xs text-slate-500">暂无执行轨迹。</p>
        </div>
      </template>
    </div>
  </aside>
</template>
