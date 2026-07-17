<script setup lang="ts">
import { Wrench, Terminal } from 'lucide-vue-next'
import type { ToolCallTrace } from '../types/api'

defineProps<{ call: ToolCallTrace }>()
</script>

<template>
  <div class="panel rounded-lg p-3">
    <div class="flex items-center gap-2">
      <Wrench :size="14" class="shrink-0 text-warn" />
      <span class="min-w-0 flex-1 truncate text-sm font-medium text-slate-100">{{ call.name }}</span>
    </div>

    <details v-if="call.arguments" class="mt-2 group">
      <summary class="flex cursor-pointer list-none items-center gap-1 text-2xs text-slate-400 hover:text-slate-200">
        <Terminal :size="12" /> 入参
      </summary>
      <pre class="mt-1 max-h-44 overflow-auto rounded bg-ink-950 p-2 font-mono text-2xs leading-relaxed text-slate-300">{{ call.arguments }}</pre>
    </details>

    <details v-if="call.result" class="mt-1 group">
      <summary class="flex cursor-pointer list-none items-center gap-1 text-2xs text-slate-400 hover:text-slate-200">
        <Terminal :size="12" /> 结果
      </summary>
      <pre class="mt-1 max-h-44 overflow-auto rounded bg-ink-950 p-2 font-mono text-2xs leading-relaxed text-slate-300">{{ call.result }}</pre>
    </details>
  </div>
</template>
