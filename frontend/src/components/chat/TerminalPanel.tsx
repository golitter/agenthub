import { Terminal as TerminalIcon } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'

import type { CommandResult, GitGraphData, TerminalPanelProps } from './git-graph-types'
import { useCollapsible } from './RightSidebar'

// ─── Helpers ────────────────────────────────────────────────────

function escapeHtml(str: string): string {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

// ─── Component ──────────────────────────────────────────────────

export function TerminalPanel({
  currentBranch,
  availableBranches,
  gitGraphData,
  onBranchChange,
  branchLabels,
}: TerminalPanelProps) {
  const [open, toggle] = useCollapsible('terminal', true)
  const [history, setHistory] = useState<string[]>([
    '<span class="text-success">Welcome to AgentHub Terminal</span>',
    '<span class="text-success">Type \'help\' for available commands.</span>',
    '&nbsp;',
  ])
  const [inputValue, setInputValue] = useState('')
  const outputRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  // Auto-scroll
  useEffect(() => {
    if (outputRef.current) {
      outputRef.current.scrollTop = outputRef.current.scrollHeight
    }
  }, [history])

  // ── Command handler ──
  const processCommand = useCallback(
    (cmd: string) => {
      const lines = [...history]
      lines.push(
        `<span class="text-primary">$ </span><span class="text-text-primary">${escapeHtml(cmd)}</span>`,
      )

      const trimmed = cmd.trim()
      if (trimmed === '') {
        setHistory(lines)
        return
      }

      // git checkout / git switch
      if (trimmed.startsWith('git checkout ') || trimmed.startsWith('git switch ')) {
        const target = trimmed.replace(/^git (checkout|switch) /, '').trim()
        if (availableBranches.includes(target)) {
          if (target === currentBranch) {
            lines.push(`<span class="text-success">Already on '${target}'</span>`)
          } else {
            onBranchChange(target)
            lines.push(`<span class="text-success">Switched to branch '${target}'</span>`)
          }
        } else {
          lines.push(
            `<span class="text-error">error: pathspec '${escapeHtml(target)}' did not match any branch known to git</span>`,
          )
        }
        setHistory(lines)
        return
      }

      // Command map
      const result = getCommandOutput(trimmed, currentBranch, availableBranches, gitGraphData)
      if (result === '__CLEAR__') {
        setHistory([])
        return
      }
      lines.push(result)
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
      {/* Header */}
      <button
        type="button"
        className="flex shrink-0 w-full items-center justify-between px-4 py-3 pb-2.5 text-left"
        onClick={toggle}
      >
        <span className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wider text-text-secondary">
          <TerminalIcon className="h-3.5 w-3.5" strokeWidth={1.25} />
          Terminal
          <span className="ml-1 flex items-center gap-1 text-[10px] text-success">
            <span className="inline-block h-[5px] w-[5px] animate-pulse rounded-full bg-success" />
            Connected
          </span>
        </span>
        <svg
          className={`h-3.5 w-3.5 text-text-tertiary transition-transform ${open ? 'rotate-90' : ''}`}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={1.25}
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>

      {/* Body */}
      <div
        className={`flex flex-col overflow-hidden transition-[max-height] duration-200 ease-out ${
          open ? 'max-h-[600px] flex-1' : 'max-h-0'
        }`}
        style={open ? { paddingBottom: 0 } : undefined}
      >
        <div className="flex flex-1 flex-col px-4 pb-3">
          <div
            className="flex flex-1 flex-col overflow-hidden rounded-md border border-border bg-[var(--code-bg,var(--bg-subtle))]"
            onClick={() => inputRef.current?.focus()}
          >
            {/* Title bar */}
            <div className="flex items-center gap-1.5 border-b border-border bg-white/[0.03] px-2.5 py-1.5">
              <span className="h-2 w-2 rounded-full bg-destructive" />
              <span className="h-2 w-2 rounded-full bg-[var(--color-warning)]" />
              <span className="h-2 w-2 rounded-full bg-[var(--color-success)]" />
              <span className="flex-1 text-center font-mono text-[11px] text-text-tertiary">
                {gitGraphData.repoPath
                  ? `${gitGraphData.repoPath}/worktrees/...`
                  : '/workspace/project'}
              </span>
            </div>

            {/* Output */}
            <div
              ref={outputRef}
              className="terminal-output max-h-[200px] flex-1 overflow-y-auto px-3 py-2.5 font-mono text-xs leading-relaxed"
            >
              {history.map((line, i) => (
                <div
                  key={i}
                  className="whitespace-pre-wrap break-all"
                  dangerouslySetInnerHTML={{ __html: line }}
                />
              ))}
            </div>

            {/* Input row */}
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
                placeholder="Type a command..."
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

// ─── Command Map ────────────────────────────────────────────────

function getCommandOutput(
  cmd: string,
  currentBranch: string,
  availableBranches: string[],
  gitData: GitGraphData,
): CommandResult {
  const commands: Record<string, () => string> = {
    help: () =>
      '<span class="text-success">Available commands: clear, ls, pwd, git status, git log, git branch, git checkout &lt;branch&gt;, npm run build, npm test, whoami, cat, echo, help</span>',
    clear: () => '__CLEAR__',
    pwd: () =>
      `<span class="text-success">${gitData.repoPath ?? '/home/user/workspace/project'}</span>`,
    ls: () =>
      '<span class="text-success">src/  package.json  tsconfig.json  README.md  node_modules/  .git/</span>',
    whoami: () => '<span class="text-success">agent-claude</span>',
    'git status': () => {
      if (currentBranch === 'main') {
        return '<span class="text-success">On branch </span><span class="text-text-secondary">main</span>\n<span class="text-success">nothing to commit, working tree clean</span>'
      }
      return (
        '<span class="text-success">On branch </span><span class="text-text-secondary">' +
        escapeHtml(currentBranch) +
        '</span>\n<span class="text-success">Changes not staged for commit:</span>\n<span class="text-error">  modified:   src/components/chat/RightSidebar.tsx</span>\n<span class="text-error">  modified:   src/components/chat/MessageBubble.tsx</span>\n\n<span class="text-success">no changes added to commit</span>'
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
      // Show only commits on the current branch's lane
      const commits = gitData.commits.filter((c) => {
        // Include all commits if on main (shows everything reachable)
        if (currentBranch === 'main') return c.lane === 'main'
        return c.lane === currentBranch || c.lane === 'main'
      })
      if (commits.length === 0) return '<span class="text-text-tertiary">(no commits)</span>'
      return commits
        .map((c) => {
          return `<span class="text-text-secondary">${c.hash}</span> <span class="text-success">${escapeHtml(c.msg)}</span> <span class="text-text-tertiary">(${c.time})</span>`
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
    return `<span class="text-error">cat: ${escapeHtml(cmd.slice(4))}: No such file or directory</span>`
  }
  return `<span class="text-error">command not found: ${escapeHtml(cmd)}</span>`
}
