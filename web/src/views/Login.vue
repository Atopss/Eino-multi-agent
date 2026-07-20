<script setup lang="ts">
import { ref, computed } from 'vue'
import { LogIn, UserPlus, KeyRound, ShieldCheck } from 'lucide-vue-next'
import { api, setToken, getToken } from '../api/client'

// 若已持有令牌，直接进入工作台（避免重复登录）
if (getToken() && location.hash !== '#/login') {
  location.hash = '#/'
}

type Tab = 'login' | 'register'
const tab = ref<Tab>('login')
const username = ref('')
const password = ref('')
const isAdmin = ref(false)
const busy = ref(false)
const error = ref('')
const okMsg = ref('')

const canSubmit = computed(() => !!username.value.trim() && !!password.value.trim())

function switchTab(t: Tab) {
  tab.value = t
  error.value = ''
  okMsg.value = ''
}

async function onSubmit() {
  if (!canSubmit.value || busy.value) return
  busy.value = true
  error.value = ''
  okMsg.value = ''
  try {
    if (tab.value === 'login') {
      const res = await api.login(username.value.trim(), password.value)
      setToken(res.token)
      location.hash = '#/'
    } else {
      await api.register(username.value.trim(), password.value, isAdmin.value)
      okMsg.value = '账号已创建，请切换到登录页进入。'
      tab.value = 'login'
      password.value = ''
    }
  } catch (e) {
    error.value = (e as Error).message || '操作失败'
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="flex min-h-full items-center justify-center p-4">
    <div class="panel w-full max-w-sm space-y-5 p-6">
      <!-- 品牌头 -->
      <div class="space-y-1 text-center">
        <div class="mx-auto flex h-11 w-11 items-center justify-center rounded-xl bg-brand/15 text-brand-400">
          <KeyRound :size="20" />
        </div>
        <h1 class="text-lg font-semibold text-slate-100">Eino</h1>
        <p class="text-xs text-slate-500">登录后开始使用你的模型工作台</p>
      </div>

      <!-- Tab 切换 -->
      <div class="flex rounded-lg border border-ink-800 bg-ink-900/40 p-1 text-sm">
        <button
          class="flex-1 rounded-md py-1.5 font-medium transition-colors"
          :class="tab === 'login' ? 'bg-brand/15 text-brand-300' : 'text-slate-400 hover:text-slate-200'"
          @click="switchTab('login')"
        >
          登录
        </button>
        <button
          class="flex-1 rounded-md py-1.5 font-medium transition-colors"
          :class="tab === 'register' ? 'bg-brand/15 text-brand-300' : 'text-slate-400 hover:text-slate-200'"
          @click="switchTab('register')"
        >
          注册
        </button>
      </div>

      <!-- 表单 -->
      <form class="space-y-3" @submit.prevent="onSubmit">
        <label class="block text-xs text-slate-400">
          用户名
          <input v-model="username" placeholder="请输入用户名" class="input mt-1" autocomplete="username" />
        </label>
        <label class="block text-xs text-slate-400">
          密码
          <input
            v-model="password"
            type="password"
            placeholder="请输入密码"
            class="input mt-1"
            autocomplete="current-password"
          />
        </label>

        <label v-if="tab === 'register'" class="flex items-center gap-2 text-xs text-slate-400">
          <input v-model="isAdmin" type="checkbox" class="h-4 w-4 rounded border-ink-700 bg-ink-900 accent-brand" />
          <ShieldCheck :size="13" class="text-accent" />
          注册为管理员（可创建其他账号）
        </label>

        <p v-if="error" class="text-[11px] text-danger-400">{{ error }}</p>
        <p v-if="okMsg" class="text-[11px] text-emerald-400">{{ okMsg }}</p>

        <button
          type="submit"
          class="btn-primary w-full !py-2.5"
          :disabled="busy || !canSubmit"
        >
          <LogIn v-if="tab === 'login'" :size="15" />
          <UserPlus v-else :size="15" />
          {{ busy ? '处理中…' : tab === 'login' ? '登录' : '创建账号' }}
        </button>
      </form>
    </div>
  </div>
</template>
