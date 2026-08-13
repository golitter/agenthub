import { FileText, Send, Square } from 'lucide-react'
import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react'

import type { AgentSessionInfo } from '@/lib/api'
import { UI_ACTIONS, UI_LABELS, UI_PLACEHOLDERS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

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
  onStop?: () => void
  isStopping?: boolean
}

export function MessageInput({
  onSend,
  disabled = false,
  sendDisabled = false,
  sendDisabledHint,
  placeholder = UI_PLACEHOLDERS.MESSAGE_INPUT,
  mentionSessions,
  onStop,
  isStopping = false,
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
  const mentionListId = useId()

  // ── 清理 ──
  useEffect(() => {
    return () => {
      if (hintTimerRef.current) clearTimeout(hintTimerRef.current)
      if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    }
  }, [])

  // ── 提示 ──
  const showHint = useCallback((message: string) => {
    setHint(message)
    if (hintTimerRef.current) clearTimeout(hintTimerRef.current)
    hintTimerRef.current = setTimeout(() => setHint(null), HINT_DISPLAY_DURATION)
  }, [])

  // ── 单栏模式高度调整 ──
  const adjustHeight = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, MAX_INPUT_HEIGHT)}px`
  }, [])

  // ── Mention 状态 ──
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
  const activeMentionOptionIndex = mentionOptions.length
    ? Math.min(activeMentionIndex, mentionOptions.length - 1)
    : 0
  const activeMentionOption = mentionOptions[activeMentionOptionIndex]

  // ── 通过 state 插入 mention ──
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
      // 在 React 重新渲染后设置光标
      requestAnimationFrame(() => {
        el.focus()
        el.setSelectionRange(nextCursor, nextCursor)
      })
      setMentionOpen(false)
    },
    [mentionStart, mdMode],
  )

  // ── 发送 ──
  const handleSend = useCallback(() => {
    if (sendDisabled) {
      if (sendDisabledHint) showHint(sendDisabledHint)
      return
    }
    // 从 DOM 读取以避免对 inputValue 的过期闭包
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

  // ── 按键处理 ──
  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !e.nativeEvent.isComposing) {
        e.preventDefault()
        handleSend()
        return
      }
      // MD 模式下 Enter 插入换行；只有单栏模式在 Enter 时发送
      if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing && !mdMode) {
        if (mentionOpen && mentionOptions.length > 0) {
          e.preventDefault()
          insertMention(mentionOptions[activeMentionOptionIndex] ?? mentionOptions[0])
          return
        }
        e.preventDefault()
        handleSend()
        return
      }
      if (mentionOpen && mentionOptions.length > 0) {
        if (e.key === 'ArrowDown') {
          e.preventDefault()
          setActiveMentionIndex(
            (idx) => (Math.min(idx, mentionOptions.length - 1) + 1) % mentionOptions.length,
          )
        } else if (e.key === 'ArrowUp') {
          e.preventDefault()
          setActiveMentionIndex(
            (idx) =>
              (Math.min(idx, mentionOptions.length - 1) - 1 + mentionOptions.length) %
              mentionOptions.length,
          )
        } else if (e.key === 'Tab') {
          e.preventDefault()
          insertMention(mentionOptions[activeMentionOptionIndex] ?? mentionOptions[0])
        } else if (e.key === 'Escape') {
          e.preventDefault()
          setMentionOpen(false)
        }
      }
    },
    [activeMentionOptionIndex, handleSend, insertMention, mdMode, mentionOpen, mentionOptions],
  )

  // ── Markdown 预览防抖 ──
  const schedulePreview = useCallback((value: string) => {
    if (debounceTimerRef.current) clearTimeout(debounceTimerRef.current)
    debounceTimerRef.current = setTimeout(() => {
      setPreviewContent(value)
    }, PREVIEW_DEBOUNCE_MS)
  }, [])

  // ── 双栏自动增高 ──
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

  // ── 同步滚动 ──
  const syncPreviewScroll = useCallback(() => {
    const textarea = mdTextareaRef.current
    const preview = previewRef.current
    if (!textarea || !preview) return
    const scrollRatio = textarea.scrollTop / (textarea.scrollHeight - textarea.clientHeight || 1)
    lastScrollRatioRef.current = scrollRatio
    const previewMax = preview.scrollHeight - preview.clientHeight
    preview.scrollTop = scrollRatio * previewMax
  }, [])

  // ── 预览重新渲染后恢复滚动 ──
  useEffect(() => {
    if (!mdMode) return
    const preview = previewRef.current
    if (!preview) return
    const previewMax = preview.scrollHeight - preview.clientHeight
    preview.scrollTop = lastScrollRatioRef.current * previewMax
  }, [previewContent, mdMode])

  // ── 切换 MD 模式 ──
  const toggleMdMode = useCallback(() => {
    setMdMode((prev) => {
      if (!prev) setPreviewContent(inputValue)
      return !prev
    })
  }, [inputValue])

  // ── 切换到 MD 模式时聚焦 textarea ──
  useEffect(() => {
    if (mdMode) {
      requestAnimationFrame(() => mdTextareaRef.current?.focus())
    }
  }, [mdMode])

  const hasDraft = inputValue.trim().length > 0
  const canSend = !disabled && !sendDisabled && hasDraft
  const sendButtonDisabled = disabled || !hasDraft
  const sendButtonTitle =
    sendDisabled && sendDisabledHint ? sendDisabledHint : UI_ACTIONS.SEND_MESSAGE

  const renderMentionList = () => {
    if (!mentionOpen || mentionOptions.length === 0) return null

    return (
      <div
        id={mentionListId}
        role="listbox"
        className="absolute bottom-[calc(100%+8px)] left-0 z-20 max-h-64 w-[min(360px,calc(100vw-1.5rem))] min-w-[min(220px,calc(100vw-1.5rem))] overflow-auto rounded-[8px] border border-border bg-popover py-1 shadow-[var(--shadow-popup)] sm:left-4"
      >
        {mentionOptions.map((session, index) => (
          <button
            key={session.sessionId}
            id={`${mentionListId}-${session.sessionId}`}
            type="button"
            role="option"
            aria-selected={index === activeMentionOptionIndex}
            className={cn(
              'flex w-full min-w-0 items-center justify-between gap-3 px-3 py-2 text-left text-sm transition-colors hover:bg-muted',
              index === activeMentionOptionIndex ? 'bg-muted text-foreground' : 'text-foreground',
            )}
            onMouseDown={(e) => {
              e.preventDefault()
              insertMention(session)
            }}
          >
            <span className="min-w-0 truncate font-medium">{session.mentionLabel}</span>
            <span className="shrink-0 font-mono text-[11px] text-tertiary">
              {session.agentType}
            </span>
          </button>
        ))}
      </div>
    )
  }

  return (
    <div className="border-t border-border bg-background/95 backdrop-blur">
      {/* 提示 */}
      {hint && (
        <div className="px-3 pt-3 sm:px-4" role="status" aria-live="polite">
          <div className="rounded-lg bg-muted px-3 py-1.5 text-xs text-tertiary">{hint}</div>
        </div>
      )}

      {/* 工具栏 */}
      <div className={`flex items-center gap-2 px-3 pt-2 sm:px-4 ${mdMode ? 'pb-0' : ''}`}>
        <button
          type="button"
          onClick={toggleMdMode}
          aria-pressed={mdMode}
          title={UI_LABELS.MARKDOWN_MODE}
          className={`flex items-center gap-1 rounded-[5px] border px-2 py-0.5 font-mono text-[11px] font-medium transition-[background,border-color,color,transform] active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring ${
            mdMode
              ? 'border-primary-border bg-primary-soft text-primary'
              : 'border-border text-tertiary hover:bg-muted hover:text-secondary'
          }`}
        >
          <FileText className="h-3 w-3" strokeWidth={1.5} />
          {UI_LABELS.MARKDOWN_MODE}
        </button>
      </div>

      {!mdMode ? (
        /* ═══ 单栏模式 ═══ */
        <div className="relative flex items-end gap-2 px-3 py-3 sm:px-4">
          {renderMentionList()}
          <textarea
            ref={textareaRef}
            value={inputValue}
            className="flex-1 resize-none break-words rounded-[8px] border border-transparent bg-card px-3 py-2.5 text-sm text-foreground outline-none shadow-[inset_0_1px_0_rgba(255,255,255,0.03)] transition-[border-color,box-shadow] placeholder:text-tertiary hover:border-border focus:border-primary-border focus:ring-2 focus:ring-primary/15 disabled:opacity-50"
            style={{
              minHeight: MIN_INPUT_HEIGHT,
              maxHeight: MAX_INPUT_HEIGHT,
            }}
            placeholder={placeholder}
            disabled={disabled}
            aria-label={UI_LABELS.MESSAGE_EDITOR}
            aria-controls={mentionOpen ? mentionListId : undefined}
            aria-autocomplete={mentionSessions?.length ? 'list' : undefined}
            aria-expanded={mentionSessions?.length ? mentionOpen : undefined}
            aria-haspopup={mentionSessions?.length ? 'listbox' : undefined}
            aria-activedescendant={
              mentionOpen && activeMentionOption
                ? `${mentionListId}-${activeMentionOption.sessionId}`
                : undefined
            }
            rows={1}
            onChange={(e) => {
              setInputValue(e.target.value)
              adjustHeight()
              updateMentionState(e.target.value, e.target)
            }}
            onClick={(e) => updateMentionState(e.currentTarget.value, e.currentTarget)}
            onKeyDown={handleKeyDown}
          />
          <button
            type="button"
            className={cn(
              'flex w-[44px] shrink-0 items-center justify-center rounded-[6px] transition-[transform,background,opacity] active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
              canSend
                ? 'bg-primary hover:bg-primary/90'
                : sendDisabled && hasDraft
                  ? 'bg-muted hover:bg-bg-hover'
                  : 'cursor-not-allowed bg-muted opacity-50',
            )}
            style={{ height: MIN_INPUT_HEIGHT }}
            onClick={sendDisabled && onStop ? onStop : handleSend}
            disabled={sendDisabled && onStop ? isStopping : sendButtonDisabled}
            aria-disabled={sendDisabled || undefined}
            aria-label={sendDisabled && onStop ? '停止任务' : UI_ACTIONS.SEND_MESSAGE}
            title={sendDisabled && onStop ? (isStopping ? '正在停止…' : '停止任务') : sendButtonTitle}
          >
            {sendDisabled && onStop ? (
              <Square className="h-3.5 w-3.5 fill-current text-destructive" strokeWidth={1.25} />
            ) : (
              <Send
                className={cn(
                  'h-4 w-4',
                  canSend ? 'text-primary-foreground' : 'text-muted-foreground',
                )}
                strokeWidth={1.25}
              />
            )}
          </button>
        </div>
      ) : (
        /* ═══ 双栏 MD 模式 ═══ */
        <div
          className="relative flex gap-0 px-3 pb-3 pt-2 sm:px-4"
          style={{ height: mdPaneHeight }}
        >
          {renderMentionList()}
          {/* 左侧：编辑器 */}
          <div className="flex min-w-0 flex-1 flex-col overflow-hidden rounded-l-[8px] border border-border bg-card">
            <textarea
              ref={mdTextareaRef}
              value={inputValue}
              className="flex-1 resize-none px-3 py-2.5 font-mono text-[13px] leading-relaxed text-foreground outline-none transition-[box-shadow] placeholder:text-tertiary focus:ring-2 focus:ring-primary/15 disabled:opacity-50"
              placeholder={UI_PLACEHOLDERS.MESSAGE_INPUT}
              disabled={disabled}
              aria-label={UI_LABELS.MESSAGE_EDITOR}
              aria-controls={mentionOpen ? mentionListId : undefined}
              aria-autocomplete={mentionSessions?.length ? 'list' : undefined}
              aria-expanded={mentionSessions?.length ? mentionOpen : undefined}
              aria-haspopup={mentionSessions?.length ? 'listbox' : undefined}
              aria-activedescendant={
                mentionOpen && activeMentionOption
                  ? `${mentionListId}-${activeMentionOption.sessionId}`
                  : undefined
              }
              onChange={(e) => {
                setInputValue(e.target.value)
                schedulePreview(e.target.value)
                updateMentionState(e.target.value, e.target)
                // 在下一帧自动增高
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
          {/* 分隔线 */}
          <div className="hidden w-px shrink-0 bg-border sm:block" />
          {/* 右侧：预览 */}
          <div className="hidden min-w-0 flex-1 flex-col overflow-hidden border border-l-0 border-border bg-card sm:flex">
            <div
              ref={previewRef}
              className="flex-1 overflow-y-auto px-3 py-2.5 [&::-webkit-scrollbar]:w-[3px] [&::-webkit-scrollbar-thumb]:rounded-[2px] [&::-webkit-scrollbar-thumb]:bg-tertiary"
              aria-label={UI_LABELS.MARKDOWN_PREVIEW}
            >
              {inputValue.trim() ? (
                <div className="text-[13px]">
                  <MarkdownRenderer content={previewContent || inputValue} />
                </div>
              ) : (
                <p className="py-2 text-[12px] italic text-tertiary">
                  {UI_PLACEHOLDERS.MARKDOWN_PREVIEW_EMPTY}
                </p>
              )}
            </div>
          </div>
          {/* 发送按钮 */}
          <button
            type="button"
            className={cn(
              'flex shrink-0 items-center justify-center rounded-r-[8px] transition-[transform,background,opacity] active:scale-[0.96] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
              canSend
                ? 'bg-primary hover:bg-primary/90'
                : sendDisabled && hasDraft
                  ? 'bg-muted hover:bg-bg-hover'
                  : 'cursor-not-allowed bg-muted opacity-50',
            )}
            style={{ width: 44 }}
            onClick={sendDisabled && onStop ? onStop : handleSend}
            disabled={sendDisabled && onStop ? isStopping : sendButtonDisabled}
            aria-disabled={sendDisabled || undefined}
            aria-label={sendDisabled && onStop ? '停止任务' : UI_ACTIONS.SEND_MESSAGE}
            title={sendDisabled && onStop ? (isStopping ? '正在停止…' : '停止任务') : sendButtonTitle}
          >
            {sendDisabled && onStop ? (
              <Square className="h-3.5 w-3.5 fill-current text-destructive" strokeWidth={1.25} />
            ) : (
              <Send
                className={cn(
                  'h-4 w-4',
                  canSend ? 'text-primary-foreground' : 'text-muted-foreground',
                )}
                strokeWidth={1.25}
              />
            )}
          </button>
        </div>
      )}
    </div>
  )
}
