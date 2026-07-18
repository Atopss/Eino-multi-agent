<script setup lang="ts">
import { computed } from 'vue'
import { Bot, User, FileSearch, Wrench, AlertTriangle, Image, FileText, File } from 'lucide-vue-next'
import type { ChatMessage } from '../types/api'
import { useChatStore } from '../stores/chat'
import MarkdownView from './MarkdownView.vue'
import OrchestrationTimeline from './OrchestrationTimeline.vue'
import ProcessTimeline from './ProcessTimeline.vue'
import PlanCard from './PlanCard.vue'
import DiffView from './DiffView.vue'

const chat = useChatStore()
const props = defineProps<{ message: ChatMessage }>()
const emit = defineEmits<{ (e: 'show-rag'): void }>()

const isUser = computed(() => props.message.role === 'user')
const refCount = computed(() => props.message.ragReferences?.length ?? 0)
const toolCount = computed(() => props.message.toolCalls?.length ?? 0)

// 从消息内容中提取截图 key（screenshot_ready: shot-1 | ...）
interface ScreenshotInfo {
  key: string
  label: string
  fullMatch: string
}
const screenshots = computed<ScreenshotInfo[]>(() => {
  const content = props.message.content || ''
  const re = /screenshot_ready:\s*(shot-\d+)\s*\|\s*([^\n]*)/g
  const results: ScreenshotInfo[] = []
  let m: RegExpExecArray | null
  while ((m = re.exec(content)) !== null) {
    results.push({ key: m[1], label: m[2].trim(), fullMatch: m[0] })
  }
  return results
})

// 移除 screenshot_ready 行后的纯文本（用于 Markdown 渲染）
const cleanContent = computed(() => {
  let content = props.message.content || ''
  content = content.replace(/screenshot_ready:\s*shot-\d+\s*\|[^\n]*\n?/g, '')
  return content.trim()
})

// 从消息内容中提取 ```diff ... ``` 代码块
const diffBlocks = computed(() => {
  const content = props.message.content || ''
  const re = /```diff\s*\n([\s\S]*?)```/g
  const blocks: string[] = []
  let m: RegExpExecArray | null
  while ((m = re.exec(content)) !== null) {
    blocks.push(m[1].trim())
  }
  return blocks
})

// 移除 diff 代码块后的内容
const contentWithoutDiff = computed(() => {
  let content = props.message.content || ''
  content = content.replace(/screenshot_ready:\s*shot-\d+\s*\|[^\n]*\n?/g, '')
  content = content.replace(/```diff\s*\n[\s\S]*?```/g, '')
  return content.trim()
})

const modeLabel: Record<string, string> = {
  ask: 'Ask',
  craft: 'Craft',
  plan: 'Plan',
  balanced: '学习问答',
  strict: '严格资料',
}
</script>

<template>
  <div
    class="flex w-full gap-3 animate-fade-in"
    :class="isUser ? 'flex-row-reverse' : 'flex-row'"
  >
    <!-- 头像 -->
    <div
      class="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-control border"
      :class="
        isUser
          ? 'border-brand/40 bg-brand/15 text-brand-400'
          : 'border-ink-700 bg-ink-800 text-accent'
      "
    >
      <User v-if="isUser" :size="17" />
      <Bot v-else :size="17" />
    </div>

    <!-- 气泡 -->
    <div class="min-w-0 max-w-[82%]" :class="isUser ? 'items-end' : 'items-start'">
      <div
        class="rounded-card border px-4 py-3 text-md leading-relaxed"
        :class="[
            isUser
              ? 'msg-bubble-user border-brand/30 bg-brand/10 text-slate-100 hover:border-brand/50 transition-colors duration-150'
              : 'msg-bubble-assistant border-ink-800 bg-ink-900/70 text-slate-200 hover:border-ink-600 transition-colors duration-150',
          message.error ? 'border-danger/50 bg-danger/10' : '',
        ]"
      >
        <!-- 执行过程时间线：进行中展开、完成后折叠为摘要（气泡内、回答上方） -->
        <ProcessTimeline
          v-if="!isUser"
          :steps="message.steps"
          :streaming="message.streaming"
          class="mb-2"
        />
        <!-- Plan 模式：执行计划展示 -->
        <PlanCard
          v-if="!isUser && (message.plan || message.planStatus)"
          :plan="message.plan || ''"
          :planned-steps="message.plannedSteps"
          :plan-status="message.planStatus"
          :streaming="message.streaming"
          @execute="chat.executePlan()"
          @cancel="message.planStatus = 'cancelled'; message.plan = ''; message.plannedSteps = []"
        />

        <div v-if="message.error" class="mb-1 flex items-center gap-1.5 text-danger">
          <AlertTriangle :size="15" />
          <span class="text-xs font-medium">出错了</span>
        </div>

        <!-- 流式占位：仅有状态提示、尚无内容 -->
        <p
          v-if="message.streaming && !message.content"
          class="flex items-center gap-2 text-sm text-slate-400"
        >
          <span class="h-2 w-2 animate-pulse-dot rounded-full bg-accent"></span>
          {{ message.statusMessage || '思考中…' }}
        </p>

        <template v-else>
          <!-- 用户上传的附件（v2：files 优先，回退 v1：images） -->
          <div
            v-if="isUser && ((message.files?.length) || (message.images?.length))"
            class="mb-2 flex flex-wrap gap-2"
          >
            <!-- v2 files -->
            <template v-if="message.files?.length">
              <div
                v-for="(f, idx) in message.files"
                :key="'f'+idx"
                class="overflow-hidden rounded-control border border-ink-700"
              >
                <!-- 图片 -->
                <template v-if="f.kind === 'image'">
                  <img
                    :src="f.data"
                    :alt="f.name"
                    class="max-h-48 max-w-full object-contain"
                    loading="lazy"
                  />
                </template>
                <!-- 文本 / 二进制文件卡片 -->
                <template v-else>
                  <div class="flex items-center gap-1.5 px-3 py-2 bg-ink-800">
                    <FileText v-if="f.kind === 'text'" :size="15" class="shrink-0 text-slate-400" />
                    <File v-else :size="15" class="shrink-0 text-slate-500" />
                    <span class="text-xs text-slate-300">{{ f.name }}</span>
                  </div>
                </template>
              </div>
            </template>
            <!-- v1 回退：只有 images 没有 files -->
            <template v-else-if="message.images?.length">
              <div
                v-for="(img, idx) in message.images"
                :key="'i'+idx"
                class="overflow-hidden rounded-control border border-ink-700"
              >
                <img
                  :src="img.data"
                  :alt="img.name"
                  class="max-h-48 max-w-full object-contain"
                  loading="lazy"
                />
              </div>
            </template>
          </div>

          <!-- 截图预览（Computer Use 模式） -->
          <div
            v-if="!isUser && screenshots.length"
            class="mb-2 space-y-2"
          >
            <div
              v-for="shot in screenshots"
              :key="shot.key"
              class="overflow-hidden rounded-lg border border-ink-700"
            >
              <div class="flex items-center gap-1.5 bg-ink-800 px-3 py-1.5 text-xs text-slate-400">
                <Image :size="12" />
                <span>屏幕截图</span>
                <span class="text-slate-600">{{ shot.label }}</span>
              </div>
              <img
                :src="`/api/screenshot/${shot.key}`"
                :alt="`屏幕截图 ${shot.key}`"
                class="w-full max-w-[720px]"
                loading="lazy"
              />
            </div>
          </div>

          <!-- Diff 代码块（从消息中提取） -->
          <div v-if="!isUser && diffBlocks.length" class="space-y-2">
            <DiffView
              v-for="(block, idx) in diffBlocks"
              :key="idx"
              :content="block"
            />
          </div>

          <MarkdownView :source="diffBlocks.length ? contentWithoutDiff : cleanContent" />
        </template>

        <!-- 流式光标 -->
        <span
          v-if="message.streaming && message.content"
          class="ml-0.5 inline-block h-4 w-[2px] translate-y-0.5 animate-pulse-dot bg-brand-400 align-middle"
        ></span>
      </div>

      <OrchestrationTimeline
        v-if="!isUser && (message.orchestrationPlan?.length || message.orchestration?.length)"
        :topology="message.topology || 'supervisor'"
        :plan="message.orchestrationPlan"
        :steps="message.orchestration"
        class="mt-2"
      />

      <!-- 助手脚注：模式 + 引用/工具 -->
      <div
        v-if="!isUser && !message.streaming && (message.answerMode || refCount || toolCount)"
        class="mt-1.5 flex flex-wrap items-center gap-1.5 pl-1"
      >
        <span v-if="message.answerMode" class="chip">
          {{ modeLabel[message.answerMode] || message.answerMode }}
        </span>
        <button
          v-if="refCount"
          class="chip cursor-pointer hover:border-accent/60 hover:text-white"
          @click="emit('show-rag')"
        >
          <FileSearch :size="12" />
          引用 {{ refCount }}
        </button>
        <span v-if="toolCount" class="chip">
          <Wrench :size="12" />
          工具 {{ toolCount }}
        </span>
      </div>
    </div>
  </div>
</template>
