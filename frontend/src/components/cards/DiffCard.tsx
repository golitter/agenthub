import 'react-diff-view/style/index.css'

import { Check, Pencil, RotateCcw } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { DiffFileEditor } from '@/components/diff/DiffFileEditor'
import { DiffFileTabs } from '@/components/diff/DiffFileTabs'
import { DiffFileView } from '@/components/diff/DiffFileView'
import type { ParsedDiffFile } from '@/lib/diff-parser'
import { getFileName, parseUnifiedDiff } from '@/lib/diff-parser'

const API_BASE = '/api'

interface DiffCardProps {
  sessionId?: string
  initialDiff?: string
}

export function DiffCard({ sessionId, initialDiff }: DiffCardProps) {
  const [diff, setDiff] = useState<string | null>(initialDiff ?? null)
  const [loading, setLoading] = useState(!initialDiff && !!sessionId)
  const [error, setError] = useState<string | null>(null)
  const [activeFileIndex, setActiveFileIndex] = useState(0)
  const [editingFile, setEditingFile] = useState(false)
  const [actionStatus, setActionStatus] = useState<'idle' | 'committing' | 'reverting'>('idle')
  const [settled, setSettled] = useState<'committed' | 'reverted' | null>(null)
  const fetched = useRef(!!initialDiff)

  const parsed = useMemo(() => parseUnifiedDiff(diff ?? ''), [diff])
  const activeFile: ParsedDiffFile | undefined = parsed.files[activeFileIndex]

  const refresh = useCallback(async () => {
    if (!sessionId) return
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(`${API_BASE}/session/${sessionId}/diff`)
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setDiff(await res.text())
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load diff')
    } finally {
      setLoading(false)
    }
  }, [sessionId])

  useEffect(() => {
    if (!fetched.current && sessionId) {
      fetched.current = true
      refresh()
    }
  }, [sessionId, refresh])

  const handleAccept = async () => {
    if (!sessionId || actionStatus !== 'idle') return
    setActionStatus('committing')
    try {
      await fetch(`${API_BASE}/session/${sessionId}/commit`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: 'auto commit' }),
      })
      setSettled('committed')
      await refresh()
    } catch {
      // ignore
    } finally {
      setActionStatus('idle')
    }
  }

  const handleReject = async () => {
    if (!sessionId || actionStatus !== 'idle') return
    setActionStatus('reverting')
    try {
      await fetch(`${API_BASE}/session/${sessionId}/revert`, {
        method: 'POST',
      })
      setSettled('reverted')
      await refresh()
    } catch {
      // ignore
    } finally {
      setActionStatus('idle')
    }
  }

  const handleEditSave = async (content: string) => {
    if (!sessionId || !activeFile) return
    const filePath = activeFile.newPath
    await fetch(`${API_BASE}/session/${sessionId}/files/${encodeURIComponent(filePath)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content }),
    })
    setEditingFile(false)
    await refresh()
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

  if (!diff?.trim() || parsed.files.length === 0) {
    if (!settled) return null
    return (
      <div className="my-2 flex items-center gap-2 rounded-lg border border-border bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
        {settled === 'committed' ? (
          <>
            <Check className="h-3.5 w-3.5 text-green-500" /> 变更已接受
          </>
        ) : (
          <>
            <RotateCcw className="h-3.5 w-3.5 text-muted-foreground" /> 变更已拒绝
          </>
        )}
      </div>
    )
  }

  const { summary } = parsed

  return (
    <div className="diff-card my-2 overflow-hidden rounded-lg border border-border">
      {/* Header */}
      <div className="flex items-center justify-between border-b border-border bg-muted/50 px-3 py-1.5">
        <span className="text-xs text-muted-foreground">
          {summary.filesChanged} file{summary.filesChanged !== 1 ? 's' : ''} changed,{' '}
          <span className="text-green-500">+{summary.additions}</span>{' '}
          <span className="text-red-500">-{summary.deletions}</span>
        </span>
        <div className="flex gap-1">
          {sessionId && (
            <button
              onClick={() => setEditingFile(true)}
              className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
            >
              <Pencil className="h-3 w-3" />
              编辑
            </button>
          )}
          <button
            onClick={handleAccept}
            disabled={actionStatus !== 'idle'}
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:opacity-50"
          >
            <Check className="h-3 w-3" />
            {actionStatus === 'committing' ? '提交中...' : '接受变更'}
          </button>
          <button
            onClick={handleReject}
            disabled={actionStatus !== 'idle'}
            className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:opacity-50"
          >
            <RotateCcw className="h-3 w-3" />
            {actionStatus === 'reverting' ? '撤销中...' : '拒绝变更'}
          </button>
        </div>
      </div>

      {/* File tabs */}
      <DiffFileTabs
        files={parsed.files}
        activeIndex={activeFileIndex}
        onSelect={setActiveFileIndex}
      />

      {/* Content */}
      {activeFile && (
        <div className="max-h-96 overflow-auto text-xs">
          {editingFile ? (
            <DiffFileEditor
              oldContent={activeFile.oldContent}
              newContent={activeFile.newContent}
              fileName={getFileName(activeFile.newPath)}
              onSave={handleEditSave}
              onCancel={() => setEditingFile(false)}
            />
          ) : (
            <DiffFileView file={activeFile} />
          )}
        </div>
      )}
    </div>
  )
}
