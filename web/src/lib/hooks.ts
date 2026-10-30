import { useCallback, useEffect, useEffectEvent, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'

/** A button's last answer, kept while it's asked again: a new run starts empty, so its words would blink back. */
export function useHeldData<T>(mutation: { data?: T; isPending: boolean }): T | undefined {
  const [last, setLast] = useState(mutation.data)
  if (mutation.data !== undefined && mutation.data !== last) setLast(mutation.data)
  return mutation.data ?? (mutation.isPending ? last : undefined)
}

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
  // The latest close, so a caller's new function doesn't rebuild the Back watcher, which would lose its place.
  const close = useEffectEvent(onClose)

  useEffect(() => {
    if (!active) return
    function onPointer(e: MouseEvent | TouchEvent) {
      const target = e.target as Node | null
      if (!ref.current || ref.current.contains(target)) return
      // A portal menu isn't inside the trigger, so without this a click on it reads as outside and closes it.
      const portal = target instanceof Element ? target.closest('[data-portal-menu]') : null
      if (portal && !portal.contains(ref.current)) return
      close()
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') close()
    }
    // A menu in the picture-in-picture window hears that window's clicks and keys too.
    const docs = new Set([document, ref.current?.ownerDocument ?? document])
    for (const doc of docs) {
      doc.addEventListener('mousedown', onPointer)
      doc.addEventListener('touchstart', onPointer)
      doc.addEventListener('keydown', onKey)
    }
    // Android's Back closes what is open rather than leaving the page; Esc comes this way too, a second close.
    const Watcher = (window as { CloseWatcher?: new () => { onclose: (() => void) | null; destroy: () => void } }).CloseWatcher
    const back = Watcher ? new Watcher() : null
    if (back) back.onclose = () => close()
    return () => {
      back?.destroy()
      for (const doc of docs) {
        doc.removeEventListener('mousedown', onPointer)
        doc.removeEventListener('touchstart', onPointer)
        doc.removeEventListener('keydown', onKey)
      }
    }
  }, [active])

  return ref
}

/** A menu's items as the keyboard walks them: the visible ones (a phone hides some), radios included. */
export function menuItems(menu: HTMLElement | null): HTMLElement[] {
  return [...(menu?.querySelectorAll<HTMLElement>('[role^="menuitem"]') ?? [])].filter((el) => el.offsetParent !== null)
}

/** A menu's onKeyDown: arrows walk its items in reading order, Home and End jump to either end. */
export function walkMenu(e: React.KeyboardEvent<HTMLElement>) {
  const all = menuItems(e.currentTarget)
  const at = all.indexOf(e.currentTarget.ownerDocument.activeElement as HTMLElement)
  const moves: Record<string, number | undefined> = { ArrowDown: at + 1, ArrowUp: Math.max(at, 0) - 1, Home: 0, End: -1 }
  const to = moves[e.key]
  if (to === undefined || all.length === 0) return
  e.preventDefault()
  all[(to + all.length) % all.length].focus()
}

/** A menu button: opened from the keyboard its first item takes focus, and closing hands focus back. */
export function useMenuButton() {
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  // A click leaves focus on the button, as a mouse user expects.
  const byKey = useRef(false)
  const close = useCallback(() => {
    if (menu.current?.contains(menu.current.ownerDocument.activeElement)) trigger.current?.focus()
    setOpen(false)
  }, [])
  useEffect(() => {
    if (open && byKey.current) menuItems(menu.current)[0]?.focus()
  }, [open])
  const toggle = useCallback(
    (e: React.MouseEvent) => {
      byKey.current = e.detail === 0
      if (open) close()
      else setOpen(true)
    },
    [open, close],
  )
  return { open, close, toggle, trigger, menu }
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
export function useRoomAbove(anchor: React.RefObject<HTMLElement | null>, open: boolean, view: Window = window) {
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
    if (!open) return
    measure()
    // A turned phone or a resized window moves the anchor; view is the one it is in (picture in picture's).
    view.addEventListener('resize', measure)
    return () => view.removeEventListener('resize', measure)
  }, [anchor, open, view])
  return open ? room : undefined
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
