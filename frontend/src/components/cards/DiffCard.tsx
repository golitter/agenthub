import 'react-diff-view/style/index.css'

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { DiffFileEditor } from '@/components/diff/DiffFileEditor'
import { DiffFileInfo } from '@/components/diff/DiffFileInfo'
import { DiffFileTabs } from '@/components/diff/DiffFileTabs'
import { DiffFileView } from '@/components/diff/DiffFileView'
import { DiffHeader } from '@/components/diff/DiffHeader'
import { API_BASE } from '@/lib/constants'
import type { ParsedDiffFile } from '@/lib/diff-parser'
import { parseUnifiedDiff } from '@/lib/diff-parser'
import { getFileName } from '@/lib/utils'

type SnapshotStatus = 'pending' | 'committed' | 'reverted' | 'cancelled'

export function DiffCard({ snapshotId, sessionId }: { snapshotId: string; sessionId?: string }) {
  const [diff, setDiff] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [activeFileIndex, setActiveFileIndex] = useState(0)
  const [editingFile, setEditingFile] = useState(false)
  const [viewType, setViewType] = useState<'split' | 'unified'>('split')
  const [actionStatus, setActionStatus] = useState<'idle' | 'committing' | 'reverting'>('idle')
  const [snapshotStatus, setSnapshotStatus] = useState<SnapshotStatus | null>(null)
  const initialized = useRef(false)

  const isSettled =
    snapshotStatus === 'committed' ||
    snapshotStatus === 'reverted' ||
    snapshotStatus === 'cancelled'

  const parsed = useMemo(() => parseUnifiedDiff(diff ?? ''), [diff])
  const activeFile: ParsedDiffFile | undefined = parsed.files[activeFileIndex]

  // Snapshot-first load: GET snapshot → 404 → workspace diff → PUT pending
  useEffect(() => {
    if (initialized.current) return
    initialized.current = true
    ;(async () => {
      setLoading(true)
      setError(null)
      try {
        // Try fetching existing snapshot
        const snapRes = await fetch(`${API_BASE}/diff-snapshots/${snapshotId}`)
        if (snapRes.ok) {
          const snap = await snapRes.json()
          const data = snap?.data ?? snap
          setDiff(data.diff_content ?? data.diff ?? '')
          setSnapshotStatus(data.status ?? 'pending')
          setLoading(false)
          return
        }

        // Snapshot not found — fetch workspace diff and create pending snapshot
        if (!sessionId) {
          setLoading(false)
          return
        }

        const wsRes = await fetch(`${API_BASE}/session/${sessionId}/diff`)
        let diffText = ''
        if (wsRes.ok) {
          diffText = await wsRes.text()
        }

        if (!diffText?.trim()) {
          setLoading(false)
          return
        }

        // Create pending snapshot
        await fetch(`${API_BASE}/diff-snapshots/${snapshotId}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ session_id: sessionId, diff: diffText, status: 'pending' }),
        })
        setDiff(diffText)
        setSnapshotStatus('pending')
      } catch (e) {
        setError(e instanceof Error ? e.message : 'Failed to load diff')
      } finally {
        setLoading(false)
      }
    })()
  }, [snapshotId, sessionId])

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

  const handleAccept = async () => {
    if (!sessionId || actionStatus !== 'idle' || !diff) return
    setActionStatus('committing')
    try {
      await fetch(`${API_BASE}/session/${sessionId}/commit`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: 'auto commit' }),
      })
      await fetch(`${API_BASE}/diff-snapshots/${snapshotId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: sessionId, diff, status: 'committed' }),
      })
      setSnapshotStatus('committed')
    } catch {
      // ignore
    } finally {
      setActionStatus('idle')
    }
  }

  const handleReject = async () => {
    if (!sessionId || actionStatus !== 'idle' || !diff) return
    setActionStatus('reverting')
    try {
      await fetch(`${API_BASE}/session/${sessionId}/revert`, { method: 'POST' })
      await fetch(`${API_BASE}/diff-snapshots/${snapshotId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: sessionId, diff, status: 'reverted' }),
      })
      setSnapshotStatus('reverted')
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
      headers: { 'Content-Type': 'text/plain' },
      body: content,
    })
    setEditingFile(false)
    await refresh()
  }

  if (loading)
    return (
      <div className="my-2 rounded-lg border border-border bg-card px-4 py-3 text-sm text-muted-foreground">
        Loading diff...
      </div>
    )
  if (error)
    return (
      <div className="my-2 rounded-lg border border-destructive/50 bg-card px-4 py-3 text-sm text-destructive">
        {error}
      </div>
    )
  if (!diff?.trim() || parsed.files.length === 0) return null

  const { summary } = parsed

  return (
    <div className="diff-card my-2 overflow-hidden rounded-lg border border-border">
      <DiffHeader
        summary={summary}
        viewType={viewType}
        onViewTypeChange={setViewType}
        snapshotStatus={snapshotStatus}
        isSettled={isSettled}
        hasSession={!!sessionId}
        onEdit={() => setEditingFile(true)}
        onAccept={handleAccept}
        onReject={handleReject}
        actionStatus={actionStatus}
      />

      <DiffFileTabs
        files={parsed.files}
        activeIndex={activeFileIndex}
        onSelect={setActiveFileIndex}
      />

      {activeFile && <DiffFileInfo file={activeFile} />}
      {activeFile && (
        <div className={`max-h-96 overflow-auto text-xs${isSettled ? ' opacity-60' : ''}`}>
          {editingFile ? (
            <DiffFileEditor
              oldContent={activeFile.oldContent}
              newContent={activeFile.newContent}
              fileName={getFileName(activeFile.newPath)}
              onSave={handleEditSave}
              onCancel={() => setEditingFile(false)}
            />
          ) : (
            <DiffFileView file={activeFile} viewType={viewType} />
          )}
        </div>
      )}
    </div>
  )
}
