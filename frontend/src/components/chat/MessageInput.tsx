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
      if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
        e.preventDefault()
        handleSend()
      }
    },
    [handleSend],
  )

  return (
    <div className="flex items-end gap-2 border-t border-border px-4 py-3">
      <textarea
        ref={textareaRef}
        className="flex-1 resize-none rounded-lg bg-card px-3 py-2.5 text-sm text-foreground outline-none placeholder:text-tertiary disabled:opacity-50"
        style={{
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
        className="flex h-[48px] w-[40px] shrink-0 items-center justify-center rounded-lg bg-primary transition-colors disabled:opacity-40"
        onClick={handleSend}
        disabled={disabled}
      >
        <Send className="h-4 w-4 text-primary-foreground" strokeWidth={1.25} />
      </button>
    </div>
  )
}
