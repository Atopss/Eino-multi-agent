<script setup lang="ts">
import { ref, computed } from 'vue'
import { FileText, ChevronDown } from 'lucide-vue-next'
import type { RAGReference } from '../types/api'

const props = defineProps<{ item: RAGReference }>()
const expanded = ref(false)

const scoreClass = computed(() => {
  const s = props.item.score ?? 0
  if (s >= 0.5) return 'bg-brand/15 text-brand-400 border-brand/30'
  if (s >= 0.3) return 'bg-warn/15 text-warn border-warn/30'
  return 'bg-ink-800 text-slate-400 border-ink-700'
})
</script>

<template>
  <div class="panel rounded-lg p-3 transition-colors hover:border-ink-700">
    <div class="flex items-center gap-2">
      <FileText :size="14" class="shrink-0 text-accent" />
      <span class="min-w-0 flex-1 truncate text-sm font-medium text-slate-100">
        {{ item.fileName || item.id }}
      </span>
      <span v-if="item.chunkIndex >= 0" class="shrink-0 text-2xs text-slate-500">
        切片 #{{ item.chunkIndex }}
      </span>
      <span class="shrink-0 rounded border px-1.5 py-0.5 text-2xs font-medium" :class="scoreClass">
        {{ item.score != null ? item.score.toFixed(3) : '-' }}
      </span>
    </div>

    <div
      v-if="item.matchType"
      class="mt-1.5 inline-block rounded bg-ink-800 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-slate-400"
    >
      {{ item.matchType }}
    </div>

    <p
      class="mt-2 whitespace-pre-wrap break-words text-sm leading-relaxed text-slate-300"
      :class="!expanded ? 'line-clamp-4' : ''"
    >
      {{ item.chunk }}
    </p>

    <button
      v-if="item.chunk && item.chunk.length > 120"
      class="mt-1 text-2xs text-accent hover:underline"
      @click="expanded = !expanded"
    >
      <ChevronDown :size="12" class="inline transition-transform" :class="expanded ? 'rotate-180' : ''" />
      {{ expanded ? '收起' : '展开' }}
    </button>
  </div>
</template>
