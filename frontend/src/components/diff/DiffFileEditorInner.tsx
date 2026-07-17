import { oneDark } from '@codemirror/theme-one-dark'
import CodeMirror from '@uiw/react-codemirror'
import { useCallback, useEffect, useState } from 'react'

import { UI_ACTIONS, UI_STATUS } from '@/lib/ui-text'

interface DiffFileEditorProps {
  oldContent: string
  newContent: string
  fileName: string
  onSave: (content: string) => void
  onCancel: () => void
}

type EditorExtension = NonNullable<React.ComponentProps<typeof CodeMirror>['extensions']>[number]

async function loadLanguageExtension(fileName: string): Promise<EditorExtension | null> {
  const ext = fileName.split('.').pop()?.toLowerCase()

  switch (ext) {
    case 'js':
    case 'jsx':
    case 'ts':
    case 'tsx': {
      const { javascript } = await import('@codemirror/lang-javascript')
      return javascript({ jsx: true, typescript: ext.startsWith('t') })
    }
    case 'py': {
      const { python } = await import('@codemirror/lang-python')
      return python()
    }
    case 'css':
    case 'scss': {
      const { css } = await import('@codemirror/lang-css')
      return css()
    }
    case 'html':
    case 'htm': {
      const { html } = await import('@codemirror/lang-html')
      return html()
    }
    case 'json': {
      const { json } = await import('@codemirror/lang-json')
      return json()
    }
    default:
      return null
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
  const [extensions, setExtensions] = useState<EditorExtension[]>([])

  useEffect(() => {
    let active = true
    loadLanguageExtension(fileName)
      .then((extension) => {
        if (!active) return
        setExtensions(extension ? [extension] : [])
      })
      .catch(() => {
        if (active) setExtensions([])
      })

    return () => {
      active = false
    }
  }, [fileName])

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
      <div className="max-h-96 flex-1 overflow-auto">
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
          type="button"
          onClick={onCancel}
          className="rounded-md px-2 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        >
          {UI_ACTIONS.CANCEL}
        </button>
        <button
          type="button"
          onClick={handleSave}
          disabled={saving || modifiedContent === newContent}
          className="rounded-md bg-primary px-2 py-1 text-xs text-primary-foreground transition-colors hover:bg-primary/90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring disabled:opacity-50"
        >
          {saving ? UI_STATUS.SAVING : '保存修改'}
        </button>
      </div>
    </div>
  )
}
