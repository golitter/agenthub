import { useEffect, useState } from "react"
import type { KeyboardEvent } from "react"
import { contentForFile, updateFileDraft } from "../draft"
import type { ConfigFile, Draft } from "../schema"

interface Props {
  files: ConfigFile[]
  draft: Draft
  disabled: boolean
  onDraft: (draft: Draft) => void
}

function lineCount(content: string): number {
  return content ? content.split("\n").length : 0
}

export function TemplateEditor({ files, draft, disabled, onDraft }: Props) {
  const [activeId, setActiveId] = useState(files[0]?.id || "")
  const file = files.find((item) => item.id === activeId) || files[0]

  useEffect(() => {
    if (file && !files.some((item) => item.id === activeId)) setActiveId(file.id)
  }, [activeId, file, files])

  if (!file) return <div className="empty-state">当前环境没有发现 example/actual 配置对。</div>

  const actual = contentForFile(draft, file)
  const changed = Boolean(draft[file.id])

  function update(content: string) {
    onDraft(updateFileDraft(draft, file, content))
  }

  function indent(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key !== "Tab") return
    event.preventDefault()
    const target = event.currentTarget
    const start = target.selectionStart
    const end = target.selectionEnd
    update(`${actual.slice(0, start)}  ${actual.slice(start)}`)
    requestAnimationFrame(() => target.setSelectionRange(start + 2, end + 2))
  }

  return (
    <section className="editor-panel" aria-label="example 与 actual 文件编辑器">
      <header className="file-switcher" role="tablist" aria-label="配置文件">
        {files.map((item) => (
          <button key={item.id} type="button" role="tab" aria-selected={item.id === file.id} disabled={disabled} className={item.id === file.id ? "is-active" : ""} onClick={() => setActiveId(item.id)}>
            <strong>{item.path}</strong>
            <span>{item.kind.toUpperCase()} · {draft[item.id] ? "已修改" : item.exists ? "文件存在" : "待创建"}</span>
          </button>
        ))}
      </header>

      <div className="file-pair-toolbar">
        <div><span className="format-mark">{file.kind.toUpperCase()}</span><strong>{file.examplePath}</strong><small>example · 只读</small></div>
        <span className="pair-arrow" aria-hidden="true">→</span>
        <div><span className={`format-mark ${changed ? "changed" : ""}`}>{changed ? "CHANGED" : file.kind.toUpperCase()}</span><strong>{file.path}</strong><small>{file.exists ? "actual · 可编辑" : "actual 不存在 · 保存时创建"}</small></div>
        <div className="file-tools">
          <button className="text-button" type="button" disabled={disabled || actual === file.exampleContent} onClick={() => update(file.exampleContent)}>复制 example</button>
          <button className="text-button" type="button" disabled={disabled || !changed} onClick={() => update(file.actualContent)}>还原</button>
        </div>
      </div>

      <div className="split-editor">
        <article className="code-pane example-pane">
          <header><span>EXAMPLE / READ ONLY</span><small>{lineCount(file.exampleContent)} lines</small></header>
          <textarea value={file.exampleContent} readOnly spellCheck={false} aria-label={`${file.examplePath} 只读内容`} />
        </article>
        <article className="code-pane actual-pane">
          <header><span>ACTUAL / EDITABLE</span><small>{lineCount(actual)} lines</small></header>
          <textarea value={actual} disabled={disabled} spellCheck={false} aria-label={`${file.path} 可编辑内容`} onChange={(event) => update(event.target.value)} onKeyDown={indent} />
        </article>
      </div>
    </section>
  )
}
