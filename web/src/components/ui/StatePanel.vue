<script setup lang="ts">
import { Sparkles, Loader2, AlertTriangle } from 'lucide-vue-next'

type Kind = 'empty' | 'loading' | 'error'

withDefaults(
  defineProps<{
    kind?: Kind
    title?: string
    description?: string
  }>(),
  { kind: 'empty', title: '', description: '' },
)
</script>

<template>
  <div class="flex flex-col items-center gap-3 px-6 py-14 text-center">
    <div
      class="flex h-12 w-12 items-center justify-center rounded-control"
      :class="{
        'bg-brand/15 text-brand-400': kind === 'empty',
        'bg-ink-800 text-slate-400': kind === 'loading',
        'bg-danger/15 text-danger': kind === 'error',
      }"
    >
      <Loader2 v-if="kind === 'loading'" :size="22" class="animate-spin" />
      <AlertTriangle v-else-if="kind === 'error'" :size="22" />
      <Sparkles v-else :size="22" />
    </div>
    <div>
      <h3 v-if="title" class="text-sm font-semibold text-white">{{ title }}</h3>
      <p v-if="description" class="mt-1 max-w-sm text-xs text-slate-400">{{ description }}</p>
    </div>
    <slot name="action" />
  </div>
</template>
