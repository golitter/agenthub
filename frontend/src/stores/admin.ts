import { create } from 'zustand'

import { onAdminUnauthorized, restoreAdminToken, setAdminToken as setApiToken } from '@/lib/api'

export type AdminMenuKey =
  'dashboard' | 'sessions' | 'workspaces' | 'agents' | 'services' | 'statistics' | 'users'

interface AdminStore {
  adminToken: string | null
  isAuthenticated: boolean
  showPasswordDialog: boolean
  passwordDialogPurpose: 'login' | 'reauth'
  adminAvatarUrl: string

  setAdminToken: (token: string | null, expiresInSeconds?: number) => void
  setIsAuthenticated: (val: boolean) => void
  showLoginDialog: () => void
  showReauthDialog: () => void
  hidePasswordDialog: () => void
  logout: () => void
  setAdminAvatarUrl: (url: string) => void
}

export const useAdminStore = create<AdminStore>((set) => ({
  adminToken: null,
  isAuthenticated: false,
  showPasswordDialog: false,
  passwordDialogPurpose: 'login',
  adminAvatarUrl: 'https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede',

  setAdminToken: (token, expiresInSeconds) => {
    setApiToken(token, expiresInSeconds)
    set({ adminToken: token, isAuthenticated: !!token })
  },
  setIsAuthenticated: (val) => set({ isAuthenticated: val }),
  showLoginDialog: () => set({ showPasswordDialog: true, passwordDialogPurpose: 'login' }),
  showReauthDialog: () => set({ showPasswordDialog: true, passwordDialogPurpose: 'reauth' }),
  hidePasswordDialog: () => set({ showPasswordDialog: false }),
  logout: () => {
    setApiToken(null)
    set({ adminToken: null, isAuthenticated: false })
  },
  setAdminAvatarUrl: (url) => set({ adminAvatarUrl: url }),
}))

// 启动时尝试从 sessionStorage 恢复 admin token（页面刷新场景）。
// 仅在缓存有效时恢复认证态，避免刷新后强制重新输密码。
const restoredToken = restoreAdminToken()
if (restoredToken) {
  useAdminStore.setState({ adminToken: restoredToken, isAuthenticated: true })
}

// 保存 unsubscribe 句柄，避免 HMR 下重复注册同一回调。
// （模块重新求值时会再次执行本语句，旧回调若未清理会随每次热更新累积。）
const _unsubAdminUnauthorized = onAdminUnauthorized(() => {
  const state = useAdminStore.getState()
  if (state.adminToken || state.isAuthenticated) state.logout()
})
// HMR 热替换时清理上一份注册，开发态下保持监听集合单一。
if (import.meta.hot) {
  import.meta.hot.dispose(() => _unsubAdminUnauthorized())
}

export function useAdminAuth() {
  const adminToken = useAdminStore((s) => s.adminToken)
  const isAuthenticated = useAdminStore((s) => s.isAuthenticated)
  const setAdminToken = useAdminStore((s) => s.setAdminToken)
  const logout = useAdminStore((s) => s.logout)
  return { adminToken, isAuthenticated, setAdminToken, logout }
}
