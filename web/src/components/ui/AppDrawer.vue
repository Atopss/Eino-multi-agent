<script setup lang="ts">
import { X } from 'lucide-vue-next'

withDefaults(
  defineProps<{
    open: boolean
    title?: string
    side?: 'right' | 'left'
    closeOnMask?: boolean
  }>(),
  { title: '', side: 'right', closeOnMask: true },
)
const emit = defineEmits<{ (e: 'close'): void }>()
</script>

<template>
  <teleport to="body">
    <transition name="drawer">
      <div v-if="open" class="fixed inset-0 z-40 flex" :class="side === 'right' ? 'justify-end' : 'justify-start'">
        <div class="absolute inset-0 bg-black/55 backdrop-blur-sm" @click="closeOnMask && emit('close')" />
        <div
          class="relative flex h-full w-full max-w-xl flex-col border-ink-800 bg-ink-950 shadow-panel"
          :class="side === 'right' ? 'border-l' : 'border-r'"
        >
          <header class="flex h-14 shrink-0 items-center justify-between border-b border-ink-800 px-4">
            <h2 class="text-sm font-semibold text-white">{{ title }}</h2>
            <button class="btn-ghost !p-1.5" aria-label="关闭" @click="emit('close')">
              <X :size="18" />
            </button>
          </header>
          <div class="min-h-0 flex-1 overflow-y-auto">
            <slot />
          </div>
        </div>
      </div>
    </transition>
  </teleport>
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
