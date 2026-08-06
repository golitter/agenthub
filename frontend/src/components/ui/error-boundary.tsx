import { Component, type ErrorInfo, type ReactNode } from 'react'

import { UI_ACTIONS, UI_MESSAGES } from '@/lib/ui-text'

interface ErrorBoundaryProps {
  children: ReactNode
  fallback?: ReactNode
}

interface ErrorBoundaryState {
  hasError: boolean
  error: Error | null
  // 重试计数：每次 handleRetry 递增，用作 children 的 key 以强制重建子树，
  // 确保崩溃的子组件状态被彻底重置，而不是在同一实例上重新渲染再次抛错。
  retryKey: number
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false, error: null, retryKey: 0 }
  }

  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    // 仅标记错误态并保留 error；retryKey 不可在此重置，否则崩溃→重试→再崩溃
    // 的循环会让 key 退回到与首次挂载相同的值，产生歧义。
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  handleRetry = () => {
    this.setState((prev) => ({ hasError: false, error: null, retryKey: prev.retryKey + 1 }))
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) return this.props.fallback
      return (
        <div
          className="flex h-full min-h-[12rem] flex-col items-center justify-center gap-3 p-6 text-center"
          role="alert"
        >
          <div className="flex h-10 w-10 items-center justify-center rounded-[12px] bg-danger-bg text-destructive">
            <span className="font-mono text-sm font-semibold">!</span>
          </div>
          <p className="max-w-sm text-sm leading-6 text-destructive text-pretty">
            {this.state.error?.message || UI_MESSAGES.RENDER_ERROR}
          </p>
          <button
            type="button"
            className="rounded-[6px] bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground transition-[background,transform,opacity] hover:bg-primary/90 active:scale-[0.97] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
            onClick={this.handleRetry}
          >
            {UI_ACTIONS.RETRY}
          </button>
        </div>
      )
    }
    // 用 retryKey 作为 key 强制重建子树，保证重试时子组件内部状态被清空。
    return <div key={this.state.retryKey}>{this.props.children}</div>
  }
}
