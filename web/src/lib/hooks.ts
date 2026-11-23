import { useCallback, useEffect, useEffectEvent, useLayoutEffect, useRef, useState, useSyncExternalStore } from 'react'

export function useHeldData<T>(mutation: { data?: T; isPending: boolean }): T | undefined {
  const [last, setLast] = useState(mutation.data)
  if (mutation.data !== undefined && mutation.data !== last) setLast(mutation.data)
  return mutation.data ?? (mutation.isPending ? last : undefined)
}

export function useClampPage(empty: boolean, page: number, total: number | undefined, perPage: number, onPage: (page: number) => void) {
  const move = useEffectEvent(onPage)
  useEffect(() => {
    if (empty && page > 1 && total !== undefined) move(Math.max(1, Math.min(page - 1, Math.ceil(total / perPage))))
  }, [empty, page, total, perPage])
}

export function useModalFocus<T extends HTMLElement>(ref: React.RefObject<T | null>, open = true) {
  useEffect(() => {
    if (!open) return
    const opener = document.activeElement as HTMLElement | null
    const focusables = () =>
      Array.from(
        ref.current?.querySelectorAll<HTMLElement>(
          'button:not([disabled]), a[href], input:not([disabled]), select, textarea, iframe, [tabindex]:not([tabindex="-1"])',
        ) ?? [],
      )
    let panel = ref.current
    let frame = 0
    const focusIn = (tries: number) => {
      if (ref.current?.contains(document.activeElement)) {
        panel = ref.current
        return
      }
      const first = focusables()[0]
      if (first) {
        panel = ref.current
        first.focus()
      } else if (tries > 0) {
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
      const now = document.activeElement
      if (!now || now === document.body || panel?.contains(now)) opener?.focus?.()
    }
  }, [ref, open])
}

export function useDismiss<T extends HTMLElement>(onClose: () => void, active = true) {
  const ref = useRef<T>(null)
  const close = useEffectEvent(onClose)

  useEffect(() => {
    if (!active) return
    const layer = Symbol()
    dismissLayers.push(layer)
    function onPointer(e: MouseEvent | TouchEvent) {
      const target = e.target as Node | null
      if (!ref.current || ref.current.contains(target)) return
      const portal = target instanceof Element ? target.closest('[data-portal-menu]') : null
      if (portal && !portal.contains(ref.current)) return
      close()
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape' && dismissLayers.at(-1) === layer) close()
    }
    const docs = new Set([document, ref.current?.ownerDocument ?? document])
    for (const doc of docs) {
      doc.addEventListener('mousedown', onPointer)
      doc.addEventListener('touchstart', onPointer)
      doc.addEventListener('keydown', onKey)
    }
    const Watcher = (window as { CloseWatcher?: new () => { onclose: (() => void) | null; destroy: () => void } }).CloseWatcher
    const back = Watcher ? new Watcher() : null
    if (back) back.onclose = () => close()
    return () => {
      back?.destroy()
      dismissLayers.splice(dismissLayers.indexOf(layer), 1)
      for (const doc of docs) {
        doc.removeEventListener('mousedown', onPointer)
        doc.removeEventListener('touchstart', onPointer)
        doc.removeEventListener('keydown', onKey)
      }
    }
  }, [active])

  return ref
}

const dismissLayers: symbol[] = []

export function menuItems(menu: HTMLElement | null): HTMLElement[] {
  return [...(menu?.querySelectorAll<HTMLElement>('[role^="menuitem"]') ?? [])].filter((el) => el.offsetParent !== null)
}

export function walkMenu(e: React.KeyboardEvent<HTMLElement>) {
  const all = menuItems(e.currentTarget)
  const at = all.indexOf(e.currentTarget.ownerDocument.activeElement as HTMLElement)
  const moves: Record<string, number | undefined> = { ArrowDown: at + 1, ArrowUp: Math.max(at, 0) - 1, Home: 0, End: -1 }
  const to = moves[e.key]
  if (to === undefined || all.length === 0) return
  e.preventDefault()
  all[(to + all.length) % all.length].focus()
}

export function useMenuButton() {
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
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

export function useAnchorBox<T extends HTMLElement>(trigger: React.RefObject<T | null>, open: boolean) {
  const [box, setBox] = useState<DOMRect>()
  useLayoutEffect(() => {
    if (!open) return
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

export function useRoomAbove(anchor: React.RefObject<HTMLElement | null>, open: boolean, view: Window = window) {
  const [room, setRoom] = useState<number>()
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
    view.addEventListener('resize', measure)
    return () => view.removeEventListener('resize', measure)
  }, [anchor, open, view])
  return open ? room : undefined
}

export function useMediaQuery(query: string): boolean {
  const list = mediaList(query)
  const subscribe = useCallback(
    (changed: () => void) => {
      list.addEventListener('change', changed)
      return () => list.removeEventListener('change', changed)
    },
    [list],
  )
  return useSyncExternalStore(subscribe, () => list.matches)
}

const mediaLists = new Map<string, MediaQueryList>()

function mediaList(query: string): MediaQueryList {
  let list = mediaLists.get(query)
  if (!list) {
    list = window.matchMedia(query)
    mediaLists.set(query, list)
  }
  return list
}

export function useDocumentTitle(title?: string) {
  useEffect(() => {
    if (!title) return
    document.title = `${title} · Yoru`
    return () => {
      document.title = 'Yoru'
    }
  }, [title])
}

export function useDebounced<T>(value: T, delay = 300): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delay)
    return () => clearTimeout(id)
  }, [value, delay])
  return debounced
}
