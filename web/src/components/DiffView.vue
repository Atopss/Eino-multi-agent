<script setup lang="ts">
import { computed } from 'vue'
import { Plus, Minus, FileDiff } from 'lucide-vue-next'

const props = defineProps<{
  content: string
}>()

interface DiffHunk {
  header: string
  lines: DiffLine[]
}

interface DiffLine {
  type: 'add' | 'remove' | 'context' | 'header'
  lineNumberOld: number | null
  lineNumberNew: number | null
  content: string
}

const hunks = computed<DiffHunk[]>(() => {
  if (!props.content) return []
  const lines = props.content.split('\n')
  const result: DiffHunk[] = []
  let currentHunk: DiffHunk | null = null
  let oldLine = 0
  let newLine = 0

  for (const line of lines) {
    // @@ -start,count +start,count @@ header
    const hunkMatch = line.match(/^@@\s+-(\d+)(?:,\d+)?\s+\+(\d+)(?:,\d+)?\s+@@/)
    if (hunkMatch) {
      if (currentHunk) result.push(currentHunk)
      oldLine = parseInt(hunkMatch[1], 10)
      newLine = parseInt(hunkMatch[2], 10)
      currentHunk = {
        header: line,
        lines: [{ type: 'header', lineNumberOld: null, lineNumberNew: null, content: line }],
      }
      continue
    }

    if (!currentHunk) {
      // Lines before first hunk (file headers like --- / +++  / diff --git)
      if (!result.length) {
        const firstHunk: DiffHunk = {
          header: '',
          lines: [{ type: 'header', lineNumberOld: null, lineNumberNew: null, content: line }],
        }
        result.push(firstHunk)
        currentHunk = firstHunk
      } else {
        currentHunk = result[result.length - 1]
      }
    }

    if (line.startsWith('+') && !line.startsWith('+++')) {
      currentHunk.lines.push({
        type: 'add',
        lineNumberOld: null,
        lineNumberNew: newLine++,
        content: line.slice(1),
      })
    } else if (line.startsWith('-') && !line.startsWith('---')) {
      currentHunk.lines.push({
        type: 'remove',
        lineNumberOld: oldLine++,
        lineNumberNew: null,
        content: line.slice(1),
      })
    } else if (line.startsWith(' ') || line === '') {
      const ctx = line.startsWith(' ') ? line.slice(1) : ''
      currentHunk.lines.push({
        type: 'context',
        lineNumberOld: oldLine++,
        lineNumberNew: newLine++,
        content: ctx,
      })
    } else if (line.startsWith('---') || line.startsWith('+++') || line.startsWith('diff ')) {
      currentHunk.lines.push({
        type: 'header',
        lineNumberOld: null,
        lineNumberNew: null,
        content: line,
      })
    } else {
      // Unrecognized line, treat as context
      currentHunk.lines.push({
        type: 'context',
        lineNumberOld: oldLine++,
        lineNumberNew: newLine++,
        content: line,
      })
    }
  }

  if (currentHunk) result.push(currentHunk)

  // If there's only a header-hunk with no real changes, return empty
  if (result.length === 1 && result[0].lines.every(l => l.type === 'header')) return []
  if (result.length === 1 && !result[0].lines.some(l => l.type === 'add' || l.type === 'remove')) return []

  return result
})

// If nothing parsed as a diff, don't render
const hasDiff = computed(() => hunks.value.length > 0)

function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
</script>

<template>
  <div v-if="hasDiff" class="my-2 overflow-hidden rounded-card border border-ink-700">
    <!-- Diff 标题栏 -->
    <div class="flex items-center gap-1.5 border-b border-ink-700 bg-ink-800 px-3 py-1.5 text-xs text-slate-400">
      <FileDiff :size="13" />
      <span>文件改动</span>
    </div>

    <!-- Diff 内容 -->
    <div class="max-h-[420px] overflow-auto bg-ink-900 font-mono text-sm leading-tight">
      <table class="w-full table-fixed border-collapse">
        <tbody>
          <template v-for="(hunk, hi) in hunks" :key="hi">
            <tr
              v-for="(line, li) in hunk.lines"
              :key="`${hi}-${li}`"
              class="border-b border-ink-800/50"
            >
              <!-- 行号左（旧） -->
              <td
                class="w-[44px] select-none px-1.5 text-right text-2xs text-slate-600 align-top"
                :class="{
                  'bg-danger/10 text-danger/70': line.type === 'remove',
                  'bg-ink-900': line.type === 'add',
                }"
              >
                {{ line.lineNumberOld ?? '' }}
              </td>
              <!-- 行号右（新） -->
              <td
                class="w-[44px] select-none px-1.5 text-right text-2xs text-slate-600 align-top"
                :class="{
                  'bg-brand/10 text-brand-400': line.type === 'add',
                  'bg-ink-900': line.type === 'remove',
                }"
              >
                {{ line.lineNumberNew ?? '' }}
              </td>
              <!-- 标记 + 内容 -->
              <td class="w-[20px] select-none px-1 text-center align-top">
                <Plus v-if="line.type === 'add'" :size="12" class="inline-block text-brand-400" />
                <Minus v-else-if="line.type === 'remove'" :size="12" class="inline-block text-danger" />
              </td>
              <td
                class="w-full break-all px-2 py-px align-top"
                :class="{
                  'bg-danger/10 text-danger': line.type === 'remove',
                  'bg-brand/10 text-brand-400': line.type === 'add',
                  'text-slate-300': line.type === 'context',
                  'text-slate-400 font-semibold': line.type === 'header',
                }"
              >
                <span v-html="esc(line.content)" />
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </div>
</template>
