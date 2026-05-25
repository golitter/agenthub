import { Check, RotateCcw } from 'lucide-react'
import { useState } from 'react'
import DiffViewer from 'react-diff-viewer-continued'

const API_BASE = '/api'

interface DiffCardProps {
  workspaceId: string
  initialDiff?: string
}

export function DiffCard({ workspaceId, initialDiff }: DiffCardProps) {
  const [diff, setDiff] = useState<string | null>(initialDiff ?? null)
  const [loading, setLoading] = useState(!initialDiff)
  const [error, setError] = useState<string | null>(null)

  async function refresh() {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(`${API_BASE}/workspace/${workspaceId}/diff`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setDiff(await res.text())
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load diff')
    } finally {
      setLoading(false)
    }
  }

  const handleAccept = async () => {
    try {
      await fetch(`${API_BASE}/workspace/${workspaceId}/commit`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: 'auto commit' }),
      })
      await refresh()
    } catch {
      // ignore
    }
  }

  const handleReject = async () => {
    try {
      await fetch(`${API_BASE}/workspace/${workspaceId}/revert`, {
        method: 'POST',
      })
      await refresh()
    } catch {
      // ignore
    }
  }

  if (loading) {
    return (
      <div className="my-2 rounded-lg border border-border bg-card px-4 py-3 text-sm text-muted-foreground">
        Loading diff...
      </div>
    )
  }

  if (error) {
    return (
      <div className="my-2 rounded-lg border border-destructive/50 bg-card px-4 py-3 text-sm text-destructive">
        {error}
      </div>
    )
  }

  if (!diff || diff.trim() === '') {
    return null
  }

  return (
    <div className="my-2 overflow-hidden rounded-lg border border-border">
      <div className="flex items-center justify-between border-b border-border bg-muted/50 px-3 py-1.5">
        <span className="text-xs text-muted-foreground">Changes</span>
        <div className="flex gap-1">
          <button
            onClick={handleAccept}
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          >
            <Check className="h-3 w-3" />
            接受变更
          </button>
          <button
            onClick={handleReject}
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
          >
            <RotateCcw className="h-3 w-3" />
            拒绝变更
          </button>
        </div>
      </div>
      <div className="max-h-96 overflow-auto text-xs">
        <DiffViewer oldValue="" newValue={diff} splitView={false} hideLineNumbers={false} />
      </div>
    </div>
  )
}
