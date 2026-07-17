<script setup lang="ts">
import { ref } from 'vue'
import { Upload, FolderSearch, Search, Play, Folder } from 'lucide-vue-next'
import { useWorkspaceStore } from '../../stores/workspace'
import { api } from '../../api/client'
import RagReferenceItem from '../RagReferenceItem.vue'
import type { RagSearchResult, DirEntry } from '../../types/api'

const props = defineProps<{ mode: 'normal' | 'advanced' }>()
const ws = useWorkspaceStore()
const busy = ref(false)

// uploadId 白名单：仅允许字母、数字、. _ - ，防止路径穿越/资源覆盖
const ID_WHITELIST = /^[a-zA-Z0-9._-]+$/
const MAX_FILE_BYTES = 20 * 1024 * 1024
const ACCEPTED_EXT = '.txt,.md,.json,.csv,.xml,.html,.log,.docx,.pdf,.xlsx,.xls,.pptx,.ppt'

const uploadId = ref('')
const uploadText = ref('')
const uploadFile = ref<HTMLInputElement | null>(null)
const scanPath = ref('.')
const searchQuery = ref('')
const searchTopK = ref(5)
const searchResults = ref<RagSearchResult[]>([])
const browseOpen = ref(false)
const browseDirs = ref<DirEntry[]>([])
const browseCurrent = ref('.')

async function doUploadText() {
  const id = uploadId.value.trim()
  if (!ID_WHITELIST.test(id)) {
    ws.showToast('error', '资料标识仅允许字母、数字、. _ - 字符')
    return
  }
  if (!uploadText.value) return
  busy.value = true
  try {
    await api.ragUpload(id, uploadText.value)
    ws.showToast('success', '文本已上传并建立索引')
    uploadId.value = ''
    uploadText.value = ''
    await ws.loadRagStatus()
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}

async function onFilePicked(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (!f) return
  if (f.size > MAX_FILE_BYTES) {
    ws.showToast('error', `文件过大，上限 ${MAX_FILE_BYTES / 1024 / 1024}MB`)
    ;(e.target as HTMLInputElement).value = ''
    return
  }
  busy.value = true
  try {
    await api.ragUploadFile(f)
    ws.showToast('success', '文件已上传并索引：' + f.name)
    await ws.loadRagStatus()
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
    ;(e.target as HTMLInputElement).value = ''
  }
}

async function doScan() {
  if (!scanPath.value) return
  busy.value = true
  try {
    const r = (await api.ragScan(scanPath.value)) as { sourceCount?: number }
    ws.showToast('success', '扫描完成，当前 ' + (r.sourceCount ?? 0) + ' 个来源')
    await ws.loadRagStatus()
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}

async function doSearch() {
  if (!searchQuery.value) return
  busy.value = true
  try {
    const r = await api.ragSearch({
      query: searchQuery.value,
      topK: searchTopK.value,
    })
    searchResults.value = r.results
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}

async function testEmbedding() {
  busy.value = true
  try {
    const r = (await api.ragTestEmbedding()) as { message?: string; success?: boolean }
    ws.showToast(r.success ? 'success' : 'error', r.message || 'Embedding 测试完成')
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  } finally {
    busy.value = false
  }
}

async function openBrowse() {
  browseOpen.value = true
  await browseTo('.')
}
async function browseTo(path: string) {
  try {
    const r = await api.browse(path)
    browseCurrent.value = r.current
    browseDirs.value = r.dirs
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="grid grid-cols-3 gap-2">
      <div class="panel rounded-lg p-3">
        <p class="text-2xs text-slate-500">资料块</p>
        <p class="mt-0.5 text-lg font-semibold text-white">{{ ws.ragStatus?.chunkCount ?? 0 }}</p>
      </div>
      <div class="panel rounded-lg p-3">
        <p class="text-2xs text-slate-500">来源文件</p>
        <p class="mt-0.5 text-lg font-semibold text-white">{{ ws.ragStatus?.sourceCount ?? 0 }}</p>
      </div>
      <div class="panel rounded-lg p-3">
        <p class="text-2xs text-slate-500">状态</p>
        <p
          class="mt-0.5 text-sm font-semibold"
          :class="ws.ragStatus?.initialized ? 'text-brand-400' : 'text-danger'"
        >
          {{ ws.ragStatus?.initialized ? '已启用' : '未启用' }}
        </p>
      </div>
    </div>

    <p v-if="props.mode === 'normal'" class="text-2xs text-slate-500">
      只需上传资料即可开始。扫描整个目录、检索测试等导入方式，在右上角切到「高级」后可见。
    </p>

    <section class="panel p-3">
      <h3 class="mb-2 flex items-center gap-1.5 text-sm font-medium text-slate-200">
        <Upload :size="14" /> 上传资料
      </h3>
      <input v-model="uploadId" placeholder="资料标识（如 readme.md）" class="input mb-2" />
      <textarea v-model="uploadText" rows="4" placeholder="粘贴文本内容…" class="input mb-2 resize-y"></textarea>
      <div class="flex gap-2">
        <button class="btn-primary flex-1" :disabled="busy || !uploadId || !uploadText" @click="doUploadText">
          上传文本
        </button>
        <button class="btn-outline" @click="uploadFile?.click()">选择文件</button>
        <input ref="uploadFile" type="file" :accept="ACCEPTED_EXT" class="hidden" @change="onFilePicked" />
      </div>
    </section>

    <section v-if="props.mode === 'advanced'" class="panel rounded-xl p-3">
      <h3 class="mb-2 flex items-center gap-1.5 text-sm font-medium text-slate-200">
        <FolderSearch :size="14" /> 扫描目录
      </h3>
      <div class="flex gap-2">
        <input v-model="scanPath" placeholder="目录路径" class="input" />
        <button class="btn-outline" @click="openBrowse">浏览</button>
        <button class="btn-primary" :disabled="busy || !scanPath" @click="doScan">扫描</button>
      </div>
      <div v-if="browseOpen" class="mt-2 rounded-lg border border-ink-800 bg-ink-900/60 p-2">
        <p class="mb-1 flex items-center gap-1 px-1 text-2xs text-slate-500">
          <Folder :size="12" /> {{ browseCurrent }}
        </p>
        <div class="max-h-40 space-y-0.5 overflow-y-auto">
          <button
            v-for="d in browseDirs"
            :key="d.path"
            class="flex w-full items-center gap-1.5 rounded px-2 py-1 text-left text-sm text-slate-300 hover:bg-ink-800"
            @click="browseTo(d.path)"
          >
            <Folder :size="13" class="text-slate-500" /> {{ d.name }}
          </button>
        </div>
        <button class="btn-ghost mt-1 w-full !py-1 text-xs" @click="scanPath = browseCurrent; browseOpen = false">
          选择此目录
        </button>
      </div>
    </section>

    <section v-if="props.mode === 'advanced'" class="panel rounded-xl p-3">
      <h3 class="mb-2 flex items-center gap-1.5 text-sm font-medium text-slate-200">
        <Search :size="14" /> 检索测试
      </h3>
      <div class="flex gap-2">
        <input v-model="searchQuery" placeholder="检索问题" class="input" />
        <input v-model.number="searchTopK" type="number" min="1" max="20" class="input w-18" />
        <button class="btn-primary" :disabled="busy || !searchQuery" @click="doSearch">搜索</button>
      </div>
      <div v-if="searchResults.length" class="mt-3 space-y-2">
        <RagReferenceItem v-for="(r, i) in searchResults" :key="r.id || i" :item="r" />
      </div>
      <button class="btn-ghost mt-3 w-full !py-1.5 text-xs" :disabled="busy" @click="testEmbedding">
        <Play :size="13" /> 测试 Embedding 连通性
      </button>
    </section>
  </div>
</template>
