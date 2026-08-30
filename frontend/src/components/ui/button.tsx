import { forwardRef, type ReactNode } from 'react'

import { cn } from '@/lib/utils'

export type ButtonVariant = 'primary' | 'secondary' | 'destructive' | 'quiet'
export type ButtonSize = 'sm' | 'md' | 'icon'

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
  loading?: boolean
}

const variantClasses: Record<ButtonVariant, string> = {
  primary:
    'bg-primary text-primary-foreground shadow-[0_10px_24px_rgba(15,118,110,0.12)] hover:bg-primary/90',
  secondary:
    'border border-border bg-muted text-text-secondary hover:bg-bg-hover hover:text-foreground',
  destructive:
    'border border-destructive/20 bg-destructive/10 text-destructive hover:bg-destructive/20',
  quiet: 'text-muted-foreground hover:bg-bg-hover hover:text-foreground',
}

const sizeClasses: Record<ButtonSize, string> = {
  sm: 'min-h-8 rounded-md px-3 py-1.5 text-xs',
  md: 'min-h-9 rounded-md px-3.5 py-2 text-sm',
  icon: 'h-9 w-9 rounded-md p-0',
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant = 'primary', size = 'sm', loading = false, disabled, children, ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      className={cn(
        'inline-flex items-center justify-center gap-1.5 font-medium transition-[background,color,transform,opacity] active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
        variantClasses[variant],
        sizeClasses[size],
        className,
      )}
      {...props}
    >
      {loading && (
        <span
          className="h-3 w-3 animate-spin rounded-full border-2 border-current border-t-transparent"
          aria-hidden="true"
        />
      )}
      {children}
    </button>
  )
})

export interface IconButtonProps extends Omit<ButtonProps, 'children' | 'aria-label'> {
  label: string
  children: ReactNode
}

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { label, children, size = 'icon', variant = 'quiet', ...props },
  ref,
) {
  return (
    <Button ref={ref} {...props} size={size} variant={variant} aria-label={label}>
      {children}
    </Button>
  )
})
