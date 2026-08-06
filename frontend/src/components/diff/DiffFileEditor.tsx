import { lazy, Suspense } from 'react'

const DiffFileEditorInner = lazy(() => import('./DiffFileEditorInner'))

interface DiffFileEditorProps {
  oldContent: string
  newContent: string
  fileName: string
  onSave: (content: string) => void
  onCancel: () => void
}

export function DiffFileEditor(props: DiffFileEditorProps) {
  return (
    <Suspense
      fallback={
        <div className="flex items-center justify-center py-8 text-xs text-muted-foreground">
          加载编辑器...
        </div>
      }
    >
      {/* 以 fileName 为 key 强制在切换文件时重挂载 Inner，确保编辑器内容
          从对应文件的 newContent 重新初始化，避免复用实例导致内容错位。 */}
      <DiffFileEditorInner key={props.fileName} {...props} />
    </Suspense>
  )
}
