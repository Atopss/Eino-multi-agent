<script setup lang="ts">
import { computed, ref } from 'vue'
import {
  FileSearch,
  Wrench,
  Sparkles,
  ChevronRight,
  ChevronDown,
} from 'lucide-vue-next'
import type { AgentStep } from '../types/api'

const props = defineProps<{
  steps?: AgentStep[]
  streaming?: boolean
}>()

// 用户手动展开状态
const expanded = ref(false)

const steps = computed(() => props.steps ?? [])
const hasSteps = computed(() => steps.value.length > 0)
const ragCount = computed(() => steps.value.filter((s) => s.kind === 'rag').length)
const toolCount = computed(() => steps.value.filter((s) => s.kind === 'tool').length)

// 进行中：仍有未完成步骤，或整条消息仍在流式输出
const isLive = computed(
  () => !!props.streaming || steps.value.some((s) => s.status === 'running'),
)

// 折叠态：已完成、且用户未手动展开
const collapsed = computed(() => !isLive.value && !expanded.value)

const summaryText = computed(() => {
  const parts: string[] = []
  if (ragCount.value) parts.push(`检索 ${ragCount.value} 次`)
  if (toolCount.value) parts.push(`工具 ${toolCount.value} 次`)
  return parts.join(' · ') || '执行过程'
})

// 每条步骤的细节展开（按 kind+name+index 记忆）
const openSet = ref<Set<string>>(new Set())
function stepKey(s: AgentStep, i: number) {
  return `${s.kind}:${s.name}:${i}`
}
function toggleStep(s: AgentStep, i: number) {
  if (!s.input && !s.output) return
  const key = stepKey(s, i)
  const next = new Set(openSet.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  openSet.value = next
}
function isOpen(s: AgentStep, i: number) {
  return openSet.value.has(stepKey(s, i))
}

function statusDot(status: string) {
  if (status === 'running') return 'bg-accent animate-pulse-dot'
  if (status === 'error') return 'bg-danger'
  return 'bg-brand-400'
}
function statusText(status: string) {
  if (status === 'running') return 'text-accent'
  if (status === 'error') return 'text-danger'
  return 'text-brand-400'
}
function statusLabel(s: AgentStep) {
  if (s.status === 'running') return '执行中…'
  if (s.status === 'error') return '失败'
  if (s.kind === 'rag') return s.output ? s.output : '已完成'
  return '已完成'
}
function kindIcon(kind: string) {
  if (kind === 'rag') return FileSearch
  if (kind === 'tool') return Wrench
  return Sparkles
}
</script>

<template>
  <div
    v-if="hasSteps || isLive"
    class="mt-2 rounded-card border border-ink-800 bg-ink-900/50 text-xs"
  >
    <!-- 头部：标题 + 摘要/进行中 + 折叠箭头 -->
    <button
      class="flex w-full items-center gap-1.5 px-3 py-2 text-left text-slate-300"
      @click="expanded = !expanded"
    >
      <Sparkles :size="14" class="text-brand-400" />
      <span class="font-semibold">执行过程</span>
      <span
        v-if="!isLive && summaryText"
        class="ml-1 rounded bg-ink-800 px-1.5 py-0.5 text-[10px] text-slate-400"
      >
        {{ summaryText }}
      </span>
      <span class="ml-auto flex items-center gap-1.5 text-2xs text-slate-500">
        <span v-if="isLive" class="flex items-center gap-1 text-accent">
          <span class="h-1.5 w-1.5 animate-pulse-dot rounded-full bg-accent"></span>
          进行中
        </span>
        <component :is="collapsed ? ChevronRight : ChevronDown" :size="14" />
      </span>
    </button>

    <!-- 展开内容 -->
    <div v-if="!collapsed" class="border-t border-ink-800 px-3 py-2">
      <div v-for="(s, i) in steps" :key="i" class="flex flex-col">
        <button
          class="flex items-start gap-2 rounded-control px-1.5 py-1.5 text-left transition hover:bg-ink-800/50"
          :class="(s.input || s.output) ? 'cursor-pointer' : 'cursor-default'"
          @click="toggleStep(s, i)"
        >
          <span
            class="mt-1.5 h-2 w-2 shrink-0 rounded-full"
            :class="statusDot(s.status)"
          ></span>
          <component
            :is="kindIcon(s.kind)"
            :size="14"
            class="mt-1 shrink-0"
            :class="statusText(s.status)"
          />
          <div class="min-w-0 flex-1">
            <p class="truncate text-slate-200">{{ s.title || s.name }}</p>
            <p class="text-2xs" :class="statusText(s.status)">
              {{ statusLabel(s) }}
            </p>
          </div>
          <ChevronDown
            v-if="s.input || s.output"
            :size="13"
            class="mt-1 shrink-0 text-slate-500 transition"
            :class="isOpen(s, i) ? 'rotate-180' : ''"
          />
        </button>

        <!-- 入参 / 出参明细（默认折叠） -->
        <div v-if="isOpen(s, i)" class="mb-1.5 ml-7 space-y-1.5">
          <div v-if="s.input">
            <p class="text-[10px] uppercase tracking-wide text-slate-500">入参</p>
            <pre class="max-h-40 overflow-auto rounded-control border border-ink-800 bg-ink-950/60 p-2 font-mono text-2xs leading-relaxed text-slate-300"><code>{{ s.input }}</code></pre>
          </div>
          <div v-if="s.output">
            <p class="text-[10px] uppercase tracking-wide text-slate-500">出参</p>
            <pre class="max-h-48 overflow-auto rounded-control border border-ink-800 bg-ink-950/60 p-2 font-mono text-2xs leading-relaxed text-slate-300"><code>{{ s.output }}</code></pre>
          </div>
        </div>
      </div>

      <!-- 生成态（流式进行中、已有内容时合成显示） -->
      <div
        v-if="isLive && streaming"
        class="flex items-center gap-2 border-l border-ink-700 py-1.5 pl-3 text-slate-400"
      >
        <span class="h-2 w-2 animate-pulse-dot rounded-full bg-accent"></span>
        <Sparkles :size="12" class="text-brand-400" />
        <span>正在生成回答…</span>
      </div>
    </div>
  </div>
</template>
