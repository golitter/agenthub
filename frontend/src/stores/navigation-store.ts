/**
 * Chat selection store
 *
 * Page navigation is owned by React Router. This store only keeps the active
 * chat session because streaming state and conversation selection share it.
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
