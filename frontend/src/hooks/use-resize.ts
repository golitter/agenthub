import { useCallback, useEffect, useRef, useState } from 'react'

interface UseResizeOptions {
  /** localStorage key for persisting state */
  storageKey: string
  /** Default expanded width (px) */
  initialWidth?: number
  /** Minimum width when expanded (px) */
  minWidth?: number
  /** Maximum width when expanded (px) */
  maxWidth?: number
  /** Width below this snaps to collapsed (px) */
  collapseThreshold?: number
}

interface UseResizeReturn {
  /** Current width in px (0 when collapsed) */
  width: number
  /** Whether sidebar is fully collapsed */
  isCollapsed: boolean
  /** Whether user is currently dragging */
  isDragging: boolean
  /** Attach to resize handle's onMouseDown */
  handleMouseDown: (e: React.MouseEvent) => void
  /** Keyboard controls for an accessible resize separator */
  handleKeyDown: (e: React.KeyboardEvent) => void
  /** Expand to last known width */
  expand: () => void
  /** Collapse to 0 */
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
      // Guard against 0 (stale value from before the fix)
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

  // Persist to localStorage
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

      const nextWidth = width + (e.key === 'ArrowLeft' ? 16 : -16)
      setWidth(Math.min(maxWidth, Math.max(minWidth, nextWidth)))
      setIsCollapsed(false)
    },
    [maxWidth, minWidth, width],
  )

  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (!draggingRef.current) return

      // Dragging left = positive delta (width decreases)
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
      document.body.style.cursor = ''
      document.body.style.userSelect = ''
    }

    document.addEventListener('mousemove', handleMouseMove)
    document.addEventListener('mouseup', handleMouseUp)
    return () => {
      document.removeEventListener('mousemove', handleMouseMove)
      document.removeEventListener('mouseup', handleMouseUp)
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
