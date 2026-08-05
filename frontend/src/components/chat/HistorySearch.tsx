import { Search } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'

import { MESSAGE_ROLES } from '@/lib/constants'
import { UI_PLACEHOLDERS } from '@/lib/ui-text'
import type { ChatMessage } from '@/stores/chat'
import { useChatStore } from '@/stores/chat'

const MAX_RESULTS = 50
const MESSAGE_SCROLL_EVENT = 'agenthub:scroll-message'

interface SearchResult {
  message: ChatMessage
  snippet: string
  matchIndex: number
}

function blockSearchContent(block: NonNullable<ChatMessage['blocks']>[number]): string {
  return 'content' in block && typeof block.content === 'string' ? block.content : ''
}

function searchMessages(messages: ChatMessage[], query: string): SearchResult[] {
  const lower = query.toLowerCase()
  const results: SearchResult[] = []

  for (const msg of messages) {
    // 检查 message.content
    const contentIdx = msg.content.toLowerCase().indexOf(lower)
    if (contentIdx >= 0) {
      results.push({ message: msg, snippet: msg.content, matchIndex: contentIdx })
      continue
    }

    // 检查 block.content
    if (msg.blocks) {
      for (const block of msg.blocks) {
        const blockContent = blockSearchContent(block)
        const blockIdx = blockContent.toLowerCase().indexOf(lower)
        if (blockIdx >= 0) {
          results.push({ message: msg, snippet: blockContent, matchIndex: blockIdx })
          break
        }
      }
    }
  }

  // 按时间倒序排序
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
      <mark key={key++} className="rounded-sm bg-warning-soft px-0.5 text-warning">
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
  const debounceRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

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
      // 在截取前统计匹配总数
      const lower = q.toLowerCase()
      let total = 0
      for (const msg of msgs) {
        if (msg.content.toLowerCase().includes(lower)) total++
        else if (msg.blocks) {
          for (const block of msg.blocks) {
            const bc = blockSearchContent(block)
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

  // 点击外部时关闭下拉框
  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) {
        setShowDropdown(false)
      }
    }
    document.addEventListener('mousedown', handleClick)
    return () => {
      document.removeEventListener('mousedown', handleClick)
      if (debounceRef.current) clearTimeout(debounceRef.current)
    }
  }, [])

  const scrollToMessage = useCallback((msgId: string) => {
    setShowDropdown(false)
    setQuery('')
    // MessageList 持有滚动容器，且可能对较旧的行做虚拟化，
    // 因此目标元素可能尚未存在于 DOM 中。
    window.dispatchEvent(
      new CustomEvent(MESSAGE_SCROLL_EVENT, {
        detail: { sessionId, messageId: msgId },
      }),
    )
  }, [sessionId])

  return (
    <div className="shrink-0 px-4 py-3" ref={wrapperRef}>
      <div className="relative">
        {/* 搜索图标 */}
        <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-tertiary">
          <Search className="h-3.5 w-3.5" strokeWidth={1.25} />
        </span>
        <input
          className="w-full rounded-md border border-border bg-accent py-2 pl-9 pr-3 text-xs text-foreground outline-none transition-[transform,opacity] placeholder:text-tertiary focus:border-ring"
          placeholder={UI_PLACEHOLDERS.SEARCH_HISTORY}
          value={query}
          onChange={(e) => handleInput(e.target.value)}
          onFocus={() => query.length >= 1 && setShowDropdown(true)}
        />

        {/* 下拉框 */}
        {showDropdown && results.length > 0 && (
          <div className="absolute left-0 right-0 top-full z-50 mt-1 max-h-[300px] overflow-y-auto rounded-md border border-sidebar-border bg-popover p-1.5 shadow-lg">
            {results.map((r) => (
              <button
                key={r.message.id}
                type="button"
                className="w-full rounded-sm px-2.5 py-2 text-left transition-[transform,opacity] hover:bg-bg-hover"
                onClick={() => scrollToMessage(r.message.id)}
              >
                <div className="mb-0.5 flex items-center gap-1.5">
                  <span className="text-[11px] font-medium text-text-secondary">
                    {r.message.agentName ??
                      (r.message.role === MESSAGE_ROLES.USER ? 'You' : 'Agent')}
                  </span>
                  <span className="ml-auto text-[11px] text-tertiary">
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
          <div className="absolute left-0 right-0 top-full z-50 mt-1 rounded-md border border-sidebar-border bg-popover p-3 text-center text-xs text-tertiary shadow-lg">
            没有找到匹配的消息
          </div>
        )}
      </div>
    </div>
  )
}
