import { Moon, Sun } from 'lucide-react'

import { type Theme, useTheme } from '@/hooks/use-theme'
import { cn } from '@/lib/utils'

const OPTIONS: { value: Theme; label: string; icon: React.ReactNode }[] = [
  { value: 'dark', label: '深色', icon: <Moon className="h-3.5 w-3.5" strokeWidth={1.25} /> },
  { value: 'light', label: '浅色', icon: <Sun className="h-3.5 w-3.5" strokeWidth={1.25} /> },
]

export function SettingsPanel() {
  const { theme, setTheme } = useTheme()

  return (
    <div className="flex flex-col gap-2 p-1">
      <div className="px-2 text-[12px] font-medium text-text-secondary">外观</div>
      <div className="flex gap-1">
        {OPTIONS.map((opt) => (
          <button
            key={opt.value}
            onClick={() => setTheme(opt.value)}
            className={cn(
              'flex flex-1 items-center justify-center gap-1.5 rounded-[6px] px-2 py-1.5 text-[12px] font-medium transition-[background,color] duration-120',
              theme === opt.value
                ? 'bg-primary-soft text-primary'
                : 'bg-muted text-muted-foreground hover:bg-bg-hover',
            )}
          >
            {opt.icon}
            <span>{opt.label}</span>
          </button>
        ))}
      </div>
    </div>
  )
}
