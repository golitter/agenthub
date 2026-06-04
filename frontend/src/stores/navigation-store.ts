/**
 * Navigation Store
 *
 * Manages UI navigation state: active tab, current session selection, sidebar routing.
 * Independently importable — `useActiveTab`, `useChatNav` live here.
 */

import { create } from 'zustand'

export type NavTab = 'chat' | 'contacts' | 'skills' | 'admin' | 'settings'

interface NavigationState {
  activeTab: NavTab
  currentSessionId: string | null
  setActiveTab: (tab: NavTab) => void
  setCurrentSession: (sessionId: string) => void
  clearNavigation: () => void
}

export const useNavigationStore = create<NavigationState>((set) => ({
  activeTab: 'chat',
  currentSessionId: null,

  setActiveTab: (tab) => set({ activeTab: tab }),
  setCurrentSession: (sessionId) => set({ currentSessionId: sessionId }),
  clearNavigation: () => set({ currentSessionId: null }),
}))

export function useActiveTab() {
  const activeTab = useNavigationStore((s) => s.activeTab)
  const setActiveTab = useNavigationStore((s) => s.setActiveTab)
  return { activeTab, setActiveTab }
}

export function useChatNav() {
  const currentSessionId = useNavigationStore((s) => s.currentSessionId)
  const setCurrentSession = useNavigationStore((s) => s.setCurrentSession)
  const clearNavigation = useNavigationStore((s) => s.clearNavigation)
  return { currentSessionId, setCurrentSession, clearNavigation }
}
