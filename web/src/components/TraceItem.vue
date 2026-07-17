<script setup lang="ts">
import { computed } from 'vue'
import { Search, Wrench, Bot, ArrowRight, Info } from 'lucide-vue-next'
import type { ExecutionTraceItem } from '../types/api'

const props = defineProps<{ item: ExecutionTraceItem }>()

const meta = computed(() => {
  const i = props.item
  const parts: string[] = []
  if (i.agent) parts.push(i.agent)
  if (i.target) parts.push('→ ' + i.target)
  if (i.stage) parts.push(i.stage)
  return parts.join(' · ')
})

const icon = computed(() => {
  switch (props.item.type) {
    case 'rag_search':
    case 'rag_result':
      return Search
    case 'tool_call':
    case 'tool_result':
      return Wrench
    case 'agent_start':
    case 'agent_done':
      return Bot
    default:
      return Info
  }
})

const accent = computed(() => {
  switch (props.item.type) {
    case 'rag_search':
    case 'rag_result':
      return 'text-accent border-accent/30'
    case 'tool_call':
    case 'tool_result':
      return 'text-warn border-warn/30'
    case 'agent_start':
    case 'agent_done':
      return 'text-brand-400 border-brand/30'
    default:
      return 'text-slate-300 border-ink-700'
  }
})
</script>

<template>
  <div class="flex items-start gap-2.5 rounded-lg border border-ink-800 bg-ink-900/50 px-3 py-2">
    <component :is="icon" :size="14" class="mt-0.5 shrink-0" :class="accent" />
    <div class="min-w-0 flex-1">
      <p class="text-sm leading-snug text-slate-200">{{ item.message || item.name || item.type }}</p>
      <p v-if="meta" class="mt-0.5 flex flex-wrap items-center gap-1 text-2xs text-slate-500">
        <ArrowRight v-if="item.target" :size="10" class="text-slate-600" />
        {{ meta }}
      </p>
    </div>
  </div>
</template>
