<script setup lang="ts">
import { X } from 'lucide-vue-next'

withDefaults(
  defineProps<{
    open: boolean
    title?: string
    closeOnMask?: boolean
  }>(),
  { title: '', closeOnMask: true },
)
const emit = defineEmits<{ (e: 'close'): void }>()
</script>

<template>
  <teleport to="body">
    <transition name="fade">
      <div v-if="open" class="fixed inset-0 z-50 flex items-center justify-center p-4">
        <div class="absolute inset-0 bg-black/55 backdrop-blur-sm" @click="closeOnMask && emit('close')" />
        <div
          class="relative flex max-h-[85vh] w-full max-w-lg flex-col overflow-hidden rounded-modal border border-ink-800 bg-ink-950 shadow-panel"
        >
          <header v-if="title || $slots.header" class="flex h-14 shrink-0 items-center justify-between border-b border-ink-800 px-4">
            <slot name="header">
              <h3 class="text-sm font-semibold text-white">{{ title }}</h3>
            </slot>
            <button class="btn-ghost !p-1.5" aria-label="关闭" @click="emit('close')">
              <X :size="18" />
            </button>
          </header>
          <div class="min-h-0 flex-1 overflow-y-auto p-4">
            <slot />
          </div>
          <footer v-if="$slots.footer" class="shrink-0 border-t border-ink-800 px-4 py-3">
            <slot name="footer" />
          </footer>
        </div>
      </div>
    </transition>
  </teleport>
</template>

<style scoped>
.fade-enter-active {
  transition: opacity 0.18s ease, transform 0.2s ease-out;
}
.fade-leave-active {
  transition: opacity 0.12s ease, transform 0.15s ease-in;
}
.fade-enter-from {
  opacity: 0;
  transform: scale(0.96) translateY(4px);
}
.fade-leave-to {
  opacity: 0;
  transform: scale(0.97);
}
</style>
