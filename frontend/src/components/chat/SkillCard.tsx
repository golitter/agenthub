import type { AgentSkill } from '@/lib/api'

export function SkillCard({ skill }: { skill: AgentSkill }) {
  return (
    <div className="rounded-[10px] border border-border bg-card p-3.5">
      <div className="mb-1 flex items-center justify-between">
        <span className="text-sm font-semibold">{skill.name}</span>
        {skill.builtin && (
          <span className="inline-flex items-center gap-1 rounded bg-success/8 px-2 py-0.5 text-[11px] text-success">
            <span className="h-1 w-1 rounded-full bg-current" />
            builtin
          </span>
        )}
      </div>
      <p className="text-[13px] leading-relaxed text-foreground/75">{skill.description}</p>
      {skill.source && (
        <p className="mt-1.5 font-mono text-[11px] text-foreground/55">{skill.source}</p>
      )}
    </div>
  )
}
