import { FileText, Send } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import type { AgentSessionInfo } from '@/lib/api'
import { UI_PLACEHOLDERS } from '@/lib/ui-text'

import { MarkdownRenderer } from '../markdown/MarkdownRenderer'

const MAX_INPUT_HEIGHT = 200
const MIN_INPUT_HEIGHT = 48
const MIN_MD_PANE_HEIGHT = 120
const MAX_MD_PANE_RATIO = 0.6
const HINT_DISPLAY_DURATION = 3000
const PREVIEW_DEBOUNCE_MS = 150

interface MessageInputProps {
  onSend: (message: string) => void
  disabled?: boolean
  sendDisabled?: boolean
  sendDisabledHint?: string
  placeholder?: string
  mentionSessions?: AgentSessionInfo[]
}

export function MessageInput({
  onSend,
  disabled = false,
  sendDisabled = false,
  sendDisabledHint,
  placeholder = UI_PLACEHOLDERS.MESSAGE_INPUT,
  mentionSessions,
}: MessageInputProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const mdTextareaRef = useRef<HTMLTextAreaElement>(null)
  const previewRef = useRef<HTMLDivElement>(null)
  const [inputValue, setInputValue] = useState('')
  const [mdMode, setMdMode] = useState(false)
  const [previewContent, setPreviewContent] = useState('')
  const [hint, setHint] = useState<string | null>(null)
  const [mentionOpen, setMentionOpen] = useState(false)
  const [mentionQuery, setMentionQuery] = useState('')
  const [mentionStart, setMentionStart] = useState(0)
  const [activeMentionIndex, setActiveMentionIndex] = useState(0)
  const [mdPaneHeight, setMdPaneHeight] = useState(MIN_MD_PANE_HEIGHT)
  const hintTimerRef = useRef<ReturnType<typeof setTimeout>>(null)
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout>>(null)
  const lastScrollRatioRef = useRef(0)

  // ── Cleanup ──
  useEffect(() => {
    return () => {
      if (hintTimerRef.current) clearTimeout(hintTimerRef.current)
      if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    }
  }, [])

  // ── Hint ──
  const showHint = useCallback((message: string) => {
    setHint(message)
    if (hintTimerRef.current) clearTimeout(hintTimerRef.current)
    hintTimerRef.current = setTimeout(() => setHint(null), HINT_DISPLAY_DURATION)
  }, [])

  // ── Single-mode height adjust ──
  const adjustHeight = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, MAX_INPUT_HEIGHT)}px`
  }, [])

  // ── Mention state ──
  const updateMentionState = useCallback(
    (value: string, el: HTMLTextAreaElement | null) => {
      if (!el || !mentionSessions?.length) {
        setMentionOpen(false)
        return
      }
      const cursor = el.selectionStart ?? value.length
      const beforeCursor = value.slice(0, cursor)
      const match = /(^|\s)@([^\s@]*)$/.exec(beforeCursor)
      if (!match) {
        setMentionOpen(false)
        return
      }
      const prefix = match[1] ?? ''
      setMentionStart(beforeCursor.length - match[0].length + prefix.length)
      setMentionQuery(match[2] ?? '')
      setMentionOpen(true)
      setActiveMentionIndex(0)
    },
    [mentionSessions],
  )

  const mentionOptions = useMemo(() => {
    if (!mentionSessions?.length) return []
    const query = mentionQuery.trim().toLowerCase()
    return mentionSessions
      .filter((session) => {
        if (!query) return true
        const values = [
          session.mentionLabel,
          session.routeId,
          session.agentName,
          session.agentType,
          ...(session.aliases ?? []),
        ]
        return values.some((value) => value.toLowerCase().includes(query))
      })
      .slice(0, 8)
  }, [mentionQuery, mentionSessions])

  // ── Insert mention via state ──
  const insertMention = useCallback(
    (session: AgentSessionInfo) => {
      const el = mdMode ? mdTextareaRef.current : textareaRef.current
      if (!el) return
      const current = el.value
      const cursor = el.selectionStart ?? current.length
      const before = current.slice(0, mentionStart)
      const after = current.slice(cursor)
      const insertion = `@${session.mentionLabel} `
      const next = `${before}${insertion}${after}`
      setInputValue(next)
      const nextCursor = before.length + insertion.length
      // Cursor set after React re-render
      requestAnimationFrame(() => {
        el.focus()
        el.setSelectionRange(nextCursor, nextCursor)
      })
      setMentionOpen(false)
    },
    [mentionStart, mdMode],
  )

  // ── Send ──
  const handleSend = useCallback(() => {
    if (sendDisabled) {
      if (sendDisabledHint) showHint(sendDisabledHint)
      return
    }
    // Read from DOM to avoid stale closure over inputValue
    const el = mdMode ? mdTextareaRef.current : textareaRef.current
    const value = (el?.value ?? '').trim()
    if (!value || disabled) return
    onSend(value)
    setInputValue('')
    setPreviewContent('')
    setMentionOpen(false)
    setMdPaneHeight(MIN_MD_PANE_HEIGHT)
    if (textareaRef.current) textareaRef.current.style.height = `${MIN_INPUT_HEIGHT}px`
  }, [mdMode, onSend, disabled, sendDisabled, sendDisabledHint, showHint])

  // ── Key down ──
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      // In MD mode Enter inserts newline; only single-pane sends on Enter
      if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing && !mdMode) {
        if (mentionOpen && mentionOptions.length > 0) {
          e.preventDefault()
          insertMention(mentionOptions[activeMentionIndex] ?? mentionOptions[0])
          return
        }
        e.preventDefault()
        handleSend()
        return
      }
      if (mentionOpen && mentionOptions.length > 0) {
        if (e.key === 'ArrowDown') {
          e.preventDefault()
          setActiveMentionIndex((idx) => (idx + 1) % mentionOptions.length)
        } else if (e.key === 'ArrowUp') {
          e.preventDefault()
          setActiveMentionIndex((idx) => (idx - 1 + mentionOptions.length) % mentionOptions.length)
        } else if (e.key === 'Tab') {
          e.preventDefault()
          insertMention(mentionOptions[activeMentionIndex] ?? mentionOptions[0])
        } else if (e.key === 'Escape') {
          e.preventDefault()
          setMentionOpen(false)
        }
      }
    },
    [activeMentionIndex, handleSend, insertMention, mdMode, mentionOpen, mentionOptions],
  )

  // ── Markdown preview debounce ──
  const schedulePreview = useCallback((value: string) => {
    if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    debounceTimerRef.current = setTimeout(() => {
      setPreviewContent(value)
    }, PREVIEW_DEBOUNCE_MS)
  }, [])

  // ── Dual-pane auto-grow ──
  const adjustMdPaneHeight = useCallback(() => {
    const el = mdTextareaRef.current
    if (!el) return
    el.style.height = 'auto'
    const contentH = el.scrollHeight
    const maxH = Math.round(window.innerHeight * MAX_MD_PANE_RATIO)
    const h = Math.max(MIN_MD_PANE_HEIGHT, Math.min(contentH, maxH))
    el.style.height = `${h}px`
    setMdPaneHeight(h)
  }, [])

  // ── Sync scroll ──
  const syncPreviewScroll = useCallback(() => {
    const textarea = mdTextareaRef.current
    const preview = previewRef.current
    if (!textarea || !preview) return
    const scrollRatio = textarea.scrollTop / (textarea.scrollHeight - textarea.clientHeight || 1)
    lastScrollRatioRef.current = scrollRatio
    const previewMax = preview.scrollHeight - preview.clientHeight
    preview.scrollTop = scrollRatio * previewMax
  }, [])

  // ── Restore scroll after preview re-render ──
  useEffect(() => {
    if (!mdMode) return
    const preview = previewRef.current
    if (!preview) return
    const previewMax = preview.scrollHeight - preview.clientHeight
    preview.scrollTop = lastScrollRatioRef.current * previewMax
  }, [previewContent, mdMode])

  // ── Toggle MD mode ──
  const toggleMdMode = useCallback(() => {
    setMdMode((prev) => !prev)
  }, [])

  // ── Focus textarea when switching to MD mode ──
  useEffect(() => {
    if (mdMode) {
      requestAnimationFrame(() => mdTextareaRef.current?.focus())
    }
  }, [mdMode])

  const canSend = !disabled && !sendDisabled

  return (
    <div className="border-t border-border">
      {/* Hint */}
      {hint && (
        <div className="px-4 pt-3">
          <div className="rounded-lg bg-muted px-3 py-1.5 text-xs text-tertiary">{hint}</div>
        </div>
      )}

      {/* Toolbar */}
      <div className={`flex items-center gap-2 px-4 pt-2 ${mdMode ? 'pb-0' : ''}`}>
        <button
          type="button"
          onClick={toggleMdMode}
          className={`flex items-center gap-1 rounded-[5px] border px-2 py-0.5 font-mono text-[11px] font-medium transition-all ${
            mdMode
              ? 'border-primary-border bg-primary-soft text-primary'
              : 'border-border text-tertiary hover:bg-muted hover:text-secondary'
          }`}
        >
          <FileText className="h-3 w-3" strokeWidth={1.5} />
          Markdown
        </button>
      </div>

      {!mdMode ? (
        /* ═══ Single-pane mode ═══ */
        <div className="relative flex items-end gap-2 px-4 py-3">
          {mentionOpen && mentionOptions.length > 0 && (
            <div className="absolute bottom-[calc(100%+8px)] left-0 z-20 w-[min(360px,calc(100vw-2rem))] min-w-[220px] overflow-hidden rounded-[8px] border border-border bg-popover py-1 shadow-lg">
              {mentionOptions.map((session, index) => (
                <button
                  key={session.sessionId}
                  type="button"
                  className={`flex w-full min-w-0 items-center justify-between gap-3 px-3 py-2 text-left text-sm ${
                    index === activeMentionIndex ? 'bg-muted text-foreground' : 'text-foreground'
                  }`}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    insertMention(session)
                  }}
                >
                  <span className="min-w-0 truncate font-medium">{session.mentionLabel}</span>
                  <span className="shrink-0 text-[11px] text-tertiary">{session.agentType}</span>
                </button>
              ))}
            </div>
          )}
          <textarea
            ref={textareaRef}
            value={inputValue}
            className="flex-1 resize-none break-words rounded-[8px] bg-card px-3 py-2.5 text-sm text-foreground outline-none placeholder:text-tertiary disabled:opacity-50"
            style={{
              minHeight: MIN_INPUT_HEIGHT,
              maxHeight: MAX_INPUT_HEIGHT,
            }}
            placeholder={placeholder}
            disabled={disabled}
            rows={1}
            onChange={(e) => {
              setInputValue(e.target.value)
              adjustHeight()
              updateMentionState(e.target.value, e.target)
            }}
            onClick={(e) => updateMentionState(inputValue, e.currentTarget)}
            onKeyDown={handleKeyDown}
          />
          <button
            className="flex w-[40px] shrink-0 items-center justify-center rounded-[6px] bg-primary disabled:opacity-40"
            style={{ height: MIN_INPUT_HEIGHT }}
            onClick={handleSend}
            disabled={!canSend}
          >
            <Send className="h-4 w-4 text-primary-foreground" strokeWidth={1.25} />
          </button>
        </div>
      ) : (
        /* ═══ Dual-pane MD mode ═══ */
        <div className="relative flex gap-0 px-4 pb-3 pt-2" style={{ height: mdPaneHeight }}>
          {mentionOpen && mentionOptions.length > 0 && (
            <div className="absolute bottom-[calc(100%+8px)] left-0 z-20 w-[min(360px,calc(100vw-2rem))] min-w-[220px] overflow-hidden rounded-[8px] border border-border bg-popover py-1 shadow-lg">
              {mentionOptions.map((session, index) => (
                <button
                  key={session.sessionId}
                  type="button"
                  className={`flex w-full min-w-0 items-center justify-between gap-3 px-3 py-2 text-left text-sm ${
                    index === activeMentionIndex ? 'bg-muted text-foreground' : 'text-foreground'
                  }`}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    insertMention(session)
                  }}
                >
                  <span className="min-w-0 truncate font-medium">{session.mentionLabel}</span>
                  <span className="shrink-0 text-[11px] text-tertiary">{session.agentType}</span>
                </button>
              ))}
            </div>
          )}
          {/* Left: editor */}
          <div className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-l-[8px] border border-border bg-card">
            <textarea
              ref={mdTextareaRef}
              value={inputValue}
              className="flex-1 resize-none px-3 py-2.5 font-mono text-[13px] leading-relaxed text-foreground outline-none placeholder:text-tertiary disabled:opacity-50"
              placeholder="输入 Markdown..."
              disabled={disabled}
              onChange={(e) => {
                setInputValue(e.target.value)
                schedulePreview(e.target.value)
                updateMentionState(e.target.value, e.target)
                // Auto-grow on next frame
                requestAnimationFrame(adjustMdPaneHeight)
              }}
              onClick={(e) => {
                updateMentionState(inputValue, e.currentTarget)
                syncPreviewScroll()
              }}
              onScroll={syncPreviewScroll}
              onKeyUp={syncPreviewScroll}
              onKeyDown={handleKeyDown}
            />
          </div>
          {/* Divider */}
          <div className="w-px shrink-0 bg-border" />
          {/* Right: preview */}
          <div className="flex min-w-0 flex-1 flex-col overflow-hidden border border-l-0 border-border bg-card">
            <div
              ref={previewRef}
              className="flex-1 overflow-y-auto px-3 py-2.5 [&::-webkit-scrollbar]:w-[3px] [&::-webkit-scrollbar-thumb]:rounded-[2px] [&::-webkit-scrollbar-thumb]:bg-tertiary"
            >
              {inputValue.trim() ? (
                <div className="text-[13px]">
                  <MarkdownRenderer content={previewContent || inputValue} />
                </div>
              ) : (
                <p className="py-2 text-[12px] italic text-tertiary">
                  预览区域 — 在左侧输入 Markdown
                </p>
              )}
            </div>
          </div>
          {/* Send button */}
          <button
            className="flex shrink-0 items-center justify-center rounded-r-[8px] bg-primary disabled:opacity-40"
            style={{ width: 44 }}
            onClick={handleSend}
            disabled={!canSend}
          >
            <Send className="h-4 w-4 text-primary-foreground" strokeWidth={1.25} />
          </button>
        </div>
      )}
    </div>
  )
}
