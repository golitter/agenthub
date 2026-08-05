import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(...inputs))
}

export function getFileName(path: string): string {
  return path.split('/').pop() ?? path
}

/** 对相对于 workspace 的路径进行编码，避免把分隔符当作数据处理。 */
export function encodePathSegments(path: string): string {
  return path
    .split('/')
    .filter(Boolean)
    .map((segment) => encodeURIComponent(segment))
    .join('/')
}

/**
 * 仅接受绝对 http(s) URL，然后才将不可信内容渲染为链接或 iframe 源。
 * 相对 URL（例如 "/admin/users"）会被拒绝，这样 agent 输出的 Markdown
 * 就无法夹带同源链接，避免其被解析到当前 origin 上并被用户点击。
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
