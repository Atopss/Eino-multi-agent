<script setup lang="ts">
import { Bot, Database, MessagesSquare, ArrowRight } from 'lucide-vue-next'

const emit = defineEmits<{ (e: 'create-agent'): void; (e: 'dismiss'): void }>()

const steps = [
  {
    icon: Bot,
    title: '1 · 建一个智能体',
    desc: '定义它是谁（资深工程师 / 产品经理 / 翻译官…），相当于给助手一个身份。',
  },
  {
    icon: Database,
    title: '2 · 喂点本地资料',
    desc: '在「工作台设置 → 知识库」上传文件，让它的回答有依据、可溯源。',
  },
  {
    icon: MessagesSquare,
    title: '3 · 开聊并看引用',
    desc: '直接提问，右侧面板会实时展示它检索到的资料与执行过程。',
  },
]
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/65 p-4 backdrop-blur-sm">
    <div class="w-full max-w-md rounded-modal border border-ink-800 bg-ink-950 p-6 shadow-panel">
      <div class="mb-1 flex items-center gap-2">
        <div class="flex h-9 w-9 items-center justify-center rounded-control bg-brand/15 text-brand-400">
          <Bot :size="20" />
        </div>
        <h2 class="text-base font-semibold text-white">欢迎使用 Eino 工作台</h2>
      </div>
      <p class="mb-5 text-sm text-slate-400">
        三步即可上手，其余高级功能需要时再探索。
      </p>

      <ul class="space-y-3">
        <li
          v-for="s in steps"
          :key="s.title"
          class="flex gap-3 rounded-card border border-ink-800 bg-ink-900/50 p-3"
        >
          <div class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-ink-800 text-accent">
            <component :is="s.icon" :size="16" />
          </div>
          <div class="min-w-0">
            <p class="text-sm font-medium text-slate-100">{{ s.title }}</p>
            <p class="mt-0.5 text-[12px] leading-relaxed text-slate-400">{{ s.desc }}</p>
          </div>
        </li>
      </ul>

      <div class="mt-6 flex gap-2">
        <button class="btn-primary flex-1" @click="emit('create-agent')">
          创建第一个智能体 <ArrowRight :size="15" class="ml-1 inline" />
        </button>
        <button class="btn-ghost" @click="emit('dismiss')">稍后再说</button>
      </div>
    </div>
  </div>
</template>
