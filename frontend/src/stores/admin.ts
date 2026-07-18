import { create } from 'zustand'

import { setAdminToken as setApiToken } from '@/lib/api'

export type AdminMenuKey =
  'dashboard' | 'sessions' | 'workspaces' | 'agents' | 'services' | 'statistics' | 'users'

interface AdminStore {
  adminToken: string | null
  isAuthenticated: boolean
  showPasswordDialog: boolean
  passwordDialogPurpose: 'login' | 'reauth'
  adminAvatarUrl: string

  setAdminToken: (token: string | null) => void
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

  setAdminToken: (token) => {
    setApiToken(token)
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

export function useAdminAuth() {
  const adminToken = useAdminStore((s) => s.adminToken)
  const isAuthenticated = useAdminStore((s) => s.isAuthenticated)
  const setAdminToken = useAdminStore((s) => s.setAdminToken)
  const logout = useAdminStore((s) => s.logout)
  return { adminToken, isAuthenticated, setAdminToken, logout }
}
