import { useCallback, useEffect, useRef, useState } from 'react'

import type { ChatMessage } from '@/stores/chat'
import { useChatStore } from '@/stores/chat'

const MAX_RESULTS = 50

interface SearchResult {
  message: ChatMessage
  snippet: string
  matchIndex: number
}

function searchMessages(messages: ChatMessage[], query: string): SearchResult[] {
  const lower = query.toLowerCase()
  const results: SearchResult[] = []

  for (const msg of messages) {
    // Check message.content
    const contentIdx = msg.content.toLowerCase().indexOf(lower)
    if (contentIdx >= 0) {
      results.push({ message: msg, snippet: msg.content, matchIndex: contentIdx })
      continue
    }

    // Check block.content
    if (msg.blocks) {
      for (const block of msg.blocks) {
        const blockContent =
          block.type === 'text'
            ? ((block as { content?: string }).content ?? '')
            : block.type === 'code'
              ? ((block as { content?: string }).content ?? '')
              : ''
        const blockIdx = blockContent.toLowerCase().indexOf(lower)
        if (blockIdx >= 0) {
          results.push({ message: msg, snippet: blockContent, matchIndex: blockIdx })
          break
        }
      }
    }
  }

  // Sort by time descending
  return results.sort((a, b) => b.message.timestamp - a.message.timestamp).slice(0, MAX_RESULTS)
}

function HighlightedText({ text, query }: { text: string; query: string }) {
  if (!query) return <>{text}</>
  const parts: React.ReactNode[] = []
  let remaining = text
  const lowerQ = query.toLowerCase()
  let key = 0

  while (remaining.length > 0) {
    const idx = remaining.toLowerCase().indexOf(lowerQ)
    if (idx < 0) {
      parts.push(remaining)
      break
    }
    if (idx > 0) parts.push(remaining.slice(0, idx))
    parts.push(
      <mark key={key++} className="rounded-sm bg-warning-soft px-0.5 text-color-warning">
        {remaining.slice(idx, idx + query.length)}
      </mark>,
    )
    remaining = remaining.slice(idx + query.length)
  }
  return <>{parts}</>
}

function truncateSnippet(text: string, matchIndex: number, maxLen = 80): string {
  if (text.length <= maxLen) return text
  const start = Math.max(0, matchIndex - 20)
  const end = Math.min(text.length, start + maxLen)
  const prefix = start > 0 ? '…' : ''
  const suffix = end < text.length ? '…' : ''
  return prefix + text.slice(start, end) + suffix
}

function formatTime(ts: number): string {
  const d = new Date(ts)
  const now = new Date()
  const isToday = d.toDateString() === now.toDateString()
  if (isToday) {
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })
}

interface HistorySearchProps {
  sessionId: string
}

export function HistorySearch({ sessionId }: HistorySearchProps) {
  const [query, setQuery] = useState('')
  const [showDropdown, setShowDropdown] = useState(false)
  const [results, setResults] = useState<SearchResult[]>([])
  const [totalMatchCount, setTotalMatchCount] = useState(0)
  const wrapperRef = useRef<HTMLDivElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout>>()

  const doSearch = useCallback(
    (q: string) => {
      if (q.length < 1) {
        setResults([])
        setTotalMatchCount(0)
        setShowDropdown(false)
        return
      }
      const msgs = useChatStore.getState().sessions[sessionId]?.messages ?? []
      const all = searchMessages(msgs, q)
      // Count total matches before slicing
      const lower = q.toLowerCase()
      let total = 0
      for (const msg of msgs) {
        if (msg.content.toLowerCase().includes(lower)) total++
        else if (msg.blocks) {
          for (const block of msg.blocks) {
            const bc =
              block.type === 'text'
                ? ((block as { content?: string }).content ?? '')
                : block.type === 'code'
                  ? ((block as { content?: string }).content ?? '')
                  : ''
            if (bc.toLowerCase().includes(lower)) {
              total++
              break
            }
          }
        }
      }
      setResults(all)
      setTotalMatchCount(total)
      setShowDropdown(true)
    },
    [sessionId],
  )

  const handleInput = useCallback(
    (value: string) => {
      setQuery(value)
      if (debounceRef.current) clearTimeout(debounceRef.current)
      debounceRef.current = setTimeout(() => doSearch(value), 300)
    },
    [doSearch],
  )

  // Close dropdown on click outside
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setShowDropdown(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [])

  const scrollToMessage = useCallback((msgId: string) => {
    setShowDropdown(false)
    setQuery('')
    const el = document.querySelector(`[data-message-id="${msgId}"]`)
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' })
      el.classList.add('animate-search-highlight')
      setTimeout(() => el.classList.remove('animate-search-highlight'), 800)
    }
  }, [])

  return (
    <div className="shrink-0 px-4 py-3" ref={wrapperRef}>
      <div className="relative">
        {/* Search icon */}
        <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-tertiary">
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={1.25}
            strokeLinecap="round"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.35-4.35" />
          </svg>
        </span>
        <input
          className="w-full rounded-md border border-border bg-accent py-2 pl-9 pr-3 text-xs text-foreground outline-none transition-colors placeholder:text-tertiary focus:border-ring"
          placeholder="搜索历史消息..."
          value={query}
          onChange={(e) => handleInput(e.target.value)}
          onFocus={() => query.length >= 1 && setShowDropdown(true)}
        />

        {/* Dropdown */}
        {showDropdown && results.length > 0 && (
          <div className="absolute left-0 right-0 top-full z-50 mt-1 max-h-[300px] overflow-y-auto rounded-md border border-sidebar-border bg-popover p-1.5 shadow-shadow-lg">
            {results.map((r) => (
              <button
                key={r.message.id}
                type="button"
                className="w-full rounded-sm px-2.5 py-2 text-left transition-colors hover:bg-bg-hover"
                onClick={() => scrollToMessage(r.message.id)}
              >
                <div className="mb-0.5 flex items-center gap-1.5">
                  <span className="text-[11px] font-medium text-secondary">
                    {r.message.agentName ?? (r.message.role === 'user' ? 'You' : 'Agent')}
                  </span>
                  <span className="ml-auto text-[10px] text-tertiary">
                    {formatTime(r.message.timestamp)}
                  </span>
                </div>
                <div className="text-xs text-foreground">
                  <HighlightedText text={truncateSnippet(r.snippet, r.matchIndex)} query={query} />
                </div>
              </button>
            ))}
            {totalMatchCount > MAX_RESULTS && (
              <div className="px-2.5 py-1.5 text-center text-[11px] text-tertiary">
                还有 {totalMatchCount - MAX_RESULTS} 条匹配结果
              </div>
            )}
          </div>
        )}
        {showDropdown && query.length >= 1 && results.length === 0 && (
          <div className="absolute left-0 right-0 top-full z-50 mt-1 rounded-md border border-sidebar-border bg-popover p-3 text-center text-xs text-tertiary shadow-shadow-lg">
            没有找到匹配的消息
          </div>
        )}
      </div>
    </div>
  )
}
