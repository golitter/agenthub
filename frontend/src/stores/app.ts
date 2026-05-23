import { create } from 'zustand'

interface AppState {
  count: number
  increment: () => void
}

export const useStore = create<AppState>((set) => ({
  count: 0,
  increment: () => set((s) => ({ count: s.count + 1 })),
}))
