import { BarChart3, Bot, FolderOpen, Heart, LayoutDashboard, Trash2 } from 'lucide-react'

import { type AdminMenuKey, useAdminMenu } from '@/stores/admin'

interface MenuItemProps {
  icon: React.ReactNode
  label: string
  menuKey: AdminMenuKey
}

const MENU_ITEMS: { icon: React.ReactNode; label: string; key: AdminMenuKey }[] = [
  {
    icon: <LayoutDashboard className="h-4 w-4" strokeWidth={1.25} />,
    label: '总览仪表盘',
    key: 'dashboard',
  },
  { icon: <Trash2 className="h-4 w-4" strokeWidth={1.25} />, label: '会话清理', key: 'sessions' },
  {
    icon: <FolderOpen className="h-4 w-4" strokeWidth={1.25} />,
    label: '工作区管理',
    key: 'workspaces',
  },
  { icon: <Bot className="h-4 w-4" strokeWidth={1.25} />, label: 'Agent 概览', key: 'agents' },
  { icon: <Heart className="h-4 w-4" strokeWidth={1.25} />, label: '服务健康', key: 'services' },
  {
    icon: <BarChart3 className="h-4 w-4" strokeWidth={1.25} />,
    label: '数据统计',
    key: 'statistics',
  },
]

function MenuItem({ icon, label, menuKey }: MenuItemProps) {
  const { activeMenuKey, setActiveMenuKey } = useAdminMenu()
  const isActive = activeMenuKey === menuKey

  return (
    <button
      className="flex w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-[13px] transition-colors"
      style={{
        background: isActive ? 'var(--primary-soft)' : 'transparent',
        color: isActive ? 'var(--color-brand)' : 'var(--text-secondary)',
      }}
      onClick={() => setActiveMenuKey(menuKey)}
      onMouseEnter={(e) => {
        if (!isActive) e.currentTarget.style.background = 'var(--bg-hover)'
      }}
      onMouseLeave={(e) => {
        if (!isActive) e.currentTarget.style.background = 'transparent'
      }}
    >
      {icon}
      <span>{label}</span>
    </button>
  )
}

export function AdminMenu() {
  return (
    <div
      className="flex h-full w-[180px] shrink-0 flex-col"
      style={{ borderRight: '1px solid var(--border)', background: 'var(--bg-card)' }}
    >
      <div className="flex flex-col items-center gap-2 px-4 py-4">
        <img
          src="https://api.dicebear.com/9.x/notionists/svg?seed=tln&backgroundColor=c0aede"
          alt="Admin"
          className="h-12 w-12 rounded-[10px] object-cover"
        />
        <span className="text-[12px] font-medium" style={{ color: 'var(--text-primary)' }}>
          田乐檬
        </span>
      </div>

      <div className="mx-3 h-px" style={{ background: 'var(--border)' }} />

      <nav className="flex flex-col gap-0.5 p-2">
        {MENU_ITEMS.map((item) => (
          <MenuItem key={item.key} icon={item.icon} label={item.label} menuKey={item.key} />
        ))}
      </nav>
    </div>
  )
}
