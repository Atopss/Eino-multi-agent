<script setup lang="ts">
import { computed } from 'vue'
import { Loader2 } from 'lucide-vue-next'

type Variant = 'primary' | 'outline' | 'ghost'
type Size = 'sm' | 'md' | 'lg'

const props = withDefaults(
  defineProps<{
    variant?: Variant
    size?: Size
    loading?: boolean
    disabled?: boolean
    type?: 'button' | 'submit'
    block?: boolean
  }>(),
  { variant: 'primary', size: 'md', loading: false, disabled: false, type: 'button', block: false },
)

const variantCls = computed(() =>
  props.variant === 'primary' ? 'btn-primary' : props.variant === 'outline' ? 'btn-outline' : 'btn-ghost',
)
const sizeCls = computed(() => {
  if (props.size === 'sm') return '!py-1 !px-3 text-xs'
  if (props.size === 'lg') return '!py-3 !px-6 text-sm'
  return ''
})
</script>

<template>
  <button
    :type="type"
    :disabled="disabled || loading"
    :class="[variantCls, sizeCls, block ? 'w-full' : '', disabled || loading ? 'opacity-50 pointer-events-none' : '']"
  >
    <Loader2 v-if="loading" :size="14" class="animate-spin" />
    <slot />
  </button>
</template>
