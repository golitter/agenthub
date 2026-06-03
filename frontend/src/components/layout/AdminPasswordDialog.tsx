import { Lock } from 'lucide-react'
import { useState } from 'react'

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { adminAuth } from '@/lib/api'
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

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!password) return
    setLoading(true)
    setError('')
    try {
      const res = await adminAuth(password)
      setAdminToken(res.token)
      setIsAuthenticated(true)
      hidePasswordDialog()
      setPassword('')
    } catch {
      setError('密码错误')
    } finally {
      setLoading(false)
    }
  }

  const handleOpenChange = (open: boolean) => {
    if (!open) {
      hidePasswordDialog()
      setPassword('')
      setError('')
    }
  }

  return (
    <Dialog open={showPasswordDialog} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-[360px]">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Lock className="h-4 w-4" strokeWidth={1.25} />
            {passwordDialogPurpose === 'login' ? '管理员验证' : '敏感操作确认'}
          </DialogTitle>
          <DialogDescription className="sr-only">
            {passwordDialogPurpose === 'login'
              ? '请输入管理密码以进入管理面板'
              : '查看敏感信息需要再次验证密码'}
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="flex flex-col gap-3">
          <p className="text-sm text-text-secondary">
            {passwordDialogPurpose === 'login'
              ? '请输入管理密码以进入管理面板'
              : '查看敏感信息需要再次验证密码'}
          </p>
          <input
            type="password"
            value={password}
            onChange={(e) => {
              setPassword(e.target.value)
              setError('')
            }}
            placeholder="请输入密码"
            className="h-9 rounded-md px-3 text-sm outline-none"
            style={{
              border: '1px solid var(--border)',
              background: 'var(--bg-canvas)',
              color: 'var(--text-primary)',
            }}
            autoFocus
          />
          {error && (
            <p className="text-xs" style={{ color: 'var(--color-error)' }}>
              {error}
            </p>
          )}
          <button
            type="submit"
            disabled={loading || !password}
            className="h-9 rounded-md text-sm font-medium transition-[transform,opacity] disabled:opacity-50"
            style={{ background: 'var(--color-brand)', color: 'var(--primary-foreground)' }}
          >
            {loading ? '验证中...' : '确认'}
          </button>
        </form>
      </DialogContent>
    </Dialog>
  )
}
