<script setup lang="ts">
import { ref, watch, onMounted, nextTick } from 'vue'
import hljs from 'highlight.js/lib/common'
import { renderMarkdown } from '../utils/markdown'

const props = defineProps<{ source: string }>()
const host = ref<HTMLElement | null>(null)
let scheduled = false

function render() {
  if (!host.value) return
  host.value.innerHTML = renderMarkdown(props.source)
  host.value.querySelectorAll('pre code').forEach((el) => {
    try {
      hljs.highlightElement(el as HTMLElement)
    } catch {
      /* ignore */
    }
  })
}

// 节流：流式回答时 delta 高频触发 source 变化，合并到下一动画帧只渲染一次，
// 避免长回答下每次增量都全量重渲染+高亮导致 CPU 线性增长、界面卡顿。
function scheduleRender() {
  if (scheduled) return
  scheduled = true
  requestAnimationFrame(() => {
    scheduled = false
    nextTick(render)
  })
}

onMounted(render)
watch(
  () => props.source,
  () => scheduleRender(),
)
</script>

<template>
  <div ref="host" class="md"></div>
</template>
