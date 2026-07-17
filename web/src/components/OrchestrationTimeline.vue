<script setup lang="ts">
import { computed } from 'vue'
import { Repeat, Sparkles } from 'lucide-vue-next'
import type { SubTaskInfo, OrchestrationStep } from '../types/api'

const props = defineProps<{
  topology: string
  plan?: SubTaskInfo[]
  steps?: OrchestrationStep[]
}>()

const hasContent = computed(
  () => (props.plan?.length ?? 0) > 0 || (props.steps?.length ?? 0) > 0,
)
</script>

<template>
  <div
    v-if="hasContent"
    class="mt-2 rounded-card border border-ink-800 bg-ink-900/50 p-3 text-xs"
  >
    <div class="mb-2 flex items-center gap-1.5 text-slate-300">
      <Repeat :size="14" class="text-brand-400" />
      <span class="font-semibold">多智能体协作</span>
    </div>

    <!-- 规划拆解 -->
    <div v-if="plan && plan.length" class="mb-3">
      <p class="mb-1 text-2xs uppercase tracking-wide text-slate-500">规划拆解</p>
      <div class="flex flex-col gap-1">
        <div
          v-for="(t, i) in plan"
          :key="i"
          class="flex items-center gap-2 rounded-control bg-ink-800/60 px-2 py-1.5"
        >
          <span class="rounded bg-accent/20 px-1.5 py-0.5 text-[10px] text-accent">
            {{ t.agent }}
          </span>
          <span class="text-slate-300">{{ t.task }}</span>
        </div>
      </div>
    </div>

    <!-- 实时时间线 -->
    <div v-if="steps && steps.length" class="flex flex-col">
      <div
        v-for="(s, i) in steps"
        :key="i"
        class="flex items-start gap-2 border-l border-ink-700 py-1.5 pl-3"
        :class="i === steps.length - 1 ? 'border-l-transparent' : ''"
      >
        <span
          class="mt-1 h-2 w-2 shrink-0 rounded-full"
          :class="
            s.phase === 'synthesize'
              ? 'bg-brand-400'
              : s.status === 'end'
                ? 'bg-brand-400'
                : 'bg-accent animate-pulse-dot'
          "
        ></span>
        <div class="min-w-0">
          <p class="text-slate-200">
            <template v-if="s.agent">
              <span class="font-medium text-brand-400">{{ s.agent }}</span>
              <span class="text-slate-500"> · </span>
            </template>
            <span class="text-slate-400">{{ s.message || s.subTask || s.phase }}</span>
          </p>
        </div>
      </div>
    </div>

    <div
      v-if="(!plan || !plan.length) && (!steps || !steps.length)"
      class="flex items-center gap-1.5 text-slate-500"
    >
      <Sparkles :size="12" />
      编排进行中…
    </div>
  </div>
</template>
