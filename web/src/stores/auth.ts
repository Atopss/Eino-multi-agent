import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { request } from '../api/client'

const TOKEN_KEY = 'eino.token'
const USER_KEY = 'eino.user'
const EXP_KEY = 'eino.token.exp'

interface LoginResp {
  token: string
  username: string
  expiresIn: number
}

// 401 等外部登出场景（见 api/client.ts 的 clearAuthAndRedirect）通过事件同步本 store，
// 避免 client.ts 与 auth store 形成循环依赖。
const AUTH_CHANGED = 'eino:auth-changed'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem(TOKEN_KEY) || '')
  const username = ref<string>(localStorage.getItem(USER_KEY) || '')
  const expiresAt = ref<number>(Number(localStorage.getItem(EXP_KEY)) || 0)

  const isAuthenticated = computed(() => {
    if (!token.value) return false
    // expiresAt 为 0 表示后端未下发过期时间（长期有效）
    if (expiresAt.value > 0 && Date.now() >= expiresAt.value) return false
    return true
  })

  function applyLogout() {
    token.value = ''
    username.value = ''
    expiresAt.value = 0
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
    localStorage.removeItem(EXP_KEY)
  }

  function setSession(t: string, u: string, expiresIn = 0) {
    token.value = t
    username.value = u
    expiresAt.value = expiresIn > 0 ? Date.now() + expiresIn * 1000 : 0
    localStorage.setItem(TOKEN_KEY, t)
    localStorage.setItem(USER_KEY, u)
    if (expiresAt.value > 0) localStorage.setItem(EXP_KEY, String(expiresAt.value))
    else localStorage.removeItem(EXP_KEY)
  }

  async function login(user: string, pass: string) {
    const resp = await request<LoginResp>('/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username: user, password: pass }),
    })
    setSession(resp.token, resp.username, resp.expiresIn)
    return resp
  }

  function logout() {
    applyLogout()
  }

  // 监听外部登出（如 401），清空内存态，防止守卫因残留 token 误判已登录。
  if (typeof window !== 'undefined') {
    window.addEventListener(AUTH_CHANGED, applyLogout)
  }

  return { token, username, expiresAt, isAuthenticated, login, logout }
})
