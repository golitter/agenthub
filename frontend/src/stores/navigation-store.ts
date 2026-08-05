/**
 * 聊天选择 store
 *
 * 页面导航由 React Router 负责。本 store 仅保存当前活动的聊天会话，
 * 因为流式状态与会话选择需要共享它。
 */

import { create } from 'zustand'

interface NavigationState {
  currentSessionId: string | null
  setCurrentSession: (sessionId: string) => void
  clearNavigation: () => void
}

export const useNavigationStore = create<NavigationState>((set) => ({
  currentSessionId: null,
  setCurrentSession: (sessionId) => set({ currentSessionId: sessionId }),
  clearNavigation: () => set({ currentSessionId: null }),
}))

export function useChatNav() {
  const currentSessionId = useNavigationStore((state) => state.currentSessionId)
  const setCurrentSession = useNavigationStore((state) => state.setCurrentSession)
  const clearNavigation = useNavigationStore((state) => state.clearNavigation)
  return { currentSessionId, setCurrentSession, clearNavigation }
}
