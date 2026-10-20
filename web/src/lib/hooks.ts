import { useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'

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
    let panel = ref.current
    let frame = 0
    const focusIn = (tries: number) => {
      // An item that took focus itself (autoFocus on the checked choice) keeps it.
      if (ref.current?.contains(document.activeElement)) {
        panel = ref.current
        return
      }
      const first = focusables()[0]
      if (first) {
        panel = ref.current
        first.focus()
      } else if (tries > 0) {
        // A portal menu measures its trigger first and renders a frame later.
        frame = requestAnimationFrame(() => focusIn(tries - 1))
      }
    }
    focusIn(5)

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
    return () => {
      cancelAnimationFrame(frame)
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
    // A menu in the picture-in-picture window hears that window's clicks and keys too.
    const docs = new Set([document, ref.current?.ownerDocument ?? document])
    for (const doc of docs) {
      doc.addEventListener('mousedown', onPointer)
      doc.addEventListener('touchstart', onPointer)
      doc.addEventListener('keydown', onKey)
    }
    return () => {
      for (const doc of docs) {
        doc.removeEventListener('mousedown', onPointer)
        doc.removeEventListener('touchstart', onPointer)
        doc.removeEventListener('keydown', onKey)
      }
    }
  }, [onClose, active])

  return ref
}

/** Where an open portal menu's trigger sits: measured on open, resize and scroll, never in render. */
export function useAnchorBox<T extends HTMLElement>(trigger: React.RefObject<T | null>, open: boolean) {
  const [box, setBox] = useState<DOMRect>()
  useLayoutEffect(() => {
    if (!open) return
    // Only a moved trigger is news: every scroll inside an open list re-rendered its whole owner.
    const measure = () => {
      const next = trigger.current?.getBoundingClientRect()
      setBox((prev) =>
        prev && next && prev.top === next.top && prev.left === next.left && prev.width === next.width && prev.height === next.height
          ? prev
          : next,
      )
    }
    let frame = 0
    const soon = () => {
      if (!frame) frame = requestAnimationFrame(() => {
        frame = 0
        measure()
      })
    }
    measure()
    window.addEventListener('resize', soon)
    window.addEventListener('scroll', soon, { capture: true, passive: true })
    return () => {
      cancelAnimationFrame(frame)
      window.removeEventListener('resize', soon)
      window.removeEventListener('scroll', soon, { capture: true })
    }
  }, [trigger, open])
  return open ? box : undefined
}

/** Room above an anchor, under a sticky header, for a menu opening upward: on a phone its top was cut off. */
export function useRoomAbove(anchor: React.RefObject<HTMLElement | null>, open: boolean) {
  const [room, setRoom] = useState<number>()
  // Measured after layout, as useAnchorBox does: the DOM is what this follows.
  useLayoutEffect(() => {
    const measure = () => {
      const el = anchor.current
      if (!el) return
      const doc = el.ownerDocument
      const header = doc.fullscreenElement ? 0 : (doc.querySelector('header')?.getBoundingClientRect().bottom ?? 0)
      setRoom(Math.max(120, el.getBoundingClientRect().top - Math.max(0, header) - 12))
    }
    if (open) measure()
  }, [anchor, open])
  return open ? room : undefined
}

/** A switch this browser remembers between visits; private mode keeps it for the page. */
export function useStoredFlag(key: string): [boolean, (on: boolean) => void] {
  const [on, setOn] = useState(() => {
    try {
      return localStorage.getItem(key) === '1'
    } catch {
      return false
    }
  })
  const set = (next: boolean) => {
    setOn(next)
    try {
      if (next) localStorage.setItem(key, '1')
      else localStorage.removeItem(key)
    } catch {
      // Unstorable: it lasts for the page.
    }
  }
  return [on, set]
}

/** Whether a media query matches now, following it as the window changes. */
export function useMediaQuery(query: string): boolean {
  // Kept per query: a new subscribe each render would drop and re-add the listener every time.
  const subscribe = useCallback(
    (changed: () => void) => {
      const list = window.matchMedia(query)
      list.addEventListener('change', changed)
      return () => list.removeEventListener('change', changed)
    },
    [query],
  )
  return useSyncExternalStore(subscribe, () => window.matchMedia(query).matches)
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
