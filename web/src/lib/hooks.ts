import { useEffect, useLayoutEffect, useRef, useState } from 'react'

/** A page whose last item went: back to the last page that still has some, not a false "nothing here". */
export function useClampPage(empty: boolean, page: number, total: number | undefined, perPage: number, onPage: (page: number) => void) {
  useEffect(() => {
    // Only ever down, so a stale total can't bounce it between pages.
    if (empty && page > 1 && total !== undefined) onPage(Math.max(1, Math.min(page - 1, Math.ceil(total / perPage))))
  }, [empty, page, total, perPage, onPage])
}

/** A modal's keyboard contract: focus moves in, Tab stays inside, and returns to the opener on close. */
export function useModalFocus<T extends HTMLElement>(ref: React.RefObject<T | null>, open = true) {
  useEffect(() => {
    if (!open) return
    const opener = document.activeElement as HTMLElement | null
    const focusables = () =>
      Array.from(
        ref.current?.querySelectorAll<HTMLElement>(
          // iframe: a trailer's player takes keys and Tab too.
          'button:not([disabled]), a[href], input:not([disabled]), select, textarea, iframe, [tabindex]:not([tabindex="-1"])',
        ) ?? [],
      )
    focusables()[0]?.focus()

    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Tab') return
      const items = focusables()
      if (items.length === 0) return
      const first = items[0]
      const last = items[items.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    const panel = ref.current
    return () => {
      document.removeEventListener('keydown', onKey)
      // Not when the click that closed it put focus somewhere else on purpose.
      const now = document.activeElement
      if (!now || now === document.body || panel?.contains(now)) opener?.focus?.()
    }
  }, [ref, open])
}

/** Closes on an outside click or Escape — both matter for a menu to feel right. */
// active: listening only while the menu is open, not for every closed one on the page.
export function useDismiss<T extends HTMLElement>(onClose: () => void, active = true) {
  const ref = useRef<T>(null)

  useEffect(() => {
    if (!active) return
    function onPointer(e: MouseEvent | TouchEvent) {
      const target = e.target as Node | null
      if (!ref.current || ref.current.contains(target)) return
      // A portal menu isn't inside the trigger, so without this a click on it reads as outside and closes it.
      if (target instanceof Element && target.closest('[data-portal-menu]')) return
      onClose()
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('touchstart', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('touchstart', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [onClose, active])

  return ref
}

/** Where a portal menu's trigger sits while it is open: measured on open, resize and scroll, never in render. */
export function useAnchorBox<T extends HTMLElement>(trigger: React.RefObject<T | null>, open: boolean) {
  const [box, setBox] = useState<DOMRect>()
  useLayoutEffect(() => {
    if (!open) return
    const measure = () => setBox(trigger.current?.getBoundingClientRect())
    measure()
    window.addEventListener('resize', measure)
    window.addEventListener('scroll', measure, { capture: true, passive: true })
    return () => {
      window.removeEventListener('resize', measure)
      window.removeEventListener('scroll', measure, { capture: true })
    }
  }, [trigger, open])
  return open ? box : undefined
}

/** The window's title, which the taskbar and the picture-in-picture window show. */
export function useDocumentTitle(title?: string) {
  useEffect(() => {
    if (!title) return
    document.title = `${title} · Yoru`
    return () => {
      document.title = 'Yoru'
    }
  }, [title])
}

/** Debounced value, for search-as-you-type against a rate-limited API. */
export function useDebounced<T>(value: T, delay = 300): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delay)
    return () => clearTimeout(id)
  }, [value, delay])
  return debounced
}
