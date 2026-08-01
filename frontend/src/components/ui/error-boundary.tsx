import { Component, type ErrorInfo, type ReactNode } from 'react'

import { UI_ACTIONS, UI_MESSAGES } from '@/lib/ui-text'

interface ErrorBoundaryProps {
  children: ReactNode
  fallback?: ReactNode
}

interface ErrorBoundaryState {
  hasError: boolean
  error: Error | null
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  handleRetry = () => {
    this.setState({ hasError: false, error: null })
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
    return this.props.children
  }
}
