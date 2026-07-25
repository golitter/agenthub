# Diff Viewer — 多文件 Diff 查看与编辑

## 实现了什么

可编辑的多文件 Diff 查看器，支持 tab 切换文件、内联 CodeMirror 编辑、react-diff-view 统一视图渲染。由 `DiffCard` 作为 Smart 组件编排，内部拆分为 Header、FileInfo、Tab、View、Editor 五个子组件。

## 怎么实现的

### DiffHeader (`src/components/diff/DiffHeader.tsx`)

Diff 卡片头部组件，显示变更摘要（文件数、+/- 行数）、视图模式切换（Split / Unified）、操作按钮（编辑、接受变更、拒绝变更）以及定稿状态 Badge：

```tsx
export function DiffHeader({
  summary, viewType, onViewTypeChange, snapshotStatus, isSettled, hasSession,
  onEdit, onAccept, onReject, actionStatus,
}: DiffHeaderProps) {
  // summary: { filesChanged, additions, deletions }
  // 视图模式切换：Split (Columns2 图标) / Unified (Rows 图标)
  // 定稿状态 Badge：committed / reverted / cancelled
  // 操作按钮：编辑（非定稿 + 有 session）、接受/拒绝（非定稿）
}
```

定稿状态（committed/reverted/cancelled）显示对应 Badge 并隐藏操作按钮。操作按钮在 actionStatus 非 idle 时禁用，显示"提交中..."/"撤销中..."状态文字。

### DiffFileInfo (`src/components/diff/DiffFileInfo.tsx`)

单文件信息条，显示文件路径、变更类型 Badge 和 +/- 行数统计（使用语义化 `text-success` / `text-error` token，随主题切换）：

```tsx
export function DiffFileInfo({ file }: DiffFileInfoProps) {
  return (
    <div className="flex items-center gap-2 border-b border-border bg-muted/20 px-3 py-1 text-xs">
      <span className="truncate font-mono text-[11px]">{file.newPath}</span>
      <ChangeTypeBadge type={file.type} />
      <span className="ml-auto shrink-0 text-[11px]">
        {file.additions > 0 && <span className="text-success">+{file.additions}</span>}
        {file.deletions > 0 && <span className="text-error">-{file.deletions}</span>}
      </span>
    </div>
  )
}
```

### DiffFileTabs (`src/components/diff/DiffFileTabs.tsx`)

多文件 tab 切换组件，单文件时不渲染：

```tsx
export function DiffFileTabs({ files, activeIndex, onSelect }: DiffFileTabsProps) {
  if (files.length <= 1) return null

  return (
    <div className="flex overflow-x-auto border-b border-border bg-muted/30">
      {files.map((file, i) => (
        <button
          onClick={() => onSelect(i)}
          className={clsx(
            'flex shrink-0 items-center gap-1.5 border-b-2 px-3 py-1.5 text-xs transition-colors',
            activeIndex === i
              ? 'border-primary text-foreground'
              : 'border-transparent text-muted-foreground hover:text-foreground',
          )}
        >
          <span className="truncate max-w-32">{getFileName(file.newPath)}</span>
          <span className="shrink-0 text-[10px]">
            {file.additions > 0 && <span className="text-success">+{file.additions}</span>}
            {file.deletions > 0 && <span className="text-error">-{file.deletions}</span>}
          </span>
        </button>
      ))}
    </div>
  )
}
```

### DiffFileView (`src/components/diff/DiffFileView.tsx`)

基于 `react-diff-view` 的视图渲染，支持 Split / Unified 两种模式，通过 `viewType` 属性切换：

```tsx
export function DiffFileView({ file, viewType = 'split' }: DiffFileViewProps) {
  return (
    <Diff viewType={viewType} diffType={file.type} hunks={file.hunks}>
      {(hunks) => hunks.map((hunk) => <Hunk key={hunk.content} hunk={hunk} />)}
    </Diff>
  )
}
```

### DiffFileEditor (`src/components/diff/DiffFileEditor.tsx`)

懒加载编辑器外壳，使用 `React.lazy` + `Suspense` 按需加载 CodeMirror，加载中显示"加载编辑器..."占位：

```tsx
const DiffFileEditorInner = lazy(() => import('./DiffFileEditorInner'))
```

### DiffFileEditorInner (`src/components/diff/DiffFileEditorInner.tsx`)

CodeMirror 编辑器实现，根据文件扩展名**动态加载**语法高亮扩展（js/jsx/ts/tsx/py/css/scss/html/htm/json），使用 `oneDark` 主题：

```typescript
async function getLangExtension(fileName: string) {
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
      return []
  }
}
```

底部操作栏提供"取消"和"保存修改"按钮，保存时调用 `onSave` 回调（DiffCard 中为 `PUT /api/session/:sessionId/files/:path`）。
