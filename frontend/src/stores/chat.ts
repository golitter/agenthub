import { create } from 'zustand'

interface ChatNavState {
  currentSessionId: string | null
  setCurrentSession: (sessionId: string) => void
  clearNavigation: () => void
}

export const useChatNav = create<ChatNavState>((set) => ({
  currentSessionId: null,
  setCurrentSession: (sessionId) => set({ currentSessionId: sessionId }),
  clearNavigation: () => set({ currentSessionId: null }),
}))
