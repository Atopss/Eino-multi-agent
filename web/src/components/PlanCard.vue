<script setup lang="ts">
import { computed } from 'vue'
import { ClipboardList, Loader2, Play, X } from 'lucide-vue-next'

const props = defineProps<{
  plan: string
  plannedSteps?: string[]
  planStatus?: string
  streaming?: boolean
}>()
const emit = defineEmits<{
  (e: 'execute'): void
  (e: 'cancel'): void
}>()

const isGenerating = computed(() => props.planStatus === 'generating')
const isDone = computed(() => props.planStatus === 'done')
const isExecuting = computed(() => props.planStatus === 'executing')
const showActions = computed(() => isDone.value && !props.streaming)
</script>

<template>
  <div class="mb-3 rounded-card border border-accent/30 bg-accent/5 px-4 py-3">
    <div class="mb-2 flex items-center gap-2">
      <ClipboardList :size="16" class="text-accent" />
      <span class="text-sm font-semibold text-accent">执行计划</span>
      <span v-if="isGenerating" class="flex items-center gap-1 text-xs text-slate-400">
        <Loader2 :size="12" class="animate-spin" />
        规划中…
      </span>
      <span v-if="isDone && plannedSteps && plannedSteps.length" class="text-2xs text-slate-500">
        {{ plannedSteps.length }} 步
      </span>
      <span v-if="isExecuting" class="text-xs text-brand-400">执行中…</span>
    </div>

    <!-- 步骤列表 -->
    <ul v-if="!isGenerating && plannedSteps && plannedSteps.length" class="space-y-1.5">
      <li
        v-for="(step, i) in plannedSteps"
        :key="i"
        class="flex items-start gap-2 rounded-md bg-ink-900/50 px-3 py-1.5 text-sm text-slate-200"
      >
        <span class="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-accent/20 text-2xs font-semibold text-accent">
          {{ i + 1 }}
        </span>
        <span>{{ step }}</span>
      </li>
    </ul>

    <!-- 完整计划文本（无解析步骤时回退） -->
    <div
      v-if="!isGenerating && plan && (!plannedSteps || plannedSteps.length === 0)"
      class="mt-2 whitespace-pre-wrap rounded-md bg-ink-900/50 px-3 py-2 text-sm leading-relaxed text-slate-300"
    >
      {{ plan }}
    </div>

    <!-- 生成中占位 -->
    <div v-if="isGenerating" class="flex items-center gap-2 py-2 text-xs text-slate-500">
      <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-accent/60"></span>
      正在根据你的需求生成执行计划...
    </div>

    <!-- 操作按钮 -->
    <div v-if="showActions" class="mt-3 flex items-center gap-2 border-t border-ink-800 pt-3">
      <button
        class="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-sm font-medium text-white transition-all duration-200 hover:bg-accent/90 active:scale-[0.97]"
        @click="emit('execute')"
      >
        <Play :size="14" />
        执行
      </button>
      <button
        class="flex items-center gap-1.5 rounded-lg border border-ink-700 px-3 py-1.5 text-sm text-slate-400 transition-all duration-200 hover:bg-ink-800 hover:text-slate-200 active:scale-[0.97] active:bg-ink-700"
        @click="emit('cancel')"
      >
        <X :size="14" />
        取消
      </button>
    </div>
  </div>
</template>
