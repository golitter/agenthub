import 'react-diff-view/style/index.css'

import { useCallback, useEffect, useMemo, useState } from 'react'

import { DiffFileEditor } from '@/components/diff/DiffFileEditor'
import { DiffFileInfo } from '@/components/diff/DiffFileInfo'
import { DiffFileTabs } from '@/components/diff/DiffFileTabs'
import { DiffFileView } from '@/components/diff/DiffFileView'
import { DiffHeader } from '@/components/diff/DiffHeader'
import { API_BASE } from '@/lib/constants'
import type { ParsedDiffFile } from '@/lib/diff-parser'
import { parseUnifiedDiff } from '@/lib/diff-parser'
import { UI_ACTIONS, UI_ERRORS, UI_MESSAGES, UI_STATUS } from '@/lib/ui-text'
import { cn, encodePathSegments, getFileName } from '@/lib/utils'

type SnapshotStatus = 'pending' | 'committed' | 'reverted' | 'cancelled'

async function ensureResponseOk(response: Response, fallbackMessage: string): Promise<void> {
  if (response.ok) return
  const payload = await response.json().catch(() => ({}))
  const message =
    typeof payload?.msg === 'string'
      ? payload.msg
      : typeof payload?.message === 'string'
        ? payload.message
        : fallbackMessage
  throw new Error(message)
}

export function DiffCard({ snapshotId, sessionId }: { snapshotId: string; sessionId?: string }) {
  const [diff, setDiff] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [activeFileIndex, setActiveFileIndex] = useState(0)
  const [editingFile, setEditingFile] = useState(false)
  const [viewType, setViewType] = useState<'split' | 'unified'>('split')
  const [actionStatus, setActionStatus] = useState<'idle' | 'committing' | 'reverting'>('idle')
  const [snapshotStatus, setSnapshotStatus] = useState<SnapshotStatus | null>(null)
  const requestKey = `${snapshotId}:${sessionId ?? ''}`
  const [loadedRequestKey, setLoadedRequestKey] = useState<string | null>(null)
  const [loadRetryKey, setLoadRetryKey] = useState(0)

  const isSettled =
    snapshotStatus === 'committed' ||
    snapshotStatus === 'reverted' ||
    snapshotStatus === 'cancelled'

  const isCurrentRequest = loadedRequestKey === requestKey
  const parsed = useMemo(
    () => parseUnifiedDiff(isCurrentRequest ? (diff ?? '') : ''),
    [diff, isCurrentRequest],
  )
  const activeFile: ParsedDiffFile | undefined = parsed.files[activeFileIndex]

  // Snapshot-first load: GET snapshot → 404 → workspace diff → PUT pending
  useEffect(() => {
    let cancelled = false

    ;(async () => {
      try {
        // Try fetching existing snapshot
        const snapRes = await fetch(
          `${API_BASE}/diff-snapshots/${encodeURIComponent(snapshotId)}`,
        )
        if (cancelled) return
        if (snapRes.ok) {
          const snap = await snapRes.json()
          if (cancelled) return
          const data = snap?.data ?? snap
          setDiff(data.diff_content ?? data.diff ?? '')
          setSnapshotStatus(data.status ?? 'pending')
          setActiveFileIndex(0)
          setError(null)
          setLoadedRequestKey(requestKey)
          setLoading(false)
          return
        }

        if (snapRes.status !== 404) {
          await ensureResponseOk(snapRes, UI_MESSAGES.LOAD_DIFF_FAILED)
        }

        // Snapshot not found — fetch workspace diff and create pending snapshot
        if (!sessionId) {
          setDiff(null)
          setSnapshotStatus(null)
          setEditingFile(false)
          setError(null)
          setLoadedRequestKey(requestKey)
          setLoading(false)
          return
        }

        const wsRes = await fetch(
          `${API_BASE}/session/${encodeURIComponent(sessionId)}/diff`,
        )
        if (cancelled) return
        await ensureResponseOk(wsRes, UI_MESSAGES.LOAD_DIFF_FAILED)
        const diffText = await wsRes.text()

        if (!diffText?.trim()) {
          setDiff(null)
          setSnapshotStatus(null)
          setEditingFile(false)
          setError(null)
          setLoadedRequestKey(requestKey)
          setLoading(false)
          return
        }

        // Create pending snapshot
        const snapshotResponse = await fetch(
          `${API_BASE}/diff-snapshots/${encodeURIComponent(snapshotId)}`,
          {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ session_id: sessionId, diff: diffText, status: 'pending' }),
          },
        )
        await ensureResponseOk(snapshotResponse, UI_MESSAGES.LOAD_DIFF_FAILED)
        if (cancelled) return
        setDiff(diffText)
        setSnapshotStatus('pending')
        setActiveFileIndex(0)
        setError(null)
        setLoadedRequestKey(requestKey)
      } catch (e) {
        if (cancelled) return
        setError(e instanceof Error ? e.message : UI_MESSAGES.LOAD_DIFF_FAILED)
        setLoadedRequestKey(requestKey)
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()

    return () => {
      cancelled = true
    }
  }, [loadRetryKey, requestKey, snapshotId, sessionId])

  const retryLoad = useCallback(() => {
    setLoading(true)
    setError(null)
    setDiff(null)
    setSnapshotStatus(null)
    setActiveFileIndex(0)
    setEditingFile(false)
    setLoadedRequestKey(null)
    setLoadRetryKey((key) => key + 1)
  }, [])

  const refresh = useCallback(async () => {
    if (!sessionId) return
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(`${API_BASE}/session/${encodeURIComponent(sessionId)}/diff`)
      await ensureResponseOk(res, UI_MESSAGES.LOAD_DIFF_FAILED)
      setDiff(await res.text())
      setActiveFileIndex(0)
    } catch (e) {
      setError(e instanceof Error ? e.message : UI_MESSAGES.LOAD_DIFF_FAILED)
    } finally {
      setLoading(false)
    }
  }, [sessionId])

  const handleAccept = async () => {
    if (!sessionId || actionStatus !== 'idle' || !diff) return
    setActionStatus('committing')
    try {
      const commitResponse = await fetch(
        `${API_BASE}/session/${encodeURIComponent(sessionId)}/commit`,
        {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: 'auto commit' }),
        },
      )
      await ensureResponseOk(commitResponse, UI_ERRORS.COMMIT_FAILED)
      const snapshotResponse = await fetch(
        `${API_BASE}/diff-snapshots/${encodeURIComponent(snapshotId)}`,
        {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: sessionId, diff, status: 'committed' }),
        },
      )
      await ensureResponseOk(snapshotResponse, UI_ERRORS.COMMIT_FAILED)
      setSnapshotStatus('committed')
    } catch (e) {
      setError(e instanceof Error ? e.message : UI_ERRORS.COMMIT_FAILED)
    } finally {
      setActionStatus('idle')
    }
  }

  const handleReject = async () => {
    if (!sessionId || actionStatus !== 'idle' || !diff) return
    setActionStatus('reverting')
    try {
      const revertResponse = await fetch(
        `${API_BASE}/session/${encodeURIComponent(sessionId)}/revert`,
        { method: 'POST' },
      )
      await ensureResponseOk(revertResponse, UI_ERRORS.REVERT_FAILED)
      const snapshotResponse = await fetch(
        `${API_BASE}/diff-snapshots/${encodeURIComponent(snapshotId)}`,
        {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session_id: sessionId, diff, status: 'reverted' }),
        },
      )
      await ensureResponseOk(snapshotResponse, UI_ERRORS.REVERT_FAILED)
      setSnapshotStatus('reverted')
    } catch (e) {
      setError(e instanceof Error ? e.message : UI_ERRORS.REVERT_FAILED)
    } finally {
      setActionStatus('idle')
    }
  }

  const handleEditSave = async (content: string) => {
    if (!sessionId || !activeFile) return
    const filePath = activeFile.newPath
    try {
      const response = await fetch(
        `${API_BASE}/session/${encodeURIComponent(sessionId)}/files/${encodePathSegments(filePath)}`,
        {
          method: 'PUT',
          headers: { 'Content-Type': 'text/plain' },
          body: content,
        },
      )
      await ensureResponseOk(response, UI_ERRORS.SAVE_DIFF_FAILED)
      setEditingFile(false)
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : UI_ERRORS.SAVE_DIFF_FAILED)
    }
  }

  if (loading || !isCurrentRequest)
    return (
      <div className="my-2 rounded-lg border border-border bg-card px-4 py-3 text-sm text-muted-foreground">
        {UI_STATUS.LOADING}
      </div>
    )
  if (isCurrentRequest && error)
    return (
      <div
        className="my-2 rounded-lg border border-destructive/50 bg-card px-4 py-3 text-sm text-destructive"
        role="alert"
      >
        <p>
          {error === UI_MESSAGES.LOAD_DIFF_FAILED
            ? error
            : `${UI_MESSAGES.LOAD_DIFF_FAILED}: ${error}`}
        </p>
        <button
          type="button"
          className="mt-2 rounded-md border border-destructive/30 px-2.5 py-1 text-xs transition-[background,transform] hover:bg-destructive/10 active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          onClick={retryLoad}
        >
          {UI_ACTIONS.RETRY}
        </button>
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
        canEdit={!!activeFile && activeFile.hunks.length > 0}
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
        <div className={cn('max-h-96 overflow-auto text-xs', isSettled && 'opacity-60')}>
          {editingFile ? (
            <DiffFileEditor
              key={activeFile.newPath}
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
