import type { AgentDetail } from '@/lib/api'

export function AgentMeta({ detail }: { detail: AgentDetail }) {
  return (
    <div className="grid grid-cols-2 gap-4 rounded-[10px] border border-border bg-card p-4">
      <MetaItem label="Session ID" value={detail.session_id} mono />
      <MetaItem label="Task ID" value={detail.task_id} mono />
      {detail.repo_path && <MetaItem label="Repo Path" value={detail.repo_path} />}
      {detail.workspace_path && <MetaItem label="Workspace" value={detail.workspace_path} />}
      <MetaItem label="创建时间" value={new Date(detail.created_at).toLocaleString('zh-CN')} />
      <MetaItem label="消息数" value={String(detail.message_count)} />
    </div>
  )
}

function MetaItem({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[11px] uppercase tracking-wider text-foreground/50">{label}</span>
      <span className={`text-[13px] break-all ${mono ? 'font-mono text-xs' : ''}`}>{value}</span>
    </div>
  )
}
