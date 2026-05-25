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
      <DiffFileEditorInner {...props} />
    </Suspense>
  )
}
