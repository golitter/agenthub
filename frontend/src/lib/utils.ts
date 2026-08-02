import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(...inputs))
}

export function getFileName(path: string): string {
  return path.split('/').pop() ?? path
}

/** Encode a workspace-relative path without turning its separators into data. */
export function encodePathSegments(path: string): string {
  return path
    .split('/')
    .filter(Boolean)
    .map((segment) => encodeURIComponent(segment))
    .join('/')
}

/** Accept only web URLs before rendering untrusted content as a link or iframe source. */
export function getSafeHttpUrl(value: string): string | null {
  try {
    const base = typeof window === 'undefined' ? 'http://localhost' : window.location.origin
    const parsed = new URL(value, base)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed.href : null
  } catch {
    return null
  }
}
