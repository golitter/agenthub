import './index.css'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { lazy, StrictMode, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router'

const AgentProfilePage = lazy(() =>
  import('./pages/AgentProfilePage').then((module) => ({ default: module.AgentProfilePage })),
)
const ImPage = lazy(() => import('./pages/ImPage').then((module) => ({ default: module.ImPage })))

function AppLoadingState() {
  return (
    <div
      className="grid min-h-dvh grid-cols-1 bg-background md:grid-cols-[3.5rem_17.5rem_minmax(0,1fr)]"
      aria-busy="true"
      aria-live="polite"
    >
      <div className="hidden border-r border-border bg-sidebar md:block" />
      <div className="hidden border-r border-border bg-sidebar p-3 md:block">
        <div className="mb-4 flex items-center justify-between">
          <div className="space-y-2">
            <div className="h-3.5 w-20 rounded skeleton-sheen" />
            <div className="h-2.5 w-12 rounded skeleton-sheen" />
          </div>
          <div className="h-8 w-8 rounded-[8px] skeleton-sheen" />
        </div>
        <div className="mb-4 h-9 rounded-[10px] skeleton-sheen" />
        <div className="space-y-2">
          {Array.from({ length: 6 }).map((_, index) => (
            <div key={index} className="flex items-center gap-3 rounded-xl px-2 py-2.5">
              <div className="h-8 w-8 rounded-[9px] skeleton-sheen" />
              <div className="min-w-0 flex-1 space-y-2">
                <div className="h-3 w-3/5 rounded skeleton-sheen" />
                <div className="h-2.5 w-4/5 rounded skeleton-sheen" />
              </div>
            </div>
          ))}
        </div>
      </div>
      <main className="chat-canvas flex min-h-dvh items-center justify-center px-6 text-center">
        <div>
          <img src="/favicon.svg" alt="" className="mx-auto h-10 w-10 rounded-[10px]" />
          <p className="mt-3 text-sm font-medium text-foreground">正在准备工作台</p>
          <p className="mt-1 text-xs text-tertiary">正在载入会话与运行环境</p>
        </div>
      </main>
    </div>
  )
}

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
        <Suspense fallback={<AppLoadingState />}>
          <Routes>
            <Route path="/agent/:sessionId" element={<AgentProfilePage />} />
            <Route path="/*" element={<ImPage />} />
          </Routes>
        </Suspense>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
