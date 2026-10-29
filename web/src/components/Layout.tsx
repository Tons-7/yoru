import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
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
import { bytes, cx } from '../lib/format'
import { buttonClass } from '../lib/ui'
import {
  useAccess,
  useDownloads,
  useLibraryCounts,
  usePrefs,
  useSetPref,
  useSetup,
  useTrackers,
  type Download,
} from '../lib/queries'
import { BottomTabs } from './BottomTabs'
import { install, useInstall } from '../lib/install'
import { ErrorBoundary } from './ErrorBoundary'
import { Unpaired } from './Unpaired'
import { AccessRequests } from './AccessRequests'
import { DownloadsPanel } from './DownloadsPanel'
import { SHOW_SHORTCUTS } from './keys'
import { NotificationPanel } from './NotificationPanel'
import { Shortcuts } from './Shortcuts'
import { SavedCopyNotice } from './SavedCopyNotice'
import { menuItems, useDebounced, useDismiss, useMediaQuery, useModalFocus, walkMenu } from '../lib/hooks'
import { useNewSettings } from '../lib/whatsNew'
import { showLink } from '../lib/showLink'
import { playingNow } from '../lib/miniplayer'
import { parseTogether } from '../lib/together'
import { Centered, Spinner } from './ui'

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
  // Tailwind's sm: below it the main places are tabs along the bottom.
  const wide = useMediaQuery('(min-width: 40rem)')

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

  // A new page's heading takes focus and is said aloud; not on first load, nor between episodes (player keys).
  const main = useRef<HTMLElement>(null)
  const [arrived, setArrived] = useState('')
  const firstPage = useRef(true)
  useEffect(() => {
    if (firstPage.current) {
      firstPage.current = false
      return
    }
    // After the page's first paint, when its heading and title are there.
    const timer = window.setTimeout(() => {
      // Never out of a box being typed in or an open dialog.
      if (document.activeElement?.closest('input, textarea, select, [contenteditable], [role=dialog]')) return
      const h1 = main.current?.querySelector<HTMLElement>('h1')
      const heading = h1 ?? main.current
      if (!heading) return
      if (h1) h1.tabIndex = -1
      heading.focus({ preventScroll: true })
      // The heading names the page; the title may still be the last page's while its data loads.
      setArrived(h1?.textContent?.trim() || document.title)
    }, 150)
    return () => window.clearTimeout(timer)
  }, [pageKey])

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
      {/* The first stop for Tab: past the header's dozen controls to the page. */}
      <a
        href="#main"
        onClick={(e) => {
          e.preventDefault()
          main.current?.focus()
        }}
        className="sr-only z-[60] rounded-lg bg-accent-500 px-3 py-2 text-sm font-medium text-white focus:not-sr-only focus:fixed focus:top-2 focus:left-2"
      >
        Skip to content
      </a>
      <p role="status" className="sr-only">
        {arrived}
      </p>
      <Header />
      {/* Outside the per-page main: Back remounted them, and the pairing overlay vanished with its state. */}
      <Unpaired />
      <AccessRequests />
      {/* Outside main, which remounts per page: the docked player must outlive the watch page. */}
      <DockedWatch />
      <main
        ref={main}
        key={pageKey}
        id="main"
        tabIndex={-1}
        className={cx(
          'mx-auto w-full animate-enter outline-none',
          fullBleed ? 'pb-8' : 'max-w-[1600px] px-4 pt-4 pb-16 sm:px-6',
          // Clear of the tab bar on a phone.
          'max-sm:pb-[calc(5rem+env(safe-area-inset-bottom))]',
        )}
      >
        {/* Keyed by page, so leaving a broken one clears it. */}
        <ErrorBoundary key={pageKey} resetKey={pathname}>
          <Outlet />
        </ErrorBoundary>
      </main>
      {!wide && <BottomTabs />}
    </div>
  )
}

const Watch = lazy(() => import('../pages/Watch').then((m) => ({ default: m.Watch })))

type WatchTarget = { animeId: string; episode: string }
const watchPath = /^\/watch\/(-?\d+)\/(\d+)/

/** The one watch view: the watch page, then docked in a corner after leaving it while playing. */
function DockedWatch() {
  const match = useMatch('/watch/:animeId/:episode')
  const page = match ? { animeId: match.params.animeId!, episode: match.params.episode! } : null
  const [shown, setShown] = useState<WatchTarget | null>(null)
  const [docked, setDocked] = useState<WatchTarget | null>(null)
  // Set while rendering, so the view never unmounts for a frame between page and dock.
  if (page && (page.animeId !== shown?.animeId || page.episode !== shown?.episode)) {
    setShown(page)
    if (docked) setDocked(null)
  } else if (!page && shown) {
    setShown(null)
    setDocked(playingNow() ? shown : null)
  }
  const target = page ?? docked
  if (!target) return null
  return (
    <ErrorBoundary resetKey={`${target.animeId}-${target.episode}`}>
      <Suspense fallback={page ? <Centered><Spinner className="size-8" /></Centered> : null}>
        <Watch
          animeId={target.animeId}
          episode={target.episode}
          mini={!page}
          onGo={(path) => {
            const m = watchPath.exec(path)
            if (m) setDocked({ animeId: m[1], episode: m[2] })
          }}
          onClose={() => setDocked(null)}
        />
      </Suspense>
    </ErrorBoundary>
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
              'shrink-0 rounded-full px-3 py-1.5 text-sm transition-colors',
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
  // A pasted AniList or MyAnimeList show link, or a watch-together room, opens it rather than searching.
  const room = parseTogether(value)
  const link = showLink(value) ?? (room ? { site: 'room' as const, id: 0 } : null)
  const local = useQuery({
    enabled: q.length >= 2 && !link,
    queryKey: ['search', 'local', q],
    queryFn: () => api.get<{ results: LibraryItem[] }>(`/api/search/local?q=${encodeURIComponent(q)}&limit=8`),
    staleTime: 30_000,
  })
  const hits = q.length >= 2 && !link ? (local.data?.results ?? []) : []
  const showMenu = open && q.length >= 2

  const browse = () => {
    if (!value.trim()) return
    setOpen(false)
    if (link) {
      setValue('')
      navigate(`/open?url=${encodeURIComponent(value.trim())}`)
      return
    }
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
          {hits.length === 0 && local.isSuccess && !link && (
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
              {link?.site === 'room'
                ? 'Join this watch-together room'
                : link
                  ? `Open this ${link.site === 'anilist' ? 'AniList' : 'MyAnimeList'} link`
                  : `Search AniList for “${value.trim()}”`}
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
  const [exitAsk, setExitAsk] = useState<ExitStops | null>(null)
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
  // An update brought settings not looked at yet: Settings opens on them.
  const fresh = useNewSettings().unseen

  useEffect(() => {
    if (open && byKey.current) menuItems(menu.current)[0]?.focus()
  }, [open])

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
        {(attention || fresh) && (
          <span
            className={cx(
              'absolute -top-0.5 -right-0.5 size-2.5 rounded-full ring-2 ring-base-950',
              attention ? 'bg-amber-400' : 'bg-accent-400',
            )}
          />
        )}
      </button>

      {open && (
        <div
          ref={menu}
          role="menu"
          aria-label="Profile"
          onKeyDown={walkMenu}
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

          {MENU_ITEMS.map((item) => {
            const news = fresh && item.to === '/settings'
            return (
              <NavLink
                key={item.to}
                role="menuitem"
                to={news ? '/settings?tab=New' : item.to}
                onClick={close}
                className={({ isActive }) => cx(menuRow, isActive ? 'bg-base-800 text-white' : 'text-base-200')}
              >
                <span className={iconChip}>{item.icon}</span>
                {item.label}
                {news && <span className="ml-auto rounded bg-accent-500/20 px-1.5 py-0.5 text-[11px] font-medium text-accent-200">New</span>}
              </NavLink>
            )
          })}
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
          <InstallItem close={close} />
          <ExitItem
            onAsk={(stops) => {
              close()
              setExitAsk(stops)
            }}
          />
        </div>
      )}
      {/* Outside the menu: a click in it would close the menu, and a dialog inside with it. */}
      {exitAsk && <ExitDialog stops={exitAsk} onClose={() => setExitAsk(null)} />}
    </div>
  )
}

/** Install Yoru as an app: at once where the browser offers it, else to Settings, which says how. */
function InstallItem({ close }: { close: () => void }) {
  const state = useInstall()
  const navigate = useNavigate()
  if (state === 'installed') return null
  return (
    <button
      type="button"
      role="menuitem"
      onClick={() => {
        close()
        if (state === 'ready') void install()
        else navigate('/settings?tab=Devices')
      }}
      className={cx(menuRow, 'w-full text-left text-base-200')}
    >
      <span className={iconChip}>
        <InstallIcon />
      </span>
      Install Yoru
    </button>
  )
}

function InstallIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <rect x="6" y="2.5" width="12" height="19" rx="2.5" fill="none" stroke="currentColor" strokeWidth="1.7" />
      <path d="M12 7v7m0 0 3-3m-3 3-3-3" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

interface Watching {
  device: string
  title: string
  episode: number
}

/** What exiting would stop: downloads under way, and other devices playing through Yoru. */
interface ExitStops {
  downloads: number
  left: number
  watching: Watching[]
  /** Neither could be asked about: exiting asks, saying so, rather than guess nothing runs. */
  unknown?: boolean
}

function stopsOf(items: Download[] | undefined, watching: Watching[] | undefined): ExitStops {
  const active = (items ?? []).filter((d) => d.percent < 100 && !d.paused)
  return {
    downloads: active.length,
    left: active.reduce((sum, d) => sum + Math.max(0, d.totalBytes - d.bytesOnDisk), 0),
    watching: watching ?? [],
    unknown: items === undefined || watching === undefined,
  }
}

const stopsSummary = (s: ExitStops) =>
  [
    s.downloads > 0 && `${s.downloads} downloading`,
    s.watching.length === 1 && `${s.watching[0].device} is watching`,
    s.watching.length > 1 && `${s.watching.length} devices watching`,
  ]
    .filter(Boolean)
    .join(' · ')

/** Asks Yoru to exit; it answers before it stops. */
function useExitYoru() {
  const [exiting, setExiting] = useState(false)
  const [failed, setFailed] = useState('')
  const exit = () => {
    setFailed('')
    setExiting(true)
    api.post('/api/app/exit').catch((e: unknown) => {
      setExiting(false)
      setFailed(e instanceof Error ? e.message : "Couldn't exit Yoru")
    })
  }
  return { exit, exiting, failed }
}

/** On the machine Yoru runs on: says what runs, and exits at once when nothing would stop; asks otherwise. */
function ExitItem({ onAsk }: { onAsk: (stops: ExitStops) => void }) {
  const access = useAccess()
  const host = !!access.data?.host
  const downloads = useDownloads()
  const activity = useQuery({
    queryKey: ['app-activity'],
    queryFn: () => api.get<{ watching: Watching[] }>('/api/app/activity'),
    enabled: host,
    staleTime: 0,
    refetchInterval: 5000,
  })
  const { exit, exiting, failed } = useExitYoru()
  const [checking, setChecking] = useState(false)
  if (!host) return null

  const summary = stopsSummary(stopsOf(downloads.data?.items, activity.data?.watching))

  // Decided on fresh answers: the menu's may be seconds old, or not in yet.
  const choose = async () => {
    setChecking(true)
    const [d, a] = await Promise.all([downloads.refetch(), activity.refetch()])
    setChecking(false)
    const stops = stopsOf(d.data?.items, a.data?.watching)
    if (stops.unknown || stopsSummary(stops)) return onAsk(stops)
    exit()
  }
  return (
    <>
      <div className="my-2 h-px bg-base-750" />
      <button
        type="button"
        role="menuitem"
        onClick={() => void choose()}
        disabled={exiting || checking}
        className={cx(menuRow, 'w-full text-left text-base-200')}
      >
        <span className={iconChip}>
          <PowerIcon />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block">{exiting ? 'Yoru is exiting…' : 'Exit Yoru'}</span>
          {summary && !exiting && <span className="block truncate text-xs text-base-400">{summary}</span>}
        </span>
      </button>
      {failed && <p className="px-2 pt-1 text-xs text-recap">{failed}</p>}
    </>
  )
}

/** Exiting would stop something: what, in the middle of the screen, with Cancel the safe default. */
function ExitDialog({ stops, onClose }: { stops: ExitStops; onClose: () => void }) {
  const panel = useDismiss<HTMLDivElement>(onClose)
  useModalFocus(panel)
  const { exit, exiting, failed } = useExitYoru()
  return createPortal(
    <div className="fixed inset-0 z-[60] flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
      <div
        ref={panel}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="exit-title"
        aria-describedby="exit-stops"
        className="animate-fade-in relative w-full max-w-sm rounded-xl border border-base-800 bg-base-900 p-5 shadow-2xl"
      >
        <h2 id="exit-title" className="text-base font-semibold text-white">
          Exit Yoru?
        </h2>
        <div id="exit-stops">
          {stops.unknown && (
            <p className="mt-1 text-sm text-base-400">Yoru couldn't check for downloads or other devices watching.</p>
          )}
          {(stops.downloads > 0 || stops.watching.length > 0) && (
            <p className="mt-1 text-sm text-base-400">These stop until you open Yoru again:</p>
          )}
          <ul className="mt-2 list-disc space-y-1 pl-5 text-sm text-base-200">
            {stops.downloads > 0 && (
              <li>
                {stops.downloads === 1 ? '1 download' : `${stops.downloads} downloads`}
                {stops.left > 0 && ` (${bytes(stops.left)} left)`}
              </li>
            )}
            {stops.watching.map((w, i) => (
              <li key={`${w.device}-${i}`}>
                {w.device} is watching {w.title || 'an episode'}
                {w.episode > 0 && `, episode ${w.episode}`}
              </li>
            ))}
          </ul>
        </div>
        {failed && <p className="mt-3 text-xs text-recap">{failed}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" onClick={onClose} className={buttonClass('secondary')}>
            Cancel
          </button>
          <button type="button" onClick={exit} disabled={exiting} className={buttonClass('danger')}>
            {exiting ? 'Exiting…' : 'Exit Yoru'}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

function PowerIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M12 4v7" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      <path d="M7.4 7.1a7 7 0 1 0 9.2 0" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  )
}

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
