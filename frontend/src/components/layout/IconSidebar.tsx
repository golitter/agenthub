import { LayoutDashboard, MessageSquare, Settings, Sparkles, Users } from 'lucide-react'
import { NavLink } from 'react-router'

import { SettingsPanel } from '@/components/layout/SettingsPanel'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { FALLBACK_ADMIN_AVATAR_URL, useAdminAvatar } from '@/hooks/use-admin'
import { CURRENT_USER_NAME, PROJECT_META } from '@/lib/constants'
import { UI_ACTIONS, UI_LABELS, UI_MISC } from '@/lib/ui-text'
import { cn } from '@/lib/utils'
import { useAdminStore } from '@/stores/admin'

interface NavItemProps {
  icon: React.ReactNode
  label: string
  to: string
}

const navigationClass =
  'flex h-11 min-w-11 flex-1 flex-col items-center justify-center gap-0.5 rounded-md py-1.5 text-tertiary transition-colors hover:bg-bg-hover hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring active:bg-active md:w-11 md:flex-none'

function NavItem({ icon, label, to }: NavItemProps) {
  return (
    <NavLink
      to={to}
      aria-label={label}
      className={({ isActive }) =>
        cn(navigationClass, isActive && 'bg-primary-soft text-primary hover:bg-primary-soft')
      }
    >
      {icon}
      <span className="text-[11px] leading-none">{label}</span>
    </NavLink>
  )
}

function UserAvatarCard() {
  const isAuthenticated = useAdminStore((state) => state.isAuthenticated)
  const logout = useAdminStore((state) => state.logout)
  // 通过共享 react-query 订阅 admin 头像，避免与 AdminMenu 各自 fetch 重复请求。
  const { url: adminAvatarUrl } = useAdminAvatar()

  const displayUrl = adminAvatarUrl ?? FALLBACK_ADMIN_AVATAR_URL

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="relative mb-5 hidden rounded-[11px] transition-[opacity,transform] hover:opacity-85 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring md:block"
          aria-label={`当前用户：${CURRENT_USER_NAME}`}
        >
          <img
            src={displayUrl}
            alt=""
            className="h-9 w-9 rounded-[10px] object-cover"
            draggable={false}
            onError={(event) => {
              event.currentTarget.src = '/favicon.svg'
            }}
          />
          <span
            className="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full border border-sidebar bg-success"
            aria-hidden="true"
          />
        </button>
      </PopoverTrigger>
      <PopoverContent side="right" align="start" className="w-[220px] p-4">
        <div className="flex items-center gap-2.5">
          <img
            src={displayUrl}
            alt={CURRENT_USER_NAME}
            className="h-10 w-10 rounded-lg object-cover"
            onError={(event) => {
              event.currentTarget.src = '/favicon.svg'
            }}
          />
          <div className="min-w-0">
            <div className="truncate text-[13px] font-semibold text-foreground">
              {CURRENT_USER_NAME}
            </div>
            <div className="text-[11px] text-tertiary">{`${UI_MISC.ME} · ${UI_MISC.ONLINE}`}</div>
          </div>
        </div>
        {isAuthenticated && (
          <>
            <div className="my-3 h-px bg-border" />
            <button
              type="button"
              className="w-full rounded-md bg-muted px-3 py-2 text-xs text-text-secondary transition-colors hover:bg-bg-hover hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
              onClick={logout}
            >
              {UI_ACTIONS.LOGOUT}
            </button>
          </>
        )}
      </PopoverContent>
    </Popover>
  )
}

export function IconSidebar() {
  return (
    <aside className="fixed inset-x-0 bottom-0 z-40 flex h-14 shrink-0 items-center border-t border-border bg-sidebar/95 px-2 backdrop-blur md:static md:h-full md:w-14 md:flex-col md:border-r md:border-t-0 md:bg-sidebar md:px-0 md:py-3">
      <UserAvatarCard />

      <nav
        className="flex min-w-0 flex-1 items-center justify-around gap-1 md:flex-none md:flex-col md:justify-start"
        aria-label="主要导航"
      >
        <NavItem
          to="/chat"
          label={UI_LABELS.CHAT}
          icon={<MessageSquare className="h-5 w-5" strokeWidth={1.25} aria-hidden="true" />}
        />
        <NavItem
          to="/contacts"
          label={UI_LABELS.CONTACTS}
          icon={<Users className="h-5 w-5" strokeWidth={1.25} aria-hidden="true" />}
        />
        <NavItem
          to="/skills"
          label={UI_LABELS.SKILLS_HUB}
          icon={<Sparkles className="h-5 w-5" strokeWidth={1.25} aria-hidden="true" />}
        />
        <NavItem
          to="/admin"
          label={UI_LABELS.ADMIN}
          icon={<LayoutDashboard className="h-5 w-5" strokeWidth={1.25} aria-hidden="true" />}
        />
      </nav>

      <div className="flex shrink-0 items-center gap-1 md:mt-auto md:flex-col">
        <Popover>
          <PopoverTrigger asChild>
            <button type="button" className={navigationClass} aria-label={UI_LABELS.SETTINGS}>
              <Settings className="h-5 w-5" strokeWidth={1.25} aria-hidden="true" />
              <span className="text-[11px] leading-none">{UI_LABELS.SETTINGS}</span>
            </button>
          </PopoverTrigger>
          <PopoverContent side="right" align="end" className="w-[200px]">
            <SettingsPanel />
          </PopoverContent>
        </Popover>
        <a
          href={PROJECT_META.GITHUB_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="mt-1 hidden h-9 w-9 items-center justify-center rounded-lg transition-opacity hover:opacity-80 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring md:flex"
          aria-label="GitHub"
        >
          <img src="/favicon.svg" alt="" className="h-7 w-7" draggable={false} />
        </a>
      </div>
    </aside>
  )
}
