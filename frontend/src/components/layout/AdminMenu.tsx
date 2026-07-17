import { BarChart3, Bot, FolderOpen, Heart, LayoutDashboard, Trash2, UserCog } from 'lucide-react'
import { useEffect, useState } from 'react'
import { NavLink } from 'react-router'

import { getAdminAvatar } from '@/lib/api'
import { CURRENT_USER_NAME } from '@/lib/constants'
import { UI_LABELS } from '@/lib/ui-text'
import { cn } from '@/lib/utils'
import { type AdminMenuKey, useAdminStore } from '@/stores/admin'

const MENU_ITEMS: { icon: React.ReactNode; label: string; key: AdminMenuKey }[] = [
  {
    icon: <LayoutDashboard className="h-4 w-4" strokeWidth={1.25} />,
    label: UI_LABELS.DASHBOARD,
    key: 'dashboard',
  },
  {
    icon: <Trash2 className="h-4 w-4" strokeWidth={1.25} />,
    label: UI_LABELS.SESSION_CLEANUP,
    key: 'sessions',
  },
  {
    icon: <FolderOpen className="h-4 w-4" strokeWidth={1.25} />,
    label: UI_LABELS.WORKSPACE_MANAGE,
    key: 'workspaces',
  },
  {
    icon: <Bot className="h-4 w-4" strokeWidth={1.25} />,
    label: UI_LABELS.AGENT_OVERVIEW,
    key: 'agents',
  },
  {
    icon: <Heart className="h-4 w-4" strokeWidth={1.25} />,
    label: UI_LABELS.SERVICE_HEALTH,
    key: 'services',
  },
  {
    icon: <BarChart3 className="h-4 w-4" strokeWidth={1.25} />,
    label: UI_LABELS.STATISTICS,
    key: 'statistics',
  },
  {
    icon: <UserCog className="h-4 w-4" strokeWidth={1.25} />,
    label: UI_LABELS.USER_MANAGEMENT,
    key: 'users',
  },
]

function MenuItem({
  icon,
  label,
  menuKey,
}: {
  icon: React.ReactNode
  label: string
  menuKey: AdminMenuKey
}) {
  return (
    <NavLink
      to={`/admin/${menuKey}`}
      className={({ isActive }) =>
        cn(
          'flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-[13px] transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
          isActive ? 'bg-primary-soft text-brand' : 'text-text-secondary hover:bg-hover',
        )
      }
    >
      {icon}
      <span>{label}</span>
    </NavLink>
  )
}

export function AdminMenu() {
  const adminAvatarUrl = useAdminStore((state) => state.adminAvatarUrl)
  const setAdminAvatarUrl = useAdminStore((state) => state.setAdminAvatarUrl)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    getAdminAvatar()
      .then((data) => {
        setAdminAvatarUrl(data.url)
        setLoaded(true)
      })
      .catch(() => setLoaded(true))
  }, [setAdminAvatarUrl])

  return (
    <aside className="hidden h-full w-[180px] shrink-0 flex-col border-r border-border bg-card sm:flex">
      <div className="flex flex-col items-center gap-2 px-4 py-4">
        <img
          src={
            loaded
              ? adminAvatarUrl
              : 'https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede'
          }
          alt={CURRENT_USER_NAME}
          className="h-12 w-12 rounded-[10px] object-cover"
        />
        <span className="text-xs font-medium text-foreground">{CURRENT_USER_NAME}</span>
      </div>

      <div className="mx-3 h-px bg-border" />

      <nav className="flex flex-col gap-0.5 p-2" aria-label="管理后台">
        {MENU_ITEMS.map((item) => (
          <MenuItem key={item.key} icon={item.icon} label={item.label} menuKey={item.key} />
        ))}
      </nav>
    </aside>
  )
}
