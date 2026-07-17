<script setup lang="ts">
import { ref } from 'vue'
import {
  Folder, FolderOpen, File, FileText, Image, ChevronRight, ChevronDown,
  RefreshCw, Home, Loader2, X, Eye,
} from 'lucide-vue-next'
import { api } from '../api/client'
import type { DirEntry } from '../types/api'

const currentPath = ref('')
const entries = ref<DirEntry[]>([])
const loading = ref(false)
const error = ref<string | null>(null)
const expandedDirs = ref<Set<string>>(new Set())
const history = ref<string[]>([]) // 面包屑导航

// 预览状态
const previewFile = ref<DirEntry | null>(null)
const previewContent = ref<string>('')
const previewLoading = ref(false)

function getFileIcon(entry: DirEntry) {
  if (entry.isDir) return Folder
  const ext = (entry.name || '').toLowerCase()
  if (/\.(png|jpg|jpeg|gif|webp|bmp|svg|ico)$/.test(ext)) return Image
  if (/\.(txt|md|json|xml|html|css|js|ts|py|go|rs|java|c|cpp|vue|yaml|yml|toml|ini|cfg|log|sh|bat|ps1|sql|env)$/.test(ext)) return FileText
  return File
}

async function browse(path: string, addToHistory = true) {
  loading.value = true
  error.value = null
  try {
    const data = await api.browse(path)
    currentPath.value = data.current
    entries.value = data.dirs
    if (addToHistory && history.value[history.value.length - 1] !== data.current) {
      history.value.push(data.current)
    }
  } catch (e) {
    error.value = (e as Error).message
    entries.value = []
  } finally {
    loading.value = false
  }
}

function goTo(path: string) {
  // 修剪面包屑到目标位置
  const idx = history.value.indexOf(path)
  if (idx >= 0) {
    history.value = history.value.slice(0, idx + 1)
  }
  browse(path, false)
}

function enterDir(dir: DirEntry) {
  const fullPath = dir.path || (currentPath.value + '/' + dir.name).replace(/\/+/g, '/')
  browse(fullPath)
}

function goHome() {
  browse('')
}

function toggleDir(dir: DirEntry) {
  const key = dir.path || dir.name
  if (expandedDirs.value.has(key)) {
    expandedDirs.value.delete(key)
    return
  }
  expandedDirs.value.add(key)
  enterDir(dir)
}

async function openPreview(entry: DirEntry) {
  previewFile.value = entry
  previewLoading.value = true
  previewContent.value = ''
  try {
    const resp = await fetch(`/api/file/read?path=${encodeURIComponent(entry.path || currentPath.value + '/' + entry.name)}`, {
      headers: { 'Authorization': 'Bearer ' + localStorage.getItem('eino.token') || '' }
    })
    if (!resp.ok) {
      previewContent.value = `读取失败 (${resp.status})`
    } else {
      const text = await resp.text()
      previewContent.value = text.slice(0, 50000) // 限制 50KB
    }
  } catch (e) {
    previewContent.value = '读取错误：' + (e as Error).message
  } finally {
    previewLoading.value = false
  }
}

function closePreview() {
  previewFile.value = null
  previewContent.value = ''
}

function formatSize(entry: DirEntry) {
  // 后端可能不返回 size，这里留作扩展
  return ''
}

function getFileName(path: string): string {
  const parts = path.replace(/\\/g, '/').split('/')
  return parts[parts.length - 1] || path || '根目录'
}

// 初始化：浏览根目录
browse('')
</script>

<template>
  <div class="flex h-full flex-col">
    <!-- 标题 -->
    <div class="flex items-center justify-between border-b border-ink-800 px-3 py-2">
      <span class="text-xs font-medium uppercase tracking-wide text-slate-400">文件浏览器</span>
      <div class="flex items-center gap-1">
        <button
          class="btn-ghost !p-1"
          title="回到根目录"
          @click="goHome"
        >
          <Home :size="14" />
        </button>
        <button
          class="btn-ghost !p-1"
          title="刷新"
          @click="browse(currentPath)"
        >
          <RefreshCw :size="14" :class="loading ? 'animate-spin' : ''" />
        </button>
      </div>
    </div>

    <!-- 面包屑导航 -->
    <div class="flex items-center gap-0.5 overflow-x-auto border-b border-ink-800 px-2 py-1.5 text-2xs">
      <button
        class="shrink-0 rounded px-1 py-0.5 text-slate-400 hover:bg-ink-800 hover:text-slate-200"
        @click="goHome"
      >/</button>
      <template v-for="(dir, idx) in history" :key="idx">
        <ChevronRight :size="10" class="shrink-0 text-slate-600" />
        <button
          class="shrink-0 truncate rounded px-1 py-0.5 text-slate-400 hover:bg-ink-800 hover:text-slate-200"
          :title="dir"
          @click="goTo(dir)"
        >
          {{ getFileName(dir) || '/' }}
        </button>
      </template>
    </div>

    <!-- 错误提示 -->
    <div v-if="error" class="px-3 py-2 text-2xs text-danger">
      {{ error }}
    </div>

    <!-- 文件列表 -->
    <div class="min-h-0 flex-1 overflow-y-auto">
      <div v-if="loading" class="flex items-center gap-2 px-3 py-4 text-xs text-slate-500">
        <Loader2 :size="14" class="animate-spin" />
        加载中…
      </div>

      <div v-else-if="!entries.length" class="px-3 py-6 text-center text-xs text-slate-600">
        此目录为空
      </div>

      <div v-else class="py-1">
        <button
          v-for="entry in entries"
          :key="entry.name"
          class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm transition-colors hover:bg-ink-900"
          @click="entry.isDir ? toggleDir(entry) : openPreview(entry)"
        >
          <!-- 展开/折叠箭头（仅目录） -->
          <span v-if="entry.isDir" class="shrink-0 text-slate-500">
            <ChevronRight
              v-if="!expandedDirs.has(entry.path || entry.name)"
              :size="12"
            />
            <ChevronDown
              v-else
              :size="12"
            />
          </span>
          <span v-else class="w-3 shrink-0" />

          <!-- 图标 -->
          <component
            :is="entry.isDir
              ? (expandedDirs.has(entry.path || entry.name) ? FolderOpen : Folder)
              : getFileIcon(entry)"
            :size="15"
            class="shrink-0"
            :class="entry.isDir ? 'text-yellow-500' : 'text-slate-400'"
          />

          <!-- 文件名 -->
          <span class="truncate text-slate-200">{{ entry.name }}</span>

          <!-- 预览按钮（文件） -->
          <button
            v-if="!entry.isDir"
            class="ml-auto shrink-0 rounded p-0.5 text-slate-600 opacity-0 hover:bg-ink-800 hover:text-slate-300 group-hover:opacity-100"
            title="预览文件"
            @click.stop="openPreview(entry)"
          >
            <Eye :size="13" />
          </button>
        </button>
      </div>
    </div>

    <!-- 文件预览弹窗 -->
    <Teleport to="body">
      <div
        v-if="previewFile"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-6"
        @click.self="closePreview"
      >
        <div class="flex max-h-[85vh] w-full max-w-3xl flex-col rounded-modal border border-ink-700 bg-ink-950 shadow-2xl" @click.stop>
          <!-- 预览标题栏 -->
          <div class="flex items-center gap-2 border-b border-ink-800 px-4 py-3">
            <component :is="getFileIcon(previewFile)" :size="16" class="text-slate-400" />
            <span class="flex-1 truncate text-sm font-medium text-slate-200">{{ previewFile.name }}</span>
            <button class="btn-ghost !p-1" @click="closePreview">
              <X :size="16" />
            </button>
          </div>

          <!-- 预览内容 -->
          <div class="min-h-0 flex-1 overflow-auto">
            <div v-if="previewLoading" class="flex items-center gap-2 px-4 py-8 text-sm text-slate-500">
              <Loader2 :size="14" class="animate-spin" />
              加载中…
            </div>
            <pre v-else class="p-4 text-sm leading-relaxed text-slate-200 font-mono whitespace-pre-wrap"><code>{{ previewContent }}</code></pre>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>
