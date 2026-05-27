import { LayoutDashboard, MessageSquare, Settings, Users } from 'lucide-react'

import { useHoverStyle } from '@/hooks/use-hover-style'
import { type NavTab, useActiveTab } from '@/stores/chat'

interface NavItemProps {
  icon: React.ReactNode
  label: string
  tab: NavTab
  disabled?: boolean
}

function NavItem({ icon, label, tab, disabled }: NavItemProps) {
  const { activeTab, setActiveTab } = useActiveTab()
  const isActive = activeTab === tab
  const hoverStyle = useHoverStyle()

  return (
    <button
      className="flex w-[44px] flex-col items-center justify-center gap-[2px] rounded-[10px] py-[6px]"
      style={{
        height: 44,
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.35 : 1,
        background: isActive ? 'var(--primary-soft)' : 'transparent',
        color: isActive ? 'var(--primary)' : 'var(--text-tertiary)',
      }}
      onClick={() => !disabled && setActiveTab(tab)}
      {...(!isActive && !disabled ? hoverStyle : {})}
    >
      {icon}
      <span style={{ fontSize: 9, lineHeight: 1 }}>{label}</span>
    </button>
  )
}

function UserAvatarCard() {
  const hoverStyle = useHoverStyle()

  return (
    <div className="group relative mb-5">
      <img
        src="https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede"
        alt="田乐檬"
        className="h-9 w-9 cursor-pointer rounded-[10px] object-cover transition-opacity duration-150 group-hover:opacity-85"
        {...hoverStyle}
      />
      <span
        className="absolute -right-0.5 -bottom-0.5 h-[10px] w-[10px] rounded-full border-2 border-[var(--bg-sidebar)]"
        style={{ background: 'var(--color-success)' }}
      />

      {/* hover 卡片 */}
      <div
        className="pointer-events-none absolute left-[52px] top-0 w-[220px] rounded-xl border border-border bg-card p-4 opacity-0 shadow-lg transition-all duration-150 group-hover:pointer-events-auto group-hover:opacity-100"
        style={{ boxShadow: '0 8px 32px rgba(0,0,0,0.4)', transform: 'translateX(-4px)' }}
      >
        <div className="flex items-center gap-2.5">
          <img
            src="https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede"
            alt="田乐檬"
            className="h-10 w-10 rounded-[10px] object-cover"
          />
          <div>
            <div className="text-[13px] font-semibold text-foreground">田乐檬</div>
            <div className="text-[11px] text-tertiary">tln · 在线</div>
          </div>
        </div>
        <div className="my-2 h-px bg-border" />
        <div className="flex gap-1.5">
          <button className="h-7 flex-1 rounded-md border border-border bg-hover text-[11px] text-secondary transition-colors hover:bg-active">
            编辑资料
          </button>
          <button className="h-7 flex-1 rounded-md border border-border bg-hover text-[11px] text-secondary transition-colors hover:bg-active">
            退出登录
          </button>
        </div>
      </div>
    </div>
  )
}

export function IconSidebar() {
  return (
    <div className="flex w-[56px] shrink-0 flex-col items-center border-r border-border bg-sidebar py-3">
      <UserAvatarCard />

      <div className="flex flex-col items-center gap-1">
        <NavItem
          tab="chat"
          label="聊天"
          icon={<MessageSquare className="h-5 w-5" strokeWidth={1.25} />}
        />
        <NavItem
          tab="contacts"
          label="通讯录"
          disabled
          icon={<Users className="h-5 w-5" strokeWidth={1.25} />}
        />
        <NavItem
          tab="admin"
          label="管理"
          icon={<LayoutDashboard className="h-5 w-5" strokeWidth={1.25} />}
        />
      </div>

      <div className="mt-auto flex flex-col items-center gap-1">
        <NavItem
          tab="settings"
          label="设置"
          disabled
          icon={<Settings className="h-5 w-5" strokeWidth={1.25} />}
        />
      </div>
    </div>
  )
}
