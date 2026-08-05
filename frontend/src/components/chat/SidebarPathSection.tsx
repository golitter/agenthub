import { ChevronRight, FolderOpen, FolderSync } from 'lucide-react'
import { useId, useState } from 'react'

import { UI_ACTIONS, UI_ERRORS, UI_LABELS, UI_MESSAGES } from '@/lib/ui-text'

import { showCopyToast } from './SidebarActions'
import { useCollapsible } from './useCollapsible'

export function SidebarPathSection({ repoPath, taskId }: { repoPath: string; taskId: string }) {
  const [pathsOpen, togglePaths] = useCollapsible('paths', false)
  const [copyError, setCopyError] = useState(false)
  const pathsBodyId = useId()
  const taskPath = `${repoPath.replace(/\/[^/]+$/, '')}/worktrees/${taskId}`

  const copyPath = async (path: string) => {
    try {
      await navigator.clipboard.writeText(path)
      setCopyError(false)
      showCopyToast()
    } catch {
      setCopyError(true)
    }
  }

  return (
    <div className="border-b border-sidebar-border px-4 py-3">
      <button
        type="button"
        className="flex w-full items-center gap-1 rounded-[4px] text-[11px] font-medium uppercase tracking-wider text-tertiary transition-colors hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
        onClick={togglePaths}
        aria-expanded={pathsOpen}
        aria-controls={pathsBodyId}
      >
        <ChevronRight
          className={`h-3 w-3 transition-transform ${pathsOpen ? 'rotate-90' : ''}`}
          strokeWidth={1.25}
          aria-hidden="true"
        />
        {UI_LABELS.PATH_INFO}
      </button>
      {pathsOpen && (
        <div id={pathsBodyId} className="mt-2 flex flex-col gap-2">
          {copyError && (
            <p className="text-xs text-destructive" role="alert">
              {UI_ERRORS.COPY_FAILED}
            </p>
          )}
          {/* 仓库路径 */}
          <div>
            <span className="mb-0.5 block text-[11px] text-tertiary">{UI_LABELS.REPO_PATH}</span>
            <button
              type="button"
              className="flex w-full select-none truncate rounded-md bg-bg-subtle px-2.5 py-1.5 text-left text-xs text-muted-foreground transition-[background-color,color,transform] hover:bg-bg-hover hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring active:translate-y-px"
              title={`${repoPath} — ${UI_MESSAGES.CLICK_TO_COPY}`}
              aria-label={`${UI_ACTIONS.COPY}${UI_LABELS.REPO_PATH}: ${repoPath}`}
              onClick={() => copyPath(repoPath)}
            >
              <FolderOpen
                className="mr-1.5 h-3.5 w-3.5 shrink-0 text-tertiary"
                strokeWidth={1.25}
                aria-hidden="true"
              />
              <span className="truncate">{repoPath}</span>
            </button>
          </div>
          {/* 任务路径 —— worktrees 位于 <repo_parent>/worktrees/<taskId> */}
          <div>
            <span className="mb-0.5 block text-[11px] text-tertiary">{UI_LABELS.TASK_PATH}</span>
            <button
              type="button"
              className="flex w-full select-none truncate rounded-md bg-bg-subtle px-2.5 py-1.5 text-left text-xs text-muted-foreground transition-[background-color,color,transform] hover:bg-bg-hover hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring active:translate-y-px"
              title={`${taskPath} — ${UI_MESSAGES.CLICK_TO_COPY}`}
              aria-label={`${UI_ACTIONS.COPY}${UI_LABELS.TASK_PATH}: ${taskPath}`}
              onClick={() => copyPath(taskPath)}
            >
              <FolderSync
                className="mr-1.5 h-3.5 w-3.5 shrink-0 text-tertiary"
                strokeWidth={1.25}
                aria-hidden="true"
              />
              <span className="truncate">{taskPath}</span>
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
