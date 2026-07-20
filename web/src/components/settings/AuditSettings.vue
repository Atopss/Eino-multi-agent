<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { api } from '../../api/client'
import type { AuditEntry } from '../../types/api'

const entries = ref<AuditEntry[]>([])
const total = ref(0)
const limit = 50
const offset = ref(0)
const loading = ref(false)
const error = ref<string | null>(null)

const actionMeta: Record<string, { label: string; cls: string }> = {
  login_ok: { label: '登录成功', cls: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30' },
  login_fail: { label: '登录失败', cls: 'bg-red-500/15 text-red-300 border-red-500/30' },
  register_ok: { label: '注册成功', cls: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30' },
  register_fail: { label: '注册失败', cls: 'bg-red-500/15 text-red-300 border-red-500/30' },
  rag_upload: { label: '上传知识', cls: 'bg-indigo-500/15 text-indigo-300 border-indigo-500/30' },
  rag_upload_file: { label: '上传文件', cls: 'bg-indigo-500/15 text-indigo-300 border-indigo-500/30' },
  rag_scan: { label: '扫描知识库', cls: 'bg-indigo-500/15 text-indigo-300 border-indigo-500/30' },
  agent_create: { label: '创建智能体', cls: 'bg-blue-500/15 text-blue-300 border-blue-500/30' },
  agent_update: { label: '更新智能体', cls: 'bg-blue-500/15 text-blue-300 border-blue-500/30' },
  agent_delete: { label: '删除智能体', cls: 'bg-red-500/15 text-red-300 border-red-500/30' },
  skill_add: { label: '新增技能', cls: 'bg-blue-500/15 text-blue-300 border-blue-500/30' },
  skill_delete: { label: '删除技能', cls: 'bg-red-500/15 text-red-300 border-red-500/30' },
  session_delete: { label: '删除会话', cls: 'bg-red-500/15 text-red-300 border-red-500/30' },
  settings_update: { label: '更新设置', cls: 'bg-blue-500/15 text-blue-300 border-blue-500/30' },
  perm_resolve: { label: '审批权限', cls: 'bg-amber-500/15 text-amber-300 border-amber-500/30' },
  backup_create: { label: '创建备份', cls: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30' },
}

function badge(action: string) {
  return actionMeta[action] ?? { label: action, cls: 'bg-slate-500/15 text-slate-300 border-slate-500/30' }
}

const permissionDenied = computed(() => {
  const m = error.value ?? ''
  return m.includes('管理员') || m.toLowerCase().includes('admin') || m.includes('权限')
})

const rangeText = computed(() => {
  if (total.value === 0) return '共 0 条'
  const end = Math.min(offset.value + limit, total.value)
  return `第 ${offset.value + 1}–${end} 条 / 共 ${total.value} 条`
})

async function load() {
  loading.value = true
  error.value = null
  try {
    const res = await api.getAudit(limit, offset.value)
    entries.value = res.entries ?? []
    total.value = res.total ?? 0
  } catch (e) {
    entries.value = []
    total.value = 0
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

function prevPage() {
  if (offset.value <= 0) return
  offset.value = Math.max(0, offset.value - limit)
  load()
}
function nextPage() {
  if (offset.value + limit >= total.value) return
  offset.value += limit
  load()
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-3">
    <div class="flex items-center justify-between">
      <div class="text-xs text-slate-400">操作审计日志（登录 / 知识库 / 智能体 / 权限 / 备份等）</div>
      <button class="btn btn-outline !py-1.5 !text-xs" :disabled="loading" @click="load">
        <span v-if="loading" class="opacity-60">加载中…</span>
        <span v-else>刷新</span>
      </button>
    </div>

    <div
      v-if="permissionDenied"
      class="rounded-control border border-amber-500/30 bg-amber-500/10 px-3 py-3 text-sm text-amber-300"
    >
      无权限查看审计日志，需管理员账号登录。
    </div>
    <div
      v-else-if="error"
      class="rounded-control border border-red-500/30 bg-red-500/10 px-3 py-3 text-sm text-red-300"
    >
      {{ error }}
    </div>

    <div v-else class="panel overflow-hidden">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="border-b border-ink-800 text-left text-xs uppercase tracking-wide text-slate-400">
              <th class="whitespace-nowrap px-3 py-2 font-medium">时间</th>
              <th class="whitespace-nowrap px-3 py-2 font-medium">用户</th>
              <th class="whitespace-nowrap px-3 py-2 font-medium">操作</th>
              <th class="whitespace-nowrap px-3 py-2 font-medium">目标</th>
              <th class="px-3 py-2 font-medium">详情</th>
              <th class="whitespace-nowrap px-3 py-2 font-medium">IP</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="row in entries"
              :key="row.id"
              class="border-b border-ink-800/60 hover:bg-ink-800/40"
            >
              <td class="whitespace-nowrap px-3 py-2 font-mono text-xs text-slate-400">{{ row.ts }}</td>
              <td class="whitespace-nowrap px-3 py-2 text-slate-300">{{ row.userId || '—' }}</td>
              <td class="whitespace-nowrap px-3 py-2">
                <span
                  class="inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium"
                  :class="badge(row.action).cls"
                >{{ badge(row.action).label }}</span>
              </td>
              <td class="whitespace-nowrap px-3 py-2 text-slate-300">{{ row.target || '—' }}</td>
              <td class="max-w-[22rem] truncate px-3 py-2 text-slate-400" :title="row.detail">
                {{ row.detail || '—' }}
              </td>
              <td class="whitespace-nowrap px-3 py-2 font-mono text-xs text-slate-400">{{ row.ip || '—' }}</td>
            </tr>
            <tr v-if="!loading && entries.length === 0">
              <td colspan="6" class="px-3 py-8 text-center text-sm text-slate-500">暂无审计记录</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="flex items-center justify-between border-t border-ink-800 px-3 py-2 text-xs text-slate-400">
        <span>{{ rangeText }}</span>
        <div class="flex items-center gap-2">
          <button class="btn btn-outline !py-1 !text-xs" :disabled="offset <= 0 || loading" @click="prevPage">
            上一页
          </button>
          <button
            class="btn btn-outline !py-1 !text-xs"
            :disabled="offset + limit >= total || loading"
            @click="nextPage"
          >
            下一页
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
