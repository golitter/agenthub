import { css } from '@codemirror/lang-css'
import { html } from '@codemirror/lang-html'
import { javascript } from '@codemirror/lang-javascript'
import { json } from '@codemirror/lang-json'
import { python } from '@codemirror/lang-python'
import { oneDark } from '@codemirror/theme-one-dark'
import CodeMirror from '@uiw/react-codemirror'
import { useCallback, useMemo, useState } from 'react'

interface DiffFileEditorProps {
  oldContent: string
  newContent: string
  fileName: string
  onSave: (content: string) => void
  onCancel: () => void
}

function getLangExtension(fileName: string) {
  const ext = fileName.split('.').pop()?.toLowerCase()
  switch (ext) {
    case 'js':
    case 'jsx':
    case 'ts':
    case 'tsx':
      return javascript({ jsx: true, typescript: ext?.startsWith('t') })
    case 'py':
      return python()
    case 'css':
    case 'scss':
      return css()
    case 'html':
    case 'htm':
      return html()
    case 'json':
      return json()
    default:
      return []
  }
}

export default function DiffFileEditorInner({
  newContent,
  fileName,
  onSave,
  onCancel,
}: DiffFileEditorProps) {
  const [modifiedContent, setModifiedContent] = useState(newContent)
  const [saving, setSaving] = useState(false)

  const extensions = useMemo(() => [getLangExtension(fileName)], [fileName])

  const handleSave = useCallback(async () => {
    setSaving(true)
    try {
      await onSave(modifiedContent)
    } finally {
      setSaving(false)
    }
  }, [modifiedContent, onSave])

  return (
    <div className="flex flex-col">
      <div className="flex-1 overflow-auto" style={{ maxHeight: '24rem' }}>
        <CodeMirror
          value={modifiedContent}
          height="100%"
          theme={oneDark}
          extensions={extensions}
          onChange={setModifiedContent}
        />
      </div>
      <div className="flex items-center justify-end gap-2 border-t border-border px-3 py-1.5">
        <button
          onClick={onCancel}
          className="rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
        >
          取消
        </button>
        <button
          onClick={handleSave}
          disabled={saving || modifiedContent === newContent}
          className="rounded-md bg-primary px-2 py-1 text-xs text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
        >
          {saving ? '保存中...' : '保存修改'}
        </button>
      </div>
    </div>
  )
}
