import { useEffect } from 'react'

import { PROJECT_META } from '@/lib/constants'

export function formatPageTitle(subject?: string): string {
  return subject ? `${subject} · ${PROJECT_META.NAME}` : PROJECT_META.NAME
}

export function usePageTitle(subject?: string) {
  useEffect(() => {
    document.title = formatPageTitle(subject)
  }, [subject])
}
