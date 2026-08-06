import { ChevronRight, Terminal as TerminalIcon } from 'lucide-react'
import { useCallback, useEffect, useId, useRef, useState } from 'react'

import { UI_ACTIONS, UI_LABELS, UI_PLACEHOLDERS, UI_STATUS } from '@/lib/ui-text'

import type { CommandResult, GitGraphData, TerminalPanelProps } from './git-graph-types'
import { useCollapsible } from './RightSidebar'

// ─── 辅助函数 ────────────────────────────────────────────────────

function escapeHtml(str: string): string {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

// 终端历史行的全局自增 id 序列，用于生成稳定的 React key，
// 避免清屏后再追加时因 index 复用导致 dangerouslySetInnerHTML 内容错位。
// 模块级声明：在 render 之外自增，不违反 react-hooks/refs 规则。
let terminalLineSeq = 0
function nextLineId(): number {
  return ++terminalLineSeq
}
function makeLine(html: string): { id: number; html: string } {
  return { id: nextLineId(), html }
}

// ─── 组件 ──────────────────────────────────────────────────

export function TerminalPanel({
  currentBranch,
  availableBranches,
  gitGraphData,
  onBranchChange,
  branchLabels,
}: TerminalPanelProps) {
  const [open, toggle] = useCollapsible('terminal', true)
  // history 每条记录携带稳定唯一 id（来自模块级 nextLineId），作为 React key，
  // 避免清屏后再追加时因 index 复用导致 dangerouslySetInnerHTML 内容错位/残留。
  const [history, setHistory] = useState<{ id: number; html: string }[]>(() => [
    makeLine('<span class="text-success">AgentHub 终端已连接</span>'),
    makeLine("<span class=\"text-success\">输入 'help' 查看可用命令。</span>"),
    makeLine('&nbsp;'),
  ])
  const [inputValue, setInputValue] = useState('')
  const outputRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const terminalPanelId = useId()

  // 自动滚动
  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight
    }
  }, [history])

  // ── 命令处理 ──
  const processCommand = useCallback(
    (cmd: string) => {
      const lines = [...history]
      lines.push(
        makeLine(
          `<span class="text-primary">$ </span><span class="text-text-primary">${escapeHtml(cmd)}</span>`,
        ),
      )

      const trimmed = cmd.trim()
      if (trimmed === '') {
        setHistory(lines)
        return
      }

      // git checkout / git switch 命令
      if (trimmed.startsWith('git checkout ') || trimmed.startsWith('git switch ')) {
        const target = trimmed.replace(/^git (checkout|switch) /, '').trim()
        if (availableBranches.includes(target)) {
          if (target === currentBranch) {
            lines.push(makeLine(`<span class="text-success">已经在 '${escapeHtml(target)}' 分支</span>`))
          } else {
            onBranchChange(target)
            lines.push(makeLine(`<span class="text-success">已切换到 '${escapeHtml(target)}' 分支</span>`))
          }
        } else {
          lines.push(
            makeLine(
              `<span class="text-error">错误：没有找到 '${escapeHtml(target)}' 对应的分支</span>`,
            ),
          )
        }
        setHistory(lines)
        return
      }

      // 命令映射表
      const result = getCommandOutput(trimmed, currentBranch, availableBranches, gitGraphData)
      if (result === '__CLEAR__') {
        setHistory([])
        return
      }
      lines.push(makeLine(result))
      setHistory(lines)
    },
    [history, currentBranch, availableBranches, gitGraphData, onBranchChange],
  )

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      processCommand(inputValue)
      setInputValue('')
    }
  }

  return (
    <div className="flex flex-1 flex-col border-b-0">
      {/* 头部 */}
      <button
        type="button"
        className="flex w-full shrink-0 items-center justify-between px-4 py-3 pb-2.5 text-left transition-colors hover:bg-bg-hover/40 focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-ring"
        onClick={toggle}
        aria-expanded={open}
        aria-controls={terminalPanelId}
        aria-label={`${open ? UI_ACTIONS.COLLAPSE : UI_ACTIONS.EXPAND}${UI_LABELS.TERMINAL}`}
      >
        <span className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-text-secondary">
          <TerminalIcon className="h-3.5 w-3.5" strokeWidth={1.25} aria-hidden="true" />
          {UI_LABELS.TERMINAL}
          <span className="ml-1 flex items-center gap-1 text-[10px] text-success">
            <span
              className="inline-block h-[5px] w-[5px] animate-pulse rounded-full bg-success"
              aria-hidden="true"
            />
            {UI_STATUS.CONNECTED}
          </span>
        </span>
        <ChevronRight
          className={`h-3.5 w-3.5 text-text-tertiary transition-transform ${open ? 'rotate-90' : ''}`}
          strokeWidth={1.25}
          aria-hidden="true"
        />
      </button>

      {/* 主体 */}
      <div
        id={terminalPanelId}
        className={`flex flex-col overflow-hidden transition-[max-height] duration-200 ease-out ${
          open ? 'max-h-[600px] flex-1' : 'max-h-0'
        }`}
      >
        <div className="flex flex-1 flex-col px-4 pb-3">
          <div
            className="flex flex-1 flex-col overflow-hidden rounded-md border border-border bg-[var(--code-bg,var(--bg-subtle))]"
            onClick={() => inputRef.current?.focus()}
          >
            {/* 标题栏 */}
            <div className="flex items-center gap-1.5 border-b border-border bg-white/[0.03] px-2.5 py-1.5">
              <span className="h-2 w-2 rounded-full bg-destructive" />
              <span className="h-2 w-2 rounded-full bg-[var(--color-warning)]" />
              <span className="h-2 w-2 rounded-full bg-[var(--color-success)]" />
              <span className="flex-1 text-center font-mono text-[11px] text-text-tertiary">
                {gitGraphData.repoPath
                  ? `${gitGraphData.repoPath.replace(/\/[^/]+$/, '')}/worktrees/...`
                  : '/workspace/project'}
              </span>
            </div>

            {/* 输出 */}
            <div
              ref={outputRef}
              className="terminal-output max-h-[200px] flex-1 overflow-y-auto px-3 py-2.5 font-mono text-xs leading-relaxed"
              role="log"
              aria-live="polite"
              aria-label="终端输出"
            >
              {history.map((line) => (
                <div
                  key={line.id}
                  className="whitespace-pre-wrap break-all"
                  dangerouslySetInnerHTML={{ __html: line.html }}
                />
              ))}
            </div>

            {/* 输入行 */}
            <div className="flex items-center border-t border-border bg-white/[0.02] px-3 py-1.5 font-mono text-xs">
              <span className="mr-1.5 shrink-0 whitespace-nowrap text-primary">
                (
                <span className="text-success">
                  {(branchLabels[currentBranch] ?? currentBranch) || '...'}
                </span>
                ) $
              </span>
              <input
                ref={inputRef}
                className="flex-1 border-none bg-transparent font-mono text-xs text-text-primary outline-none caret-primary"
                placeholder={UI_PLACEHOLDERS.TERMINAL_COMMAND}
                aria-label={UI_PLACEHOLDERS.TERMINAL_COMMAND}
                value={inputValue}
                onChange={(e) => setInputValue(e.target.value)}
                onKeyDown={handleKeyDown}
                autoFocus
              />
              {inputValue === '' && (
                <span className="ml-0.5 inline-block h-3.5 w-[7px] animate-pulse bg-primary align-text-bottom" />
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

// ─── 命令映射表 ────────────────────────────────────────────────

function getCommandOutput(
  cmd: string,
  currentBranch: string,
  availableBranches: string[],
  gitData: GitGraphData,
): CommandResult {
  const commands: Record<string, () => string> = {
    help: () =>
      '<span class="text-success">可用命令：clear, ls, pwd, git status, git log, git branch, git checkout &lt;branch&gt;, npm run build, npm test, whoami, cat, echo, help</span>',
    clear: () => '__CLEAR__',
    pwd: () =>
      `<span class="text-success">${escapeHtml(gitData.repoPath ?? '/home/user/workspace/project')}</span>`,
    ls: () =>
      '<span class="text-success">src/  package.json  tsconfig.json  README.md  node_modules/  .git/</span>',
    whoami: () => '<span class="text-success">agent-claude</span>',
    'git status': () => {
      if (currentBranch === 'main') {
        return '<span class="text-success">当前分支 </span><span class="text-text-secondary">main</span>\n<span class="text-success">没有需要提交的变更</span>'
      }
      return (
        '<span class="text-success">当前分支 </span><span class="text-text-secondary">' +
        escapeHtml(currentBranch) +
        '</span>\n<span class="text-success">尚未暂存的变更：</span>\n<span class="text-error">  modified:   src/components/chat/RightSidebar.tsx</span>\n<span class="text-error">  modified:   src/components/chat/MessageBubble.tsx</span>\n\n<span class="text-success">还没有加入提交的文件</span>'
      )
    },
    'git branch': () =>
      availableBranches
        .map(
          (b) =>
            (b === currentBranch
              ? '<span class="text-success">* </span>'
              : '<span class="text-success">  </span>') +
            '<span class="text-text-secondary">' +
            escapeHtml(b) +
            '</span>',
        )
        .join('\n'),
    'git log': () => {
      // 仅显示当前分支泳道上的提交
      const commits = gitData.commits.filter((c) => {
        // 在 main 分支上时包含所有提交（展示所有可达提交）
        if (currentBranch === 'main') return c.lane === 'main'
        return c.lane === currentBranch || c.lane === 'main'
      })
      if (commits.length === 0) return '<span class="text-text-tertiary">(no commits)</span>'
      return commits
        .map((c) => {
          return `<span class="text-text-secondary">${escapeHtml(c.hash)}</span> <span class="text-success">${escapeHtml(c.msg)}</span> <span class="text-text-tertiary">(${escapeHtml(c.time)})</span>`
        })
        .map((l) => `<div class="whitespace-pre-wrap break-all">${l}</div>`)
        .join('')
    },
    'npm run build': () =>
      '<span class="text-success">vite v8.0.0 building for production...</span>\n<span class="text-success">✓ </span><span class="text-text-secondary">42 modules transformed.</span>\n<span class="text-success">✓ built in 1.23s</span>',
    'npm test': () =>
      '<span class="text-success">Tests:       12 passed, 12 total</span>\n<span class="text-success">Time:        2.34s</span>\n<span class="text-success">Ran all test suites.</span>',
  }

  if (commands[cmd]) return commands[cmd]()
  if (cmd.startsWith('echo ')) {
    return `<span class="text-success">${escapeHtml(cmd.slice(5))}</span>`
  }
  if (cmd.startsWith('cat ')) {
    return `<span class="text-error">cat: ${escapeHtml(cmd.slice(4))}: 没有这个文件或目录</span>`
  }
  return `<span class="text-error">未找到命令: ${escapeHtml(cmd)}</span>`
}
