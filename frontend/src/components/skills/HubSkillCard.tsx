import { Package, Trash2, Wrench } from 'lucide-react'

import type { SkillHubItem } from '@/lib/api'
import { UI_ACTIONS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'

interface HubSkillCardProps {
  skill: SkillHubItem
  onDelete?: (trigger: HTMLButtonElement) => void
}

export function HubSkillCard({ skill, onDelete }: HubSkillCardProps) {
  return (
    <div className="min-h-[8rem] rounded-[14px] border border-border/70 bg-card/80 p-4 shadow-[0_12px_32px_rgba(0,0,0,0.08)] transition-[background,border-color,transform] hover:border-primary-border hover:bg-card active:scale-[0.995]">
      <div className="mb-2 flex min-w-0 flex-wrap items-center gap-2.5">
        <div
          className={cn(
            'flex h-9 w-9 items-center justify-center rounded-[10px] text-base',
            skill.builtin ? 'bg-success/10' : 'bg-primary/10',
          )}
        >
          {skill.builtin ? (
            <Wrench className="h-4 w-4" strokeWidth={1.25} aria-hidden="true" />
          ) : (
            <Package className="h-4 w-4" strokeWidth={1.25} aria-hidden="true" />
          )}
        </div>
        <span className="min-w-0 flex-[1_1_8rem] break-words text-[14px] font-semibold">
          {skill.name}
        </span>
        <span
          className={cn(
            'rounded-[6px] px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide',
            skill.builtin
              ? 'border border-success/15 bg-success/10 text-success'
              : 'border border-primary/15 bg-primary/10 text-primary',
          )}
        >
          {skill.builtin ? '内置' : '外部'}
        </span>
        {!skill.builtin && skill.status && skill.status !== 'ready' && (
          <span className="rounded-[6px] border border-warning/20 bg-warning/10 px-2 py-0.5 text-[10px] font-semibold text-warning">
            {skill.status === 'storage_error'
              ? '存储异常'
              : skill.status === 'deleting'
                ? '删除中'
                : skill.status === 'migrating'
                  ? '迁移中'
                  : skill.status}
          </span>
        )}
      </div>
      <p className="mb-3 break-words text-[12px] leading-relaxed text-text-secondary sm:pl-[46px]">
        {skill.description}
      </p>
      {!skill.builtin && (
        <div className="sm:pl-[46px]">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-[11px] text-tertiary">
              已被 {skill.import_count} 个 Agent 导入
            </span>
            {onDelete && (
              <button
                type="button"
                className="inline-flex items-center gap-1 rounded-[6px] border border-destructive/20 bg-destructive/10 px-2.5 py-1 text-[11px] text-destructive transition-[transform,background,opacity] hover:bg-destructive/20 active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
                onClick={(event) => {
                  event.stopPropagation()
                  onDelete(event.currentTarget)
                }}
                aria-label={`${UI_ACTIONS.DELETE} ${skill.name}`}
              >
                <Trash2 className="h-3 w-3" aria-hidden="true" />
                {UI_ACTIONS.DELETE}
              </button>
            )}
          </div>
          {(skill.uploaded_by || skill.sha256) && (
            <div className="mt-2 space-y-0.5 text-[10px] text-tertiary">
              {skill.uploaded_by && <p>来源：{skill.uploaded_by}</p>}
              {skill.sha256 && <p className="break-all font-mono">SHA-256：{skill.sha256}</p>}
              {skill.files && skill.files.length > 0 && <p>文件：{skill.files.join('、')}</p>}
              {(skill.contains_executable || skill.contains_binary) && (
                <p className="text-warning">
                  内容提示：
                  {[
                    skill.contains_executable && '可执行文件',
                    skill.contains_binary && '二进制文件',
                  ]
                    .filter(Boolean)
                    .join('、')}
                </p>
              )}
            </div>
          )}
          {!skill.uploaded_by &&
          !skill.sha256 &&
          (skill.files?.length || skill.contains_executable || skill.contains_binary) ? (
            <div className="mt-2 space-y-0.5 text-[10px] text-tertiary">
              {skill.files && skill.files.length > 0 && <p>文件：{skill.files.join('、')}</p>}
              {(skill.contains_executable || skill.contains_binary) && (
                <p className="text-warning">
                  内容提示：
                  {[
                    skill.contains_executable && '可执行文件',
                    skill.contains_binary && '二进制文件',
                  ]
                    .filter(Boolean)
                    .join('、')}
                </p>
              )}
            </div>
          ) : null}
        </div>
      )}
    </div>
  )
}
