import { useQuery } from '@tanstack/react-query'

import { getAdminAvatar } from '@/lib/api'

// admin 头像的共享查询键。所有需要 admin 头像的组件都应订阅此查询，
// 避免每个组件各自 fetch 导致重复请求与头像闪烁。
export const ADMIN_AVATAR_QUERY_KEY = ['admin-avatar'] as const

// admin 头像兜底地址（与 admin store 默认值保持一致）。
export const FALLBACK_ADMIN_AVATAR_URL =
  'https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede'

/**
 * 获取 admin 头像 URL 的共享 hook。
 *
 * 多个组件（IconSidebar、AdminMenu、UserManagementPage）需要展示同一个
 * admin 头像。通过 react-query 的 ['admin-avatar'] 缓存，所有订阅者共享
 * 同一次请求结果与加载状态，避免：
 * 1. 进入 /admin 页面时 IconSidebar + AdminMenu 各发一次 getAdminAvatar 请求；
 * 2. 各组件独立的 loaded 状态导致头像从 fallback 切到真实 URL 时闪烁。
 */
export function useAdminAvatar() {
  const query = useQuery({
    queryKey: ADMIN_AVATAR_QUERY_KEY,
    queryFn: getAdminAvatar,
  })
  return {
    url: query.data?.url,
    isLoading: query.isLoading,
    refetch: query.refetch,
  }
}
