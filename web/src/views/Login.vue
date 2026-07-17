<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { User, Lock, Eye, EyeOff, Loader2, Bot, ShieldCheck } from 'lucide-vue-next'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()

const username = ref('')
const password = ref('')
const showPwd = ref(false)
const loading = ref(false)
const error = ref('')

async function onSubmit() {
  if (loading.value) return
  const u = username.value.trim()
  const p = password.value
  if (!u || !p) {
    error.value = '请输入账号和密码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await auth.login(u, p)
    router.push('/')
  } catch (e) {
    const msg = (e as Error).message || ''
    error.value = msg.includes('unauthorized') || msg.includes('invalid') ? '账号或密码错误' : '登录失败，请稍后重试'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-bg relative flex min-h-screen w-screen items-center justify-center overflow-hidden px-4">
    <!-- 动态光斑背景 -->
    <div class="blob blob-a"></div>
    <div class="blob blob-b"></div>
    <div class="blob blob-c"></div>

    <!-- 玻璃卡片 -->
    <div class="login-card relative z-10 w-full max-w-[400px] rounded-modal border border-white/10 bg-white/[0.06] p-8 shadow-2xl backdrop-blur-xl">
      <!-- 品牌区 -->
      <div class="mb-7 flex flex-col items-center text-center">
        <div class="mb-3 flex h-12 w-12 items-center justify-center rounded-card bg-gradient-to-br from-brand-400 to-brand-600 text-white shadow-glow">
          <Bot :size="24" />
        </div>
        <h1 class="text-xl font-semibold text-white">Eino 智能体工作台</h1>
        <p class="mt-1 text-sm text-slate-400">你的多智能体 · 知识库平台</p>
      </div>

      <!-- 表单 -->
      <form class="space-y-4" @submit.prevent="onSubmit">
        <label class="block">
          <span class="mb-1.5 block text-xs font-medium text-slate-300">账号</span>
          <div class="input-wrap">
            <User :size="16" class="input-icon" />
            <input
              v-model="username"
              type="text"
              autocomplete="username"
              placeholder="请输入账号"
              class="login-input"
              @input="error = ''"
            />
          </div>
        </label>

        <label class="block">
          <span class="mb-1.5 block text-xs font-medium text-slate-300">密码</span>
          <div class="input-wrap">
            <Lock :size="16" class="input-icon" />
            <input
              v-model="password"
              :type="showPwd ? 'text' : 'password'"
              autocomplete="current-password"
              placeholder="请输入密码"
              class="login-input pr-10"
              @input="error = ''"
              @keyup.enter="onSubmit"
            />
            <button
              type="button"
              class="pwd-toggle"
              :aria-label="showPwd ? '隐藏密码' : '显示密码'"
              @click="showPwd = !showPwd"
            >
              <Eye v-if="!showPwd" :size="16" />
              <EyeOff v-else :size="16" />
            </button>
          </div>
        </label>

        <!-- 错误提示 -->
        <transition name="err">
          <div v-if="error" class="rounded-control border border-danger/40 bg-danger/10 px-3 py-2 text-sm text-danger">
            {{ error }}
          </div>
        </transition>

        <button
          type="submit"
          class="login-btn w-full"
          :class="{ 'is-loading': loading }"
          :disabled="loading"
        >
          <Loader2 v-if="loading" :size="16" class="animate-spin" />
          <span>{{ loading ? '登录中…' : '登 录' }}</span>
        </button>

        <p class="flex items-center justify-center gap-1.5 text-center text-2xs text-slate-500">
          <ShieldCheck :size="13" />
          本地部署 · 数据自持 · 首次启动自动创建管理员账号
        </p>
      </form>
    </div>

    <p class="absolute bottom-4 z-10 text-center text-2xs text-slate-600">
      © 2026 Eino · 生产级多智能体工作台
    </p>
  </div>
</template>

<style scoped>
.login-bg {
  background:
    radial-gradient(120% 120% at 50% 0%, #1a2138 0%, #0b1020 55%, #070a14 100%);
  font-family: 'Poppins', 'PingFang SC', system-ui, sans-serif;
}
.login-bg::before {
  content: '';
  position: absolute;
  inset: 0;
  background-image:
    radial-gradient(40% 30% at 20% 20%, rgba(99, 102, 241, 0.18), transparent 60%),
    radial-gradient(35% 30% at 85% 30%, rgba(124, 127, 233, 0.12), transparent 60%);
  pointer-events: none;
}
.blob {
  position: absolute;
  border-radius: 9999px;
  filter: blur(70px);
  opacity: 0.5;
  animation: float 14s ease-in-out infinite;
}
.blob-a {
  width: 340px; height: 340px;
  top: -80px; left: -60px;
  background: radial-gradient(circle, #6366f1, transparent 70%);
}
.blob-b {
  width: 300px; height: 300px;
  bottom: -90px; right: -40px;
  background: radial-gradient(circle, #7C7FE9, transparent 70%);
  animation-delay: -5s;
}
.blob-c {
  width: 260px; height: 260px;
  top: 40%; left: 60%;
  background: radial-gradient(circle, #9194F0, transparent 70%);
  animation-delay: -9s;
}
@keyframes float {
  0%, 100% { transform: translate(0, 0) scale(1); }
  50% { transform: translate(20px, -30px) scale(1.08); }
}

.input-wrap {
  position: relative;
  display: flex;
  align-items: center;
}
.input-icon {
  position: absolute;
  left: 12px;
  color: #64748b;
  pointer-events: none;
}
.login-input {
  width: 100%;
  border-radius: 0.75rem;
  border: 1px solid rgba(255, 255, 255, 0.12);
  background: rgba(15, 23, 42, 0.6);
  padding: 0.65rem 0.9rem 0.65rem 2.4rem;
  color: #f8fafc;
  font-size: 0.875rem;
  outline: none;
  transition: border-color 0.18s ease, box-shadow 0.18s ease;
}
.login-input::placeholder { color: #475569; }
.login-input:focus {
  border-color: #7C7FE9;
  box-shadow: 0 0 0 3px rgba(124, 127, 233, 0.25);
}
.pwd-toggle {
  position: absolute;
  right: 10px;
  display: flex;
  color: #64748b;
  cursor: pointer;
  transition: color 0.15s ease;
}
.pwd-toggle:hover { color: #cbd5e1; }

.login-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border-radius: 0.75rem;
  padding: 0.7rem 1rem;
  color: #fff;
  font-weight: 600;
  font-size: 0.9rem;
  background: linear-gradient(135deg, #6366D2, #9194F0 55%, #7C7FE9);
  box-shadow: 0 8px 24px -8px rgba(99, 102, 241, 0.7);
  cursor: pointer;
  transition: transform 0.15s ease, box-shadow 0.15s ease, opacity 0.15s ease;
}
.login-btn:hover:not(:disabled) {
  transform: translateY(-1px);
  box-shadow: 0 12px 28px -8px rgba(99, 102, 241, 0.85);
}
.login-btn:active:not(:disabled) { transform: translateY(0); }
.login-btn.is-loading { opacity: 0.85; cursor: progress; }

.err-enter-active, .err-leave-active { transition: opacity 0.2s ease, transform 0.2s ease; }
.err-enter-from, .err-leave-to { opacity: 0; transform: translateY(-4px); }
</style>
