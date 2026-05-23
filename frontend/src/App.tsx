import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { useStore } from '@/stores/app'

function App() {
  const [name, setName] = useState('')
  const { count, increment } = useStore()

  const { data, isLoading } = useQuery({
    queryKey: ['health'],
    queryFn: () => fetch('http://localhost:8080/ping').then((r) => r.json()),
    retry: false,
  })

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-8">
      <div className="flex flex-col gap-6 w-full max-w-md">
        <h1 className="text-3xl font-bold text-foreground">AgentHub</h1>

        {/* Zustand */}
        <Card>
          <CardHeader>
            <CardTitle>Zustand</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="mb-2 text-muted-foreground">Count: {count}</p>
            <Button onClick={increment}>+1</Button>
          </CardContent>
        </Card>

        {/* shadcn Input + Dialog */}
        <Card>
          <CardHeader>
            <CardTitle>Input + Dialog</CardTitle>
          </CardHeader>
          <CardContent className="flex gap-2">
            <Input placeholder="输入名字" value={name} onChange={(e) => setName(e.target.value)} />
            <Dialog>
              <DialogTrigger asChild>
                <Button variant="outline">确认</Button>
              </DialogTrigger>
              <DialogContent>
                <DialogHeader>
                  <DialogTitle>你好, {name || '世界'}!</DialogTitle>
                </DialogHeader>
                <p className="text-muted-foreground">Dialog 组件正常工作。</p>
              </DialogContent>
            </Dialog>
          </CardContent>
        </Card>

        {/* TanStack Query */}
        <Card>
          <CardHeader>
            <CardTitle>TanStack Query</CardTitle>
          </CardHeader>
          <CardContent>
            {isLoading ? (
              <p className="text-muted-foreground">加载中...</p>
            ) : data ? (
              <p className="text-muted-foreground">Backend: {JSON.stringify(data)}</p>
            ) : (
              <p className="text-muted-foreground">Backend 未连接（正常，尚未启动）</p>
            )}
          </CardContent>
        </Card>

        {/* React Router */}
        <p className="text-xs text-muted-foreground text-center">
          React Router + Vite + Tailwind + shadcn/ui + Zustand + TanStack Query
        </p>
      </div>
    </div>
  )
}

export default App
