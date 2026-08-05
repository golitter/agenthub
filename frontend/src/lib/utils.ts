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

/**
 * Accept only absolute http(s) URLs before rendering untrusted content as a
 * link or iframe source. Relative URLs (e.g. "/admin/users") are rejected so
 * that agent-emitted Markdown cannot smuggle same-origin links that would be
 * resolved against the current origin and clicked by the user.
 */
export function getSafeHttpUrl(value: string): string | null {
  if (!/^https?:\/\//i.test(value.trim())) return null
  try {
    const parsed = new URL(value)
    return parsed.protocol === 'http:' || parsed.protocol === 'https:' ? parsed.href : null
  } catch {
    return null
  }
}
