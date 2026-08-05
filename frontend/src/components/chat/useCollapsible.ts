import { useState } from 'react'

/** 持久化到 localStorage 的可折叠分区状态 Hook。 */
export function useCollapsible(key: string, defaultOpen = true): [boolean, () => void] {
  const lsKey = `sidebar-collapse-${key}`
  const [open, setOpen] = useState(() => {
    try {
      const stored = localStorage.getItem(lsKey)
      return stored === null ? defaultOpen : stored !== 'true'
    } catch {
      return defaultOpen
    }
  })
  const toggle = () => {
    setOpen((prev) => {
      try {
        localStorage.setItem(lsKey, String(!prev))
      } catch {
        /* ignore */
      }
      return !prev
    })
  }
  return [open, toggle]
}
