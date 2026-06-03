import { LayoutDashboard, MessageSquare, Settings, Users } from 'lucide-react'
import { useEffect, useState } from 'react'

import { useHoverStyle } from '@/hooks/use-hover-style'
import { getAdminAvatar } from '@/lib/api'
import { CURRENT_USER_NAME } from '@/lib/constants'
import { cn } from '@/lib/utils'
import { useAdminStore } from '@/stores/admin'
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
      className={cn(
        'flex w-[44px] h-11 flex-col items-center justify-center gap-[2px] rounded-[6px] py-[6px] transition-[transform,opacity]',
        disabled ? 'cursor-not-allowed opacity-35' : 'cursor-pointer',
        isActive ? 'bg-primary-soft text-primary' : 'text-tertiary',
      )}
      onClick={() => !disabled && setActiveTab(tab)}
      {...(!isActive && !disabled ? hoverStyle : {})}
    >
      {icon}
      <span className="text-[11px] leading-none">{label}</span>
    </button>
  )
}

function UserAvatarCard() {
  const hoverStyle = useHoverStyle()
  const adminAvatarUrl = useAdminStore((s) => s.adminAvatarUrl)
  const setAdminAvatarUrl = useAdminStore((s) => s.setAdminAvatarUrl)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    getAdminAvatar()
      .then((data) => {
        setAdminAvatarUrl(data.url)
        setLoaded(true)
      })
      .catch(() => setLoaded(true))
  }, [setAdminAvatarUrl])

  const displayUrl = loaded
    ? adminAvatarUrl
    : 'https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede'

  return (
    <div className="group relative mb-5">
      <img
        src={displayUrl}
        alt={CURRENT_USER_NAME}
        className="h-9 w-9 cursor-pointer rounded-full object-cover transition-opacity duration-150 group-hover:opacity-85"
        {...hoverStyle}
      />
      <span className="absolute -right-0.5 -bottom-0.5 h-[10px] w-[10px] rounded-full border border-sidebar bg-success" />

      {/* hover 卡片 — popup shadow is allowed per VSG */}
      <div
        className="pointer-events-none absolute left-[52px] top-0 w-[220px] rounded-[12px] border border-border bg-card p-4 opacity-0 transition-[transform,opacity] duration-150 group-hover:pointer-events-auto group-hover:opacity-100"
        style={{ boxShadow: '0 4px 24px rgba(0,0,0,0.4)', transform: 'translateX(-4px)' }}
      >
        <div className="flex items-center gap-2.5">
          <img
            src={displayUrl}
            alt={CURRENT_USER_NAME}
            className="h-10 w-10 rounded-full object-cover"
          />
          <div>
            <div className="text-[13px] font-semibold text-foreground">{CURRENT_USER_NAME}</div>
            <div className="text-[11px] text-tertiary">tln · 在线</div>
          </div>
        </div>
        <div className="my-2 h-px bg-border" />
        <div className="flex gap-1.5">
          <button className="h-7 flex-1 rounded-[6px] border border-border bg-hover text-[11px] text-text-secondary transition-[transform,opacity] hover:bg-active">
            编辑资料
          </button>
          <button className="h-7 flex-1 rounded-[6px] border border-border bg-hover text-[11px] text-text-secondary transition-[transform,opacity] hover:bg-active">
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
        <a
          href="https://github.com/golitter/bytedanceai"
          target="_blank"
          rel="noopener noreferrer"
          className="mt-1 flex h-9 w-9 items-center justify-center rounded-[8px] transition-[transform,opacity] hover:opacity-80"
          title="GitHub"
        >
          <img src="/favicon.svg" alt="bytedanceai" className="h-7 w-7" draggable={false} />
        </a>
      </div>
    </div>
  )
}
