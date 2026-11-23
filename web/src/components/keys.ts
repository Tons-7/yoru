import { useEffect, useState } from 'react'

export function plainKey(e: KeyboardEvent): string {
  if (e.ctrlKey || e.metaKey || e.altKey) return ''
  const target = e.target as HTMLElement | null
  if (target && (['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable)) return ''
  return e.key.toLowerCase()
}

export function usePortalHome(): HTMLElement {
  const current = () => (document.fullscreenElement as HTMLElement | null) ?? document.body
  const [home, setHome] = useState(current)
  useEffect(() => {
    const onChange = () => setHome(current())
    document.addEventListener('fullscreenchange', onChange)
    return () => document.removeEventListener('fullscreenchange', onChange)
  }, [])
  return home
}

export const SHOW_SHORTCUTS = 'yoru:shortcuts'

export interface Shortcut {
  keys: string[]
  does: string
}

export const PAGE_SHORTCUTS: Shortcut[] = [
  { keys: ['/'], does: 'Search' },
  { keys: ['D'], does: 'Show or hide downloads' },
  { keys: ['?'], does: 'Show or hide this list' },
  { keys: ['Esc'], does: 'Close a panel or dialog' },
]
