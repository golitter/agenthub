import './index.css'

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { lazy, StrictMode, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router'

const AgentProfilePage = lazy(() =>
  import('./pages/AgentProfilePage').then((module) => ({ default: module.AgentProfilePage })),
)
const ImPage = lazy(() => import('./pages/ImPage').then((module) => ({ default: module.ImPage })))

const queryClient = new QueryClient()

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
