import { LayoutDashboard, MessageSquare, Settings, Sparkles, Users } from 'lucide-react'
import { useEffect, useState } from 'react'
import { NavLink } from 'react-router'

import { SettingsPanel } from '@/components/layout/SettingsPanel'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { getAdminAvatar } from '@/lib/api'
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
  'flex h-11 w-11 flex-col items-center justify-center gap-0.5 rounded-md py-1.5 text-tertiary transition-colors hover:bg-hover hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring active:bg-active'

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
  const adminAvatarUrl = useAdminStore((state) => state.adminAvatarUrl)
  const setAdminAvatarUrl = useAdminStore((state) => state.setAdminAvatarUrl)
  const isAuthenticated = useAdminStore((state) => state.isAuthenticated)
  const logout = useAdminStore((state) => state.logout)
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
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="relative mb-5 rounded-full transition-opacity hover:opacity-85 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          aria-label={CURRENT_USER_NAME}
        >
          <img
            src={displayUrl}
            alt=""
            className="h-9 w-9 rounded-full object-cover"
            draggable={false}
          />
          <span
            className="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full border border-sidebar bg-success"
            aria-label={UI_MISC.ONLINE}
          />
        </button>
      </PopoverTrigger>
      <PopoverContent side="right" align="start" className="w-[220px] p-4">
        <div className="flex items-center gap-2.5">
          <img src={displayUrl} alt="" className="h-10 w-10 rounded-lg object-cover" />
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
              className="w-full rounded-md bg-muted px-3 py-2 text-xs text-text-secondary transition-colors hover:bg-hover hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
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
    <aside className="flex w-14 shrink-0 flex-col items-center border-r border-border bg-sidebar py-3">
      <UserAvatarCard />

      <nav className="flex flex-col items-center gap-1" aria-label="主要导航">
        <NavItem
          to="/chat"
          label={UI_LABELS.CHAT}
          icon={<MessageSquare className="h-5 w-5" strokeWidth={1.25} />}
        />
        <NavItem
          to="/contacts"
          label={UI_LABELS.CONTACTS}
          icon={<Users className="h-5 w-5" strokeWidth={1.25} />}
        />
        <NavItem
          to="/skills"
          label={UI_LABELS.SKILLS_HUB}
          icon={<Sparkles className="h-5 w-5" strokeWidth={1.25} />}
        />
        <NavItem
          to="/admin"
          label={UI_LABELS.ADMIN}
          icon={<LayoutDashboard className="h-5 w-5" strokeWidth={1.25} />}
        />
      </nav>

      <div className="mt-auto flex flex-col items-center gap-1">
        <Popover>
          <PopoverTrigger asChild>
            <button type="button" className={navigationClass} aria-label={UI_LABELS.SETTINGS}>
              <Settings className="h-5 w-5" strokeWidth={1.25} />
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
          className="mt-1 flex h-9 w-9 items-center justify-center rounded-lg transition-opacity hover:opacity-80 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          aria-label="GitHub"
        >
          <img src="/favicon.svg" alt="" className="h-7 w-7" draggable={false} />
        </a>
      </div>
    </aside>
  )
}
