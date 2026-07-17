<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { Check, XCircle } from 'lucide-vue-next'
import { useWorkspaceStore } from '../../stores/workspace'
import { api } from '../../api/client'

const ws = useWorkspaceStore()
const perms = ref<Array<Record<string, unknown>>>([])

async function loadPerms() {
  try {
    const r = await api.permissionsPending()
    perms.value = r.permissions ?? []
  } catch {
    perms.value = []
  }
}
async function resolvePerm(id: string, decision: string) {
  try {
    await fetch('/api/permissions/resolve', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, decision }),
    })
    await loadPerms()
  } catch (e) {
    ws.showToast('error', (e as Error).message)
  }
}

onMounted(() => {
  loadPerms()
})
</script>

<template>
  <div class="space-y-3">
    <section class="panel space-y-2 p-3">
      <h3 class="text-sm font-medium text-slate-200">待审批的本地命令</h3>
      <p v-if="!perms.length" class="text-xs text-slate-500">暂无待审批项。</p>
      <div v-for="p in perms" :key="(p.id as string)" class="rounded-lg border border-ink-800 p-2">
        <p class="font-mono text-[12px] text-slate-200">{{ p.command }}</p>
        <p class="mt-0.5 text-2xs text-slate-500">{{ p.reason }}</p>
        <div class="mt-1.5 flex gap-2">
          <button class="btn-primary !py-1 text-xs" @click="resolvePerm(p.id as string, 'allow')"><Check :size="13" /> 允许</button>
          <button class="btn-outline !py-1 text-xs hover:!border-danger/50 hover:!text-danger" @click="resolvePerm(p.id as string, 'deny')"><XCircle :size="13" /> 拒绝</button>
        </div>
      </div>
    </section>
  </div>
</template>
