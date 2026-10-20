import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Link,
  NavLink,
  Outlet,
  useLocation,
  useMatch,
  useNavigate,
  useNavigationType,
} from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, LIST_STATUSES, statusLabel, type LibraryItem } from '../lib/api'
import { cx } from '../lib/format'
import { useLibraryCounts, usePrefs, useSetPref, useSetup, useTrackers } from '../lib/queries'
import { ErrorBoundary } from './ErrorBoundary'
import { Unpaired } from './Unpaired'
import { AccessRequests } from './AccessRequests'
import { DownloadsPanel } from './DownloadsPanel'
import { SHOW_SHORTCUTS } from './keys'
import { NotificationPanel } from './NotificationPanel'
import { Shortcuts } from './Shortcuts'
import { SavedCopyNotice } from './SavedCopyNotice'
import { useDebounced, useDismiss, useMediaQuery } from '../lib/hooks'

const TABS = [
  { to: '/', label: 'Home', end: true },
  { to: '/browse', label: 'Browse' },
  { to: '/recent', label: 'Recent' },
  { to: '/schedule', label: 'Schedule' },
  { to: '/library', label: 'Library' },
]

export function Layout() {
  // The watch page sets its own width, so the player runs edge to edge on a phone.
  const fullBleed = useMatch('/watch/:animeId/:episode') !== null

  // Keyed on the path so each page animates in. Short enough that navigation
  // still feels immediate; the point is to show that something changed.
  const { pathname } = useLocation()

  // Every episode is the same page; re-keying would tear the player down.
  const pageKey = fullBleed ? 'watch' : pathname

  // Scroll to top on navigation, except back/forward (POP), where the old position is the point.
  const navigation = useNavigationType()
  useEffect(() => {
    if (navigation !== 'POP') window.scrollTo(0, 0)
  }, [pathname, navigation])

  // A fresh install goes to setup once, not on every route, or a local-only install reaches nothing.
  const setup = useSetup()
  const navigate = useNavigate()
  const nudged = useRef(false)
  useEffect(() => {
    if (nudged.current || !setup.data || pathname === '/setup') return
    if (setup.data.ready && setup.data.indexers > 0) return
    nudged.current = true
    navigate('/setup', { replace: true })
  }, [setup.data, pathname, navigate])

  return (
    <div className="min-h-dvh">
      <Header />
      {/* Outside the per-page main: Back remounted them, and the pairing overlay vanished with its state. */}
      <Unpaired />
      <AccessRequests />
      <main
        key={pageKey}
        className={cx(
          'mx-auto w-full animate-enter',
          fullBleed ? 'pb-8' : 'max-w-[1600px] px-4 pt-4 pb-16 sm:px-6',
        )}
      >
        {/* Keyed by page, so leaving a broken one clears it. */}
        <ErrorBoundary key={pageKey} resetKey={pathname}>
          <Outlet />
        </ErrorBoundary>
      </main>
    </div>
  )
}

function Header() {
  const [scrolled, setScrolled] = useState(false)
  // Tailwind's sm.
  const wide = useMediaQuery('(min-width: 40rem)')

  // The header sits over the hero art, so it only earns a background once the page has moved past it.
  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 8)
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [])

  return (
    <header
      className={cx(
        'sticky top-0 z-40 transition-colors duration-200',
        scrolled
          ? 'bg-base-950/75 shadow-[0_1px_0_rgb(255_255_255/0.05)] backdrop-blur-xl backdrop-saturate-150'
          : 'bg-transparent',
      )}
    >
      <div className="mx-auto flex h-16 w-full max-w-[1600px] items-center gap-2 px-4 sm:gap-4 sm:px-6">
        {/* Not on a phone: its browser has back, and search needs the room. */}
        <span className="contents max-sm:hidden">
          <BackButton />
        </span>
        <Link to="/" className="group flex shrink-0 items-baseline gap-1">
          <span className="font-display text-2xl font-bold tracking-tight text-white">Yoru</span>
          <span className="size-1.5 rounded-full bg-accent-500 transition-colors group-hover:bg-accent-400" />
        </Link>

        {/* A rule, or the name reads as the first of six links. */}
        <span className="hidden h-6 w-px shrink-0 bg-base-800 sm:block" />

        {/* A solid, lighter surface, so the group reads as a control, not loose text. */}
        <nav className="hidden items-center gap-0.5 rounded-full bg-base-800 p-1 ring-1 ring-white/5 sm:flex">
          <Tabs />
        </nav>

        <SearchBox />
        {/* In Settings too; on a phone it cost search its width. */}
        <span className="contents max-sm:hidden">
          <TitleLanguage />
        </span>
        {/* Not on a phone: no keyboard to glance with, and Downloads is in the profile menu. Unmounted
            there, not hidden, or it polls the downloads every few seconds for nothing. */}
        {wide && <DownloadsPanel />}
        <NotificationPanel />
        <ProfileMenu />
        <Shortcuts />
      </div>

      {/* Below sm the links get their own row; beside search they push the rest off-screen. */}
      <nav className="no-scrollbar mx-4 mb-2 flex items-center gap-0.5 overflow-x-auto rounded-full bg-base-800 p-1 ring-1 ring-white/5 sm:hidden">
        <Tabs />
      </nav>
      <SavedCopyNotice />
    </header>
  )
}

// The furthest entry Yoru's history reaches in this tab; history.length also counts pages from before Yoru.
const FURTHEST_KEY = 'yoru.history.furthest'
const readFurthest = () => {
  try {
    return Number(sessionStorage.getItem(FURTHEST_KEY)) || 0
  } catch {
    return 0
  }
}

// The app window has no browser chrome, so back and forward live here.
function BackButton() {
  const navigate = useNavigate()
  useLocation()
  const idx = (window.history.state as { idx?: number } | null)?.idx ?? 0
  // A new page cuts off what was ahead; going back keeps it.
  const furthest = useNavigationType() === 'PUSH' ? idx : Math.max(readFurthest(), idx)
  useEffect(() => {
    try {
      sessionStorage.setItem(FURTHEST_KEY, String(furthest))
    } catch {
      // Unstorable: Forward goes by this page alone.
    }
  }, [furthest])
  const arrow = (d: string) => (
    <svg viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round">
      <path d={d} />
    </svg>
  )
  const cls =
    'tap relative grid size-9 shrink-0 place-items-center rounded-full text-base-300 transition-colors hover:bg-base-800 hover:text-white disabled:opacity-25 disabled:hover:bg-transparent'
  return (
    <div className="flex shrink-0 items-center">
      <button type="button" onClick={() => navigate(-1)} disabled={idx === 0} aria-label="Back" title="Back" className={cls}>
        {arrow('M15 5l-7 7 7 7')}
      </button>
      <button
        type="button"
        onClick={() => navigate(1)}
        disabled={idx >= furthest}
        aria-label="Forward"
        title="Forward"
        className={cls}
      >
        {arrow('M9 5l7 7-7 7')}
      </button>
    </div>
  )
}

function Tabs() {
  return (
    <>
      {TABS.map((tab) => (
        <NavLink
          key={tab.to}
          to={tab.to}
          end={tab.end}
          className={({ isActive }) =>
            cx(
              // Phone: shared evenly so all five fit.
              'shrink-0 rounded-full px-3 py-1.5 text-sm transition-colors max-sm:flex-1 max-sm:px-2 max-sm:text-center',
              isActive
                ? 'bg-base-700 text-white shadow-card ring-1 ring-white/10'
                : 'text-base-300 hover:bg-base-850 hover:text-white',
            )
          }
        >
          {tab.label}
        </NavLink>
      ))}
    </>
  )
}

function SearchBox() {
  const navigate = useNavigate()
  const [value, setValue] = useState('')

  // Slash focuses search the way every site with a search box does.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null
      const typing = target?.tagName === 'INPUT' || target?.tagName === 'TEXTAREA'
      if (e.key === '/' && !typing) {
        e.preventDefault()
        document.getElementById('yoru-search')?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  // Instant results from what Yoru already holds; AniList is the slow path behind Enter or the last row.
  const q = useDebounced(value.trim(), 200)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const close = useCallback(() => setOpen(false), [])
  const box = useDismiss<HTMLFormElement>(close, open)
  const local = useQuery({
    enabled: q.length >= 2,
    queryKey: ['search', 'local', q],
    queryFn: () => api.get<{ results: LibraryItem[] }>(`/api/search/local?q=${encodeURIComponent(q)}&limit=8`),
    staleTime: 30_000,
  })
  const hits = q.length >= 2 ? (local.data?.results ?? []) : []
  const showMenu = open && q.length >= 2

  const browse = () => {
    if (!value.trim()) return
    setOpen(false)
    navigate(`/browse?q=${encodeURIComponent(value.trim())}`)
  }
  const pick = (id: number) => {
    setOpen(false)
    setValue('')
    navigate(`/anime/${id}`)
  }

  return (
    <form
      ref={box}
      className="relative ml-auto flex min-w-0 flex-1 justify-end sm:max-w-xs"
      onSubmit={(e) => {
        e.preventDefault()
        if (active >= 0 && hits[active]) pick(hits[active].id)
        else browse()
      }}
      // Tab out of the field closes the results, which otherwise sat over the page.
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false)
      }}
    >
      <div className="relative w-full max-w-56 sm:max-w-none">
        <SearchIcon />
        <input
          id="yoru-search"
          value={value}
          onChange={(e) => {
            setValue(e.target.value)
            setOpen(true)
            setActive(-1)
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setActive((a) => Math.min(hits.length, a + 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setActive((a) => Math.max(-1, a - 1))
            } else if (e.key === 'Escape') {
              setOpen(false)
            }
          }}
          role="combobox"
          aria-expanded={showMenu}
          aria-controls="yoru-search-results"
          aria-autocomplete="list"
          // The highlight is announced, as focus stays in the field.
          aria-activedescendant={showMenu && active >= 0 ? `yoru-search-opt-${active}` : undefined}
          autoComplete="off"
          placeholder="Search…"
          aria-label="Search anime"
          className={cx(
            'w-full rounded-md border border-base-800 bg-base-900/80 py-1.5 pl-8 text-sm text-base-100 placeholder:text-base-500 focus:border-accent-500 focus:outline-hidden',
            // Padding only while there's something to clear, so the button never sits over the text.
            value ? 'pr-8' : 'pr-3',
          )}
        />
        {!value && (
          <kbd className="pointer-events-none absolute top-1/2 right-2 hidden -translate-y-1/2 rounded border border-base-750 bg-base-850 px-1.5 font-mono text-[10px] leading-4 text-base-500 sm:block">
            /
          </kbd>
        )}
        {value && (
          <button
            type="button"
            aria-label="Clear search"
            onClick={() => {
              setValue('')
              document.getElementById('yoru-search')?.focus()
            }}
            className="tap absolute top-1/2 right-1 grid size-6 -translate-y-1/2 place-items-center rounded text-base-500 transition-colors hover:text-base-100"
          >
            <svg viewBox="0 0 20 20" aria-hidden className="size-3.5">
              <path
                d="M5 5l10 10M15 5L5 15"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.8"
                strokeLinecap="round"
              />
            </svg>
          </button>
        )}
      </div>

      {showMenu && (
        <ul
          id="yoru-search-results"
          role="listbox"
          // Anchored to the box it ran off the left edge of a phone; there it spans the screen instead.
          className="absolute top-full right-0 z-50 mt-1.5 w-80 max-w-[90vw] animate-rise overflow-hidden rounded-xl border border-base-750 bg-base-850 p-1 shadow-panel max-sm:fixed max-sm:inset-x-2 max-sm:top-14 max-sm:w-auto max-sm:max-w-none"
        >
          {hits.map((hit, i) => (
            <li key={hit.id} id={`yoru-search-opt-${i}`} role="option" aria-selected={i === active}>
              <button
                type="button"
                tabIndex={-1}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => pick(hit.id)}
                className={cx(
                  'flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left transition-colors',
                  i === active ? 'bg-base-800' : 'hover:bg-base-800',
                )}
              >
                {hit.cover ? (
                  <img src={hit.cover} alt="" className="h-11 w-8 shrink-0 rounded object-cover" />
                ) : (
                  <div className="h-11 w-8 shrink-0 rounded bg-base-800" />
                )}
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-sm text-base-100">{hit.title}</span>
                  <span className="block text-[11px] text-base-500">
                    {hit.status ? statusLabel(hit.status) : 'Not on your list'}
                    {hit.progress > 0 && ` · ${hit.progress}${hit.episodes ? `/${hit.episodes}` : ''} watched`}
                  </span>
                </span>
              </button>
            </li>
          ))}
          {hits.length === 0 && local.isSuccess && (
            // It searches every show Yoru holds, not only the library.
            <li className="px-2 py-1.5 text-xs text-base-500">Nothing saved matches. Press Enter to search AniList.</li>
          )}
          <li id={`yoru-search-opt-${hits.length}`} role="option" aria-selected={active === hits.length}>
            <button
              type="button"
              tabIndex={-1}
              onMouseDown={(e) => e.preventDefault()}
              onClick={browse}
              className={cx(
                'block w-full rounded-lg px-2 py-1.5 text-left text-xs text-accent-400 transition-colors',
                active === hits.length ? 'bg-base-800' : 'hover:bg-base-800',
              )}
            >
              Search AniList for “{value.trim()}”
            </button>
          </li>
        </ul>
      )}
    </form>
  )
}

/** English vs romaji titles; they resolve server-side, so every list is refetched. */
function TitleLanguage() {
  const prefs = usePrefs()
  const setPref = useSetPref()

  const mode = prefs.data?.effective['display.titles'] ?? 'english'

  const choose = (next: string) => {
    if (next === mode) return
    // useSetPref refreshes every query for this key, as titles are picked server-side.
    setPref.mutate({ key: 'display.titles', value: next })
  }

  return (
    <div className="hidden shrink-0 items-center rounded-full bg-base-850 p-0.5 sm:flex">
      {[
        { value: 'english', label: 'EN' },
        { value: 'romaji', label: 'JP' },
      ].map((option) => (
        <button
          key={option.value}
          onClick={() => choose(option.value)}
          aria-pressed={mode === option.value}
          title={option.value === 'english' ? 'English titles' : 'Romaji titles'}
          className={cx(
            'rounded-full px-2.5 py-1 text-xs font-semibold transition-colors',
            mode === option.value
              ? 'bg-accent-500 text-white'
              : 'text-base-400 hover:text-base-100',
          )}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

const MENU_ITEMS = [
  { to: '/history', label: 'Watch history', icon: <ClockIcon /> },
  { to: '/downloads', label: 'Downloads', icon: <DownloadIcon /> },
  { to: '/local', label: 'Local files', icon: <FolderIcon /> },
  { to: '/settings', label: 'Settings', icon: <GearIcon /> },
]

function ProfileMenu() {
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  // Opened from the keyboard, focus starts on the first item; a click leaves it on the button.
  const byKey = useRef(false)
  const close = useCallback(() => setOpen(false), [])
  const dismiss = useCallback(() => {
    if (menu.current?.contains(document.activeElement)) trigger.current?.focus()
    setOpen(false)
  }, [])
  const ref = useDismiss<HTMLDivElement>(dismiss, open)

  const trackers = useTrackers().data?.trackers ?? []
  const signedIn = trackers.filter((t) => t.connected)
  const user = signedIn.find((t) => t.user)?.user
  const attention = trackers.some((t) => t.reconnect)

  useEffect(() => {
    if (open && byKey.current) menuItems(menu.current)[0]?.focus()
  }, [open])

  // Arrow keys walk the items in reading order, Home and End jump to either end.
  const onMenuKey = (e: React.KeyboardEvent) => {
    const all = menuItems(menu.current)
    const at = all.indexOf(document.activeElement as HTMLElement)
    const moves: Record<string, number | undefined> = {
      ArrowDown: at + 1,
      ArrowUp: Math.max(at, 0) - 1,
      Home: 0,
      End: -1,
    }
    const to = moves[e.key]
    if (to === undefined || all.length === 0) return
    e.preventDefault()
    all[(to + all.length) % all.length].focus()
  }

  return (
    <div className="relative shrink-0" ref={ref}>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="Profile and settings"
        onClick={(e) => {
          byKey.current = e.detail === 0
          setOpen((v) => !v)
        }}
        onKeyDown={(e) => {
          if (open && e.key === 'ArrowDown') {
            e.preventDefault()
            menuItems(menu.current)[0]?.focus()
          }
        }}
        className={cx(
          'tap relative grid size-9 place-items-center rounded-full bg-base-800 text-base-200 transition-[background-color,box-shadow] duration-150 hover:bg-base-700',
          user && 'p-0.5',
          open && 'ring-2 ring-accent-500/70 ring-offset-2 ring-offset-base-950',
        )}
      >
        {user ? <Initial name={user} /> : <UserIcon />}
        {attention && (
          <span className="absolute -top-0.5 -right-0.5 size-2.5 rounded-full bg-amber-400 ring-2 ring-base-950" />
        )}
      </button>

      {open && (
        <div
          ref={menu}
          role="menu"
          aria-label="Profile"
          onKeyDown={onMenuKey}
          className="absolute right-0 z-50 mt-2.5 w-80 max-w-[calc(100vw-1rem)] origin-top-right animate-rise overflow-hidden rounded-2xl border border-base-750 bg-base-850/95 p-2 shadow-lift backdrop-blur-xl"
        >
          <Link
            role="menuitem"
            to="/settings?tab=Accounts"
            onClick={close}
            className="group flex items-center gap-3 rounded-xl p-2 transition-colors hover:bg-base-800 focus-visible:bg-base-800 focus-visible:outline-hidden"
          >
            <span className="grid size-10 shrink-0 place-items-center rounded-full bg-base-800 text-base-300 ring-1 ring-white/10">
              {user ? <Initial name={user} large /> : <UserIcon />}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-semibold text-white">
                {user ?? (signedIn.length > 0 ? 'Signed in' : 'Not signed in')}
              </span>
              {attention ? (
                <span className="block truncate text-xs text-amber-300">Reconnect to resume syncing</span>
              ) : (
                <span className="block truncate text-xs text-base-400">
                  {signedIn.length > 0 ? signedIn.map((t) => t.name).join(' · ') : 'Connect AniList to sync your list'}
                </span>
              )}
            </span>
            <ChevronIcon />
          </Link>

          <div className="my-2 h-px bg-base-750" />
          <ListShortcuts close={close} />
          <div className="my-2 h-px bg-base-750" />

          {MENU_ITEMS.map((item) => (
            <NavLink
              key={item.to}
              role="menuitem"
              to={item.to}
              onClick={close}
              className={({ isActive }) => cx(menuRow, isActive ? 'bg-base-800 text-white' : 'text-base-200')}
            >
              <span className={iconChip}>{item.icon}</span>
              {item.label}
            </NavLink>
          ))}
          {/* Not on a phone, which has no keys to press. */}
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              close()
              window.dispatchEvent(new Event(SHOW_SHORTCUTS))
            }}
            className={cx(menuRow, 'w-full text-left text-base-200 max-sm:hidden')}
          >
            <span className={iconChip}>
              <KeyboardIcon />
            </span>
            Keyboard shortcuts
            <kbd className="ml-auto rounded border border-base-700 bg-base-800 px-1.5 font-mono text-[11px] leading-5 text-base-400">
              ?
            </kbd>
          </button>
        </div>
      )}
    </div>
  )
}

// The visible items: Keyboard shortcuts is hidden on a phone.
const menuItems = (menu: HTMLElement | null) =>
  [...(menu?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? [])].filter((el) => el.offsetParent !== null)

const menuRow =
  'group flex items-center gap-3 rounded-lg px-2 py-1.5 text-sm transition-colors hover:bg-base-800 hover:text-white focus-visible:bg-base-800 focus-visible:text-white focus-visible:outline-hidden pointer-coarse:py-2'
const iconChip =
  'grid size-7 shrink-0 place-items-center rounded-md bg-base-800 text-base-400 ring-1 ring-white/5 transition-colors group-hover:bg-base-750 group-hover:text-base-100 group-focus-visible:text-base-100'

/** The list statuses with how many shows each holds, the reason this menu exists. */
function ListShortcuts({ close }: { close: () => void }) {
  const counts = useLibraryCounts().data
  return (
    <>
      <div className="flex items-center justify-between px-2 pb-1.5">
        <span className="text-xs font-medium text-base-400">My list</span>
        <Link
          role="menuitem"
          to="/library"
          onClick={close}
          className="tap relative rounded text-xs font-medium text-accent-400 transition-colors hover:text-accent-300 focus-visible:text-accent-300 focus-visible:outline-hidden"
        >
          {counts?.total ? `All ${counts.total}` : 'All'}
        </Link>
      </div>
      <div className="grid grid-cols-2 gap-1">
        {LIST_STATUSES.map((status) => {
          const n = counts?.statuses[status.value] ?? 0
          return (
            <Link
              key={status.value}
              role="menuitem"
              to={`/library?status=${status.value}`}
              onClick={close}
              className="flex min-h-10 items-center justify-between gap-2 rounded-lg bg-base-800/50 px-3 text-sm text-base-200 ring-1 ring-white/[0.04] transition-colors hover:bg-base-750 hover:text-white focus-visible:bg-base-750 focus-visible:text-white focus-visible:outline-hidden pointer-coarse:min-h-11"
            >
              <span className="truncate">{status.label}</span>
              {counts && (
                <span className={cx('text-xs tabular-nums', n > 0 ? 'text-base-300' : 'text-base-600')}>{n}</span>
              )}
            </Link>
          )
        })}
      </div>
    </>
  )
}

function Initial({ name, large }: { name: string; large?: boolean }) {
  return (
    <span
      className={cx(
        'grid size-full place-items-center rounded-full bg-gradient-to-br from-accent-400 to-accent-600 font-display font-semibold text-white',
        large ? 'text-lg' : 'text-sm',
      )}
    >
      {Array.from(name.trim())[0]?.toUpperCase()}
    </span>
  )
}

function ChevronIcon() {
  return (
    <svg viewBox="0 0 20 20" className="size-4 shrink-0 text-base-500 transition-colors group-hover:text-base-300" aria-hidden>
      <path d="m8 5 5 5-5 5" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function KeyboardIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <rect x="3" y="6" width="18" height="12" rx="2" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path d="M7 10h.01M10.5 10h.01M14 10h.01M17 10h.01M8 14h8" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  )
}

function SearchIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-base-500"
      aria-hidden
    >
      <circle cx="11" cy="11" r="6.5" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <path d="m16 16 4.5 4.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  )
}

function UserIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <circle cx="12" cy="8.5" r="3.5" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path
        d="M5 20c1-3.5 3.8-5 7-5s6 1.5 7 5"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
      />
    </svg>
  )
}

function ClockIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <circle cx="12" cy="12" r="8.5" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path d="M12 7.5V12l3 2" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  )
}

function DownloadIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M12 4v10m0 0 4-4m-4 4-4-4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M5 18h14" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  )
}

function FolderIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M4 7.5A1.5 1.5 0 0 1 5.5 6h3.4a1.5 1.5 0 0 1 1.2.6l.9 1.2h7.5A1.5 1.5 0 0 1 20 9.3v8.2a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 17.5v-10Z" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
    </svg>
  )
}

function GearIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path d="M12 3.5v2m0 13v2M20.5 12h-2m-13 0h-2m14.1-6.1-1.4 1.4M7.8 16.2l-1.4 1.4m12.3 0-1.4-1.4M7.8 7.8 6.4 6.4" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  )
}
