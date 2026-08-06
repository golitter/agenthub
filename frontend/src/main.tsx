import './index.css'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { lazy, StrictMode, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router'

const AgentProfilePage = lazy(() =>
  import('./pages/AgentProfilePage').then((module) => ({ default: module.AgentProfilePage })),
)
const ImPage = lazy(() => import('./pages/ImPage').then((module) => ({ default: module.ImPage })))

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // 默认 staleTime：避免每次窗口聚焦/路由切换都立即重新拉取，
      // 同时保持数据相对新鲜。各 useQuery 可按需覆盖。
      staleTime: 30_000,
      // 收紧重试次数：默认 3 次对带鉴权的 admin 接口会放大 401 副作用，
      // 对瞬时网络抖动 1 次重试已足够。
      retry: 1,
      refetchOnWindowFocus: false,
    },
    mutations: {
      // 写操作（删除会话/工作区等非幂等动作）默认不重试，
      // 防止网络抖动下重复执行导致的数据重复删除。
      retry: 0,
    },
  },
})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Suspense fallback={<div className="min-h-dvh bg-background" aria-busy="true" />}>
          <Routes>
            <Route path="/agent/:sessionId" element={<AgentProfilePage />} />
            <Route path="/*" element={<ImPage />} />
          </Routes>
        </Suspense>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
