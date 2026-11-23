import { Component, type ReactNode } from 'react'
import { buttonClass } from '../lib/ui'

const STALE_CHUNK = /dynamically imported module|Importing a module script failed|error loading dynamically/i

export class ErrorBoundary extends Component<
  { children: ReactNode; resetKey?: string; back?: { href: string; label: string } },
  { error: Error | null; key?: string }
> {
  state: { error: Error | null; key?: string } = { error: null, key: this.props.resetKey }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  static getDerivedStateFromProps(props: { resetKey?: string }, state: { key?: string }) {
    return props.resetKey !== state.key ? { error: null, key: props.resetKey } : null
  }

  render() {
    const { error } = this.state
    if (!error) return this.props.children
    const updated = STALE_CHUNK.test(error.message)
    return (
      <div className="grid min-h-[50vh] place-items-center p-6 text-center">
        <div className="max-w-md">
          <p className="text-base font-medium text-white">
            {updated ? 'Yoru was updated' : 'This page hit a problem'}
          </p>
          <p className="mt-1.5 text-sm text-base-400">
            {updated ? 'Reload to pick up the new version.' : 'Reloading usually fixes it.'}
          </p>
          {!updated && (
            <details className="mt-2 text-xs text-base-500">
              <summary className="cursor-pointer">Details</summary>
              <p className="mt-1 break-words">{error.message.slice(0, 300)}</p>
            </details>
          )}
          <div className="mt-4 flex flex-wrap justify-center gap-2">
            <button onClick={() => window.location.reload()} className={buttonClass('primary')}>
              Reload
            </button>
            <a href={this.props.back?.href ?? '/'} className={buttonClass('secondary')}>
              {this.props.back?.label ?? 'Go home'}
            </a>
          </div>
        </div>
      </div>
    )
  }
}
