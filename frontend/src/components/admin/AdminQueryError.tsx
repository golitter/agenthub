import { RefreshCw } from 'lucide-react'

import { UI_ACTIONS, UI_ERRORS } from '@/lib/ui-text'

interface AdminQueryErrorProps {
  onRetry: () => void
}

export function AdminQueryError({ onRetry }: AdminQueryErrorProps) {
  return (
    <div
      role="alert"
      className="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-destructive/20 bg-danger-bg px-3 py-2.5 text-sm text-destructive"
    >
      <span>{UI_ERRORS.ADMIN_LOAD_FAILED}</span>
      <button
        type="button"
        onClick={onRetry}
        className="inline-flex items-center gap-1.5 rounded-md border border-destructive/20 px-2.5 py-1 text-xs font-medium transition-[background,transform] hover:bg-destructive/10 active:scale-[0.98] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        <RefreshCw className="h-3.5 w-3.5" strokeWidth={1.25} />
        {UI_ACTIONS.RETRY}
      </button>
    </div>
  )
}
