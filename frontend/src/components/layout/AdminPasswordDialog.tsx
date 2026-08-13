import { Lock } from 'lucide-react'
import { useId, useState } from 'react'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { adminAuth } from '@/lib/api'
import { UI_ACTIONS, UI_LABELS, UI_MESSAGES, UI_PLACEHOLDERS, UI_STATUS } from '@/lib/ui-text'
import { useAdminStore } from '@/stores/admin'

export function AdminPasswordDialog() {
  const {
    showPasswordDialog,
    passwordDialogPurpose,
    hidePasswordDialog,
    setAdminToken,
    setIsAuthenticated,
  } = useAdminStore()
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const passwordId = useId()
  const passwordErrorId = `${passwordId}-error`

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!password) return
    setLoading(true)
    setError('')
    try {
      const res = await adminAuth(password)
      setAdminToken(res.token, res.expires_in)
      setIsAuthenticated(true)
      hidePasswordDialog()
      setPassword('')
    } catch {
      setError(UI_MESSAGES.PASSWORD_ERROR)
    } finally {
      setLoading(false)
    }
  }

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      // login 用途的对话框是认证强制门：未认证时若允许 Esc/点遮罩关闭，
      // AdminContent 的 effect 依赖稳定的 showLoginDialog 函数引用，不会
      // 重新触发，用户会卡在"请认证"页且无法再次唤起登录框。
      // 因此 login 用途禁止关闭；reauth（敏感操作二次确认）允许取消。
      if (passwordDialogPurpose === 'login') return
      hidePasswordDialog()
      setPassword('')
      setError('')
    }
  }

  return (
    <Dialog open={showPasswordDialog} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-[360px]" showCloseButton={passwordDialogPurpose !== 'login'}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Lock className="h-4 w-4" strokeWidth={1.25} aria-hidden="true" />
            {passwordDialogPurpose === 'login'
              ? UI_LABELS.ADMIN_VERIFY
              : UI_LABELS.SENSITIVE_CONFIRM}
          </DialogTitle>
          <DialogDescription className="sr-only">
            {passwordDialogPurpose === 'login'
              ? UI_LABELS.ADMIN_VERIFY
              : UI_LABELS.SENSITIVE_CONFIRM}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col gap-3">
          <p className="text-sm text-text-secondary">
            {passwordDialogPurpose === 'login'
              ? UI_LABELS.ADMIN_VERIFY
              : UI_LABELS.SENSITIVE_CONFIRM}
          </p>
          <label htmlFor={passwordId} className="sr-only">
            {UI_LABELS.ENTER_PASSWORD}
          </label>
          <input
            id={passwordId}
            type="password"
            value={password}
            onChange={(e) => {
              setPassword(e.target.value)
              setError('')
            }}
            placeholder={UI_PLACEHOLDERS.PASSWORD}
            className={`h-9 rounded-md border bg-bg-canvas px-3 text-sm text-text-primary outline-none transition-[border-color,box-shadow] placeholder:text-tertiary focus:border-primary-border focus:ring-2 focus:ring-primary/15 ${
              error ? 'border-error' : 'border-border'
            }`}
            aria-invalid={Boolean(error) || undefined}
            aria-describedby={error ? passwordErrorId : undefined}
            autoComplete="current-password"
            autoFocus
          />
          {error && (
            <p id={passwordErrorId} className="text-xs text-error" role="alert">
              {error}
            </p>
          )}
          <button
            type="submit"
            disabled={loading || !password}
            className="h-9 rounded-md bg-brand text-sm font-medium text-primary-foreground transition-[transform,background,opacity] hover:bg-primary/90 active:scale-[0.98] disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          >
            {loading ? UI_STATUS.VERIFYING : UI_ACTIONS.CONFIRM}
          </button>
        </form>
      </DialogContent>
    </Dialog>
  )
}
