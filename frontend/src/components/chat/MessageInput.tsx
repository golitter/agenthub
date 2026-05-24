import { Send } from 'lucide-react'
import { useCallback, useRef } from 'react'

interface MessageInputProps {
  onSend: (message: string) => void
  disabled?: boolean
  placeholder?: string
}

export function MessageInput({
  onSend,
  disabled = false,
  placeholder = '输入消息...',
}: MessageInputProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 200)}px`
  }, [])

  const handleSend = useCallback(() => {
    const value = textareaRef.current?.value.trim()
    if (!value || disabled) return
    onSend(value)
    if (textareaRef.current) {
      textareaRef.current.value = ''
      textareaRef.current.style.height = '48px'
    }
  }, [onSend, disabled])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault()
        handleSend()
      }
    },
    [handleSend],
  )

  return (
    <div
      className="flex items-end gap-2 border-t px-4 py-3"
      style={{ borderColor: 'var(--divider)' }}
    >
      <textarea
        ref={textareaRef}
        className="flex-1 resize-none rounded-lg px-3 py-2.5 text-sm outline-none placeholder:text-[var(--text-tertiary)] disabled:opacity-50"
        style={{
          backgroundColor: 'var(--card)',
          color: 'var(--text-primary)',
          minHeight: 48,
          maxHeight: 200,
        }}
        placeholder={placeholder}
        disabled={disabled}
        rows={1}
        onChange={adjustHeight}
        onKeyDown={handleKeyDown}
      />
      <button
        className="flex h-[48px] w-[40px] shrink-0 items-center justify-center rounded-lg transition-colors disabled:opacity-40"
        style={{ backgroundColor: 'var(--color-brand)' }}
        onClick={handleSend}
        disabled={disabled}
      >
        <Send className="h-4 w-4" style={{ color: 'var(--text-primary)' }} strokeWidth={1.25} />
      </button>
    </div>
  )
}
