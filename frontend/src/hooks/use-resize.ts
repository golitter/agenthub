import { useCallback, useEffect, useRef, useState } from 'react'

interface UseResizeOptions {
  /** 用于持久化状态的 localStorage key */
  storageKey: string
  /** 默认展开宽度（px） */
  initialWidth?: number
  /** 展开时的最小宽度（px） */
  minWidth?: number
  /** 展开时的最大宽度（px） */
  maxWidth?: number
  /** 低于此宽度则吸附为折叠（px） */
  collapseThreshold?: number
}

interface UseResizeReturn {
  /** 当前宽度，单位 px（折叠时为 0） */
  width: number
  /** 侧栏是否已完全折叠 */
  isCollapsed: boolean
  /** 用户是否正在拖拽 */
  isDragging: boolean
  /** 绑定到 resize 把手的 onMouseDown */
  handleMouseDown: (e: React.MouseEvent) => void
  /** 用于无障碍分隔符的键盘控制 */
  handleKeyDown: (e: React.KeyboardEvent) => void
  /** 展开到上一次已知的宽度 */
  expand: () => void
  /** 折叠为 0 */
  collapse: () => void
}

const LS_WIDTH_SUFFIX = '-width'
const LS_COLLAPSED_SUFFIX = '-collapsed'

export function useResize({
  storageKey,
  initialWidth = 280,
  minWidth = 200,
  maxWidth = 400,
  collapseThreshold = 60,
}: UseResizeOptions): UseResizeReturn {
  const [width, setWidth] = useState(() => {
    try {
      const stored = localStorage.getItem(storageKey + LS_WIDTH_SUFFIX)
      const parsed = stored ? Number(stored) : initialWidth
      // 防御 0（修复前遗留的旧值）
      return parsed > 0 ? parsed : initialWidth
    } catch {
      return initialWidth
    }
  })

  const [isCollapsed, setIsCollapsed] = useState(() => {
    try {
      return localStorage.getItem(storageKey + LS_COLLAPSED_SUFFIX) === 'true'
    } catch {
      return false
    }
  })

  const [isDragging, setIsDragging] = useState(false)
  const draggingRef = useRef(false)
  const startXRef = useRef(0)
  const startWidthRef = useRef(0)

  // 持久化到 localStorage
  useEffect(() => {
    try {
      localStorage.setItem(storageKey + LS_WIDTH_SUFFIX, String(width))
    } catch {
      /* ignore */
    }
  }, [storageKey, width])

  useEffect(() => {
    try {
      localStorage.setItem(storageKey + LS_COLLAPSED_SUFFIX, String(isCollapsed))
    } catch {
      /* ignore */
    }
  }, [storageKey, isCollapsed])

  const expand = useCallback(() => {
    setIsCollapsed(false)
  }, [])

  const collapse = useCallback(() => {
    setIsCollapsed(true)
  }, [])

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault()
      draggingRef.current = true
      setIsDragging(true)
      startXRef.current = e.clientX
      startWidthRef.current = width

      document.body.style.cursor = 'col-resize'
      document.body.style.userSelect = 'none'
    },
    [width],
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return
      e.preventDefault()

      if (e.key === 'Home') {
        setIsCollapsed(true)
        return
      }
      if (e.key === 'End') {
        setWidth(maxWidth)
        setIsCollapsed(false)
        return
      }

      // ArrowRight 展开（变宽），ArrowLeft 收缩（变窄） — 与鼠标拖拽方向
      // 以及 WAI-ARIA 分隔符约定一致。
      const nextWidth = width + (e.key === 'ArrowLeft' ? -16 : 16)
      setWidth(Math.min(maxWidth, Math.max(minWidth, nextWidth)))
      setIsCollapsed(false)
    },
    [maxWidth, minWidth, width],
  )

  useEffect(() => {
    const restoreBodyState = () => {
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }

    const handleMouseMove = (e: MouseEvent) => {
      if (!draggingRef.current) return

      // 向左拖拽 = 正的 delta（宽度减小）
      const delta = startXRef.current - e.clientX
      const newWidth = Math.min(maxWidth, Math.max(0, startWidthRef.current + delta))

      if (newWidth < collapseThreshold) {
        setIsCollapsed(true)
      } else {
        const clamped = Math.max(minWidth, newWidth)
        setWidth(clamped)
        setIsCollapsed(false)
      }
    }

    const handleMouseUp = () => {
      if (!draggingRef.current) return
      draggingRef.current = false
      setIsDragging(false)
      restoreBodyState()
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
      if (draggingRef.current) {
        draggingRef.current = false
        restoreBodyState()
      }
    }
  }, [minWidth, maxWidth, collapseThreshold])

  return {
    width: isCollapsed ? 0 : width,
    isCollapsed,
    isDragging,
    handleMouseDown,
    handleKeyDown,
    expand,
    collapse,
  }
}
