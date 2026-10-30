import { lazy, Suspense, useCallback, useEffect, useRef, useState, type RefObject } from 'react'
import {
  Link,
  NavLink,
  Outlet,
  useLocation,
  useMatch,
  useNavigate,
  useNavigationType,
} from 'react-router-dom'
import { LIST_STATUSES } from '../lib/api'
import { cx } from '../lib/format'
import { useAccess, useLibraryCounts, useSetup } from '../lib/queries'
import { BottomTabs } from './BottomTabs'
import { install, useInstall } from '../lib/install'
import { ErrorBoundary } from './ErrorBoundary'
import { Unpaired } from './Unpaired'
import { AccessRequests } from './AccessRequests'
import { DownloadsPanel } from './DownloadsPanel'
import { ExitDialog } from './ExitDialog'
import { useExitCheck, type ExitStops } from './exit'
import { SHOW_SHORTCUTS } from './keys'
import { NotificationPanel } from './NotificationPanel'
import {
  historyIndex,
  isTabPage,
  pageName,
  parentOf,
  placeOf,
  roomyPage,
  settingsCategory,
  studioName,
  type Parent,
  type Place,
} from './places'
import { SearchBox } from './SearchBox'
import { Shortcuts } from './Shortcuts'
import { SavedCopyNotice } from './SavedCopyNotice'
import { menuItems, useDismiss, useMediaQuery, walkMenu } from '../lib/hooks'
import { useNewSettings } from '../lib/whatsNew'
import { playingNow } from '../lib/miniplayer'
import { useFocus } from '../lib/focus'
import { Centered, Spinner } from './ui'
import { useAccount, useDeviceLine, useNoLine, usePcLine, useTitleLanguage, type DownloadsLine } from './you'
import { Avatar, Chevron, MiniRing } from './YouBits'

const BAR = 52

export function Layout() {
  const fullBleed = useMatch('/watch/:animeId/:episode') !== null
  const wide = useMediaQuery('(min-width: 40rem)')
  const focus = useFocus()
  const { pathname, search } = useLocation()
  const backBar = !wide && !fullBleed && !isTabPage(pathname, search)

  const pageKey = fullBleed ? 'watch' : pathname

  const navigation = useNavigationType()
  useEffect(() => {
    if (navigation !== 'POP') window.scrollTo(0, 0)
  }, [pathname, navigation])

  useTrail()

  const main = useRef<HTMLElement>(null)
  const [arrived, setArrived] = useState('')
  const firstPage = useRef(true)
  useEffect(() => {
    if (firstPage.current) {
      firstPage.current = false
      return
    }
    const timer = window.setTimeout(() => {
      if (document.activeElement?.closest('input, textarea, select, [contenteditable], [role=dialog]')) return
      const h1 = main.current?.querySelector<HTMLElement>('h1')
      const heading = h1 ?? main.current
      if (!heading) return
      if (h1) h1.tabIndex = -1
      heading.focus({ preventScroll: true })
      setArrived(h1?.textContent?.trim() || document.title)
    }, 150)
    return () => window.clearTimeout(timer)
  }, [pageKey])

  const setup = useSetup()
  const navigate = useNavigate()
  const nudged = useRef(false)
  useEffect(() => {
    if (nudged.current || !setup.data || pathname === '/setup') return
    if (setup.data.ready && setup.data.indexers > 0) return
    nudged.current = true
    navigate('/setup', { replace: true })
  }, [setup.data, pathname, navigate])

  const heading = usePageHeading(main, pageKey, backBar)
  const category = settingsCategory(pathname, search)
  const studio = studioName(pathname, search)
  const title = studio ?? (heading.text || category || pageName(pathname, search) || '')
  const titleShown = !!studio || !!category || !heading.text || heading.hidden || heading.away

  return (
    <div className="min-h-dvh">
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
      {!focus && (
        <Header
          wide={wide}
          bare={fullBleed && !wide}
          back={backBar ? { parent: parentOf(pathname, search), title, shown: titleShown, tall: !!studio } : null}
        />
      )}
      <Unpaired />
      <AccessRequests />
      <DockedWatch />
      <main
        ref={main}
        key={pageKey}
        id="main"
        tabIndex={-1}
        className={cx(
          'mx-auto w-full animate-enter outline-none',
          fullBleed ? 'pb-8' : 'max-w-[1600px] px-4 pt-4 pb-16 sm:px-6',
          !fullBleed && roomyPage(pathname, search) && 'min-[1800px]:max-w-[2400px] min-[1800px]:px-12',
          !fullBleed && 'max-sm:pb-[calc(5rem+env(safe-area-inset-bottom))]',
          backBar && '[&_h1.page-title]:sr-only',
        )}
      >
        <ErrorBoundary key={pageKey} resetKey={pathname}>
          <Outlet />
        </ErrorBoundary>
      </main>
      {!wide && !focus && !fullBleed && <BottomTabs />}
    </div>
  )
}

const trail = new Map<number, string>()

function useTrail() {
  const { pathname, search } = useLocation()
  useEffect(() => {
    trail.set(historyIndex(), pathname + search)
  }, [pathname, search])
}

function isParent(entry: string | undefined, to: string) {
  if (!entry) return false
  const [path, qs = ''] = entry.split('?')
  return path === to && (to !== '/settings' || !new URLSearchParams(qs).has('tab'))
}

function usePageHeading(main: RefObject<HTMLElement | null>, pageKey: string, on: boolean) {
  const [found, setFound] = useState<{ el: HTMLElement | null; text: string; hidden: boolean }>({
    el: null,
    text: '',
    hidden: false,
  })
  const [away, setAway] = useState(false)

  useEffect(() => {
    const root = main.current
    if (!on || !root) return
    const read = () => {
      const el = root.querySelector<HTMLElement>('h1')
      const text = el?.textContent?.trim() ?? ''
      const hidden = !!el && (el.classList.contains('page-title') || el.offsetWidth <= 1)
      setFound((prev) => (prev.el === el && prev.text === text && prev.hidden === hidden ? prev : { el, text, hidden }))
    }
    read()
    const watcher = new MutationObserver(read)
    watcher.observe(root, { childList: true, subtree: true, characterData: true })
    return () => {
      watcher.disconnect()
      setFound({ el: null, text: '', hidden: false })
    }
  }, [main, pageKey, on])

  useEffect(() => {
    const el = found.el
    if (!el) return
    const watcher = new IntersectionObserver(
      ([entry]) => setAway(!entry.isIntersecting && entry.boundingClientRect.top < BAR),
      { rootMargin: `-${BAR}px 0px 0px 0px` },
    )
    watcher.observe(el)
    return () => {
      watcher.disconnect()
      setAway(false)
    }
  }, [found.el])

  return { text: on ? found.text : '', hidden: found.hidden, away }
}

const Watch = lazy(() => import('../pages/Watch').then((m) => ({ default: m.Watch })))

type WatchTarget = { animeId: string; episode: string }
const watchPath = /^\/watch\/(-?\d+)\/(\d+)/

function DockedWatch() {
  const match = useMatch('/watch/:animeId/:episode')
  const page = match ? { animeId: match.params.animeId!, episode: match.params.episode! } : null
  const [shown, setShown] = useState<WatchTarget | null>(null)
  const [docked, setDocked] = useState<WatchTarget | null>(null)
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

interface Back {
  parent: Parent
  title: string
  shown: boolean
  tall: boolean
}

function Header({ wide, back, bare }: { wide: boolean; back: Back | null; bare: boolean }) {
  const [scrolled, setScrolled] = useState(false)
  const [tucked, setTucked] = useState(false)
  const hides = !wide && !back && !bare

  useEffect(() => {
    let last = window.scrollY
    const onScroll = () => {
      const y = window.scrollY
      setScrolled(y > 8)
      if (!hides || y < 48) setTucked(false)
      else if (y > last + 6) setTucked(true)
      else if (y < last - 6) setTucked(false)
      last = y
    }
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => window.removeEventListener('scroll', onScroll)
  }, [hides])

  useEffect(() => {
    document.documentElement.style.setProperty('--bar', tucked || bare ? '0px' : '4rem')
  }, [tucked, bare])

  return (
    <header
      className={cx(
        'sticky top-0 z-40 transition-[background-color,transform] duration-200',
        tucked && '-translate-y-full',
        !scrolled
          ? 'bg-transparent'
          : wide
            ? 'bg-base-950/75 shadow-[0_1px_0_rgb(255_255_255/0.05)] backdrop-blur-xl backdrop-saturate-150'
            : 'bg-base-950 shadow-[0_1px_0_rgb(255_255_255/0.06)]',
      )}
    >
      {wide ? <DesktopBar /> : bare ? null : back ? <BackBar {...back} /> : <LogoBar />}
      {!wide && (
        <div hidden>
          <NotificationPanel />
        </div>
      )}
      <Shortcuts />
      <SavedCopyNotice />
    </header>
  )
}

function Logo() {
  return (
    <Link to="/" className="group flex shrink-0 items-baseline gap-1">
      <span className="font-display text-2xl font-bold tracking-tight text-white">Yoru</span>
      <span className="size-1.5 rounded-full bg-accent-500 transition-colors group-hover:bg-accent-400" />
    </Link>
  )
}

function LogoBar() {
  return (
    <div className="flex h-16 items-center px-4">
      <Logo />
    </div>
  )
}

function BackBar({ parent, title, shown, tall }: Back) {
  const navigate = useNavigate()
  useLocation()
  const idx = historyIndex()
  const home = !parent.to && idx === 0
  const label = home ? 'Home' : parent.label
  const up = () => {
    if (!parent.to) {
      if (idx > 0) navigate(-1)
      else navigate('/')
      return
    }
    if (idx > 0 && isParent(trail.get(idx - 1), parent.to)) navigate(-1)
    else navigate(parent.to, { replace: true })
  }
  return (
    <div className={cx('flex items-center px-1', tall ? 'h-16' : 'h-13')}>
      <div className="flex flex-1 basis-0">
        <button
          type="button"
          onClick={up}
          aria-label={parent.to || home ? `Back to ${label}` : 'Back'}
          className="press flex min-h-11 min-w-11 items-center gap-0.5 rounded-lg pr-2 text-base font-medium whitespace-nowrap text-accent-300 transition-colors active:text-accent-200"
        >
          <svg viewBox="0 0 24 24" className="size-7 shrink-0" aria-hidden>
            <path d="M14.5 5.5 8 12l6.5 6.5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          {label}
        </button>
      </div>
      <p
        aria-hidden
        className={cx(
          'truncate px-1 text-center text-base font-semibold text-base-100 transition-opacity duration-200',
          !shown && 'opacity-0',
        )}
      >
        {title}
      </p>
      <div className="flex-1 basis-0" />
    </div>
  )
}

function DesktopBar() {
  return (
    <div className="mx-auto flex h-16 w-full max-w-[1600px] items-center gap-2 px-4 lg:gap-4 lg:px-6 min-[1800px]:max-w-[2400px] min-[1800px]:px-12">
      <BackButton />
      <Logo />
      <span className="h-6 w-px shrink-0 bg-base-800" />
      <nav aria-label="Main" className="flex items-center gap-1 self-stretch">
        <Tabs />
      </nav>
      <span aria-hidden className="flex-1 lg:hidden" />
      <SearchBox />
      <TitleLanguage />
      <DownloadsPanel />
      <NotificationPanel />
      <ProfileMenu />
    </div>
  )
}

const FURTHEST_KEY = 'yoru.history.furthest'
const readFurthest = () => {
  try {
    return Number(sessionStorage.getItem(FURTHEST_KEY)) || 0
  } catch {
    return 0
  }
}

function BackButton() {
  const navigate = useNavigate()
  useLocation()
  const idx = historyIndex()
  const furthest = useNavigationType() === 'PUSH' ? idx : Math.max(readFurthest(), idx)
  useEffect(() => {
    try {
      sessionStorage.setItem(FURTHEST_KEY, String(furthest))
    } catch {
      return
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
    <div className="flex shrink-0 items-center max-md:hidden">
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

const NAV: { place: Place; to: string; label: string }[] = [
  { place: 'home', to: '/', label: 'Home' },
  { place: 'search', to: '/browse', label: 'Search' },
  { place: 'schedule', to: '/schedule', label: 'Schedule' },
  { place: 'library', to: '/library', label: 'Library' },
]

function Tabs() {
  const here = placeOf(useLocation().pathname)
  return (
    <>
      {NAV.map((tab) => {
        const active = here === tab.place
        return (
          <Link
            key={tab.to}
            to={tab.to}
            aria-current={active ? 'page' : undefined}
            className={cx(
              'relative flex shrink-0 items-center self-stretch px-2.5 text-sm font-medium transition-colors lg:px-3',
              'after:absolute after:inset-x-2.5 after:bottom-0 after:h-0.5 after:rounded-full after:transition-colors lg:after:inset-x-3',
              active ? 'text-white after:bg-accent-400' : 'text-base-400 hover:text-white after:bg-transparent',
            )}
          >
            {tab.label}
          </Link>
        )
      })}
    </>
  )
}

function TitleLanguage() {
  const { japanese, settling, choose } = useTitleLanguage()
  return (
    <button
      type="button"
      onClick={() => choose(!japanese)}
      disabled={settling}
      aria-label={japanese ? 'Titles: JP. Switch to EN' : 'Titles: EN. Switch to JP'}
      title={japanese ? 'Japanese (romaji) titles; click for English' : 'English titles; click for Japanese (romaji)'}
      className="tap group/titles relative flex shrink-0 items-center gap-0.5 rounded-lg p-0.5 text-xs font-semibold ring-1 ring-white/10 transition-colors hover:ring-white/20"
    >
      {['EN', 'JP'].map((label) => (
        <span
          key={label}
          className={cx(
            'rounded-md px-2 py-1 transition-colors',
            (label === 'JP') === japanese ? 'bg-base-800 text-white' : 'text-base-500 group-hover/titles:text-base-300',
          )}
        >
          {label}
        </span>
      ))}
    </button>
  )
}

const MENU_ITEMS = [
  { to: '/local', label: 'Local files', icon: <FolderIcon /> },
  { to: '/settings', label: 'Settings', icon: <GearIcon /> },
]

function ProfileMenu() {
  const [open, setOpen] = useState(false)
  const [exitAsk, setExitAsk] = useState<ExitStops | null>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const byKey = useRef(false)
  const close = useCallback(() => setOpen(false), [])
  const dismiss = useCallback(() => {
    if (menu.current?.contains(document.activeElement)) trigger.current?.focus()
    setOpen(false)
  }, [])
  const ref = useDismiss<HTMLDivElement>(dismiss, open)

  const { user, signedIn, attention } = useAccount()
  const fresh = useNewSettings().unseen
  const access = useAccess()
  const youHere = placeOf(useLocation().pathname) === 'you'

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
          (open || youHere) && 'ring-2 ring-base-100 ring-offset-2 ring-offset-base-950',
        )}
      >
        <Avatar name={user} className="size-full" />
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
          className="absolute right-0 z-50 mt-2.5 w-80 max-w-[calc(100vw-1rem)] origin-top-right animate-rise overflow-hidden rounded-sheet bg-base-750 p-2 shadow-lift backdrop-blur-xl"
        >
          <Link
            role="menuitem"
            to="/settings?tab=Accounts"
            onClick={close}
            className="group flex items-center gap-3 rounded-xl p-2 transition-colors hover:bg-base-800 focus-visible:bg-base-800 focus-visible:outline-hidden"
          >
            <Avatar name={user} className="size-10 ring-1 ring-white/10" letter="text-lg" />
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
            <Chevron className="transition-colors group-hover:text-base-300" />
          </Link>

          <div className="my-2 h-px bg-base-750" />
          <ListShortcuts close={close} />
          <div className="my-2 h-px bg-base-750" />

          {access.data?.host ? (
            <DownloadsItem key="pc" close={close} useLine={usePcLine} />
          ) : access.data ? (
            <DownloadsItem key="device" close={close} useLine={useDeviceLine} />
          ) : (
            <DownloadsItem key="none" close={close} useLine={useNoLine} />
          )}
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
                {news && <span className="ml-auto rounded bg-accent-500/20 px-1.5 py-0.5 text-xs font-medium text-accent-200">New</span>}
              </NavLink>
            )
          })}
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              close()
              window.dispatchEvent(new Event(SHOW_SHORTCUTS))
            }}
            className={cx(menuRow, 'w-full text-left text-base-200')}
          >
            <span className={iconChip}>
              <KeyboardIcon />
            </span>
            Keyboard shortcuts
            <kbd className="ml-auto rounded border border-base-700 bg-base-800 px-1.5 font-mono text-xs leading-5 text-base-400">
              ?
            </kbd>
          </button>
          <InstallItem close={close} />
          {access.data?.host && (
            <ExitItem
              onAsk={(stops) => {
                close()
                setExitAsk(stops)
              }}
            />
          )}
        </div>
      )}
      {exitAsk && <ExitDialog stops={exitAsk} onClose={() => setExitAsk(null)} />}
    </div>
  )
}

function DownloadsItem({ close, useLine }: { close: () => void; useLine: () => DownloadsLine }) {
  const { activity, words, idle } = useLine()
  return (
    <NavLink
      role="menuitem"
      to="/downloads"
      onClick={close}
      className={({ isActive }) => cx(menuRow, isActive ? 'bg-base-800 text-white' : 'text-base-200')}
    >
      <span className={iconChip}>
        <DownloadIcon />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block">Downloads</span>
        {!idle && words && <span className="block truncate text-xs text-base-400">{words}</span>}
      </span>
      <MiniRing activity={activity} />
    </NavLink>
  )
}

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

function ExitItem({ onAsk }: { onAsk: (stops: ExitStops) => void }) {
  const { summary, choose, exiting, checking, failed } = useExitCheck(onAsk)
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

const menuRow =
  'group flex items-center gap-3 rounded-lg px-2 py-1.5 text-sm transition-colors hover:bg-base-800 hover:text-white focus-visible:bg-base-800 focus-visible:text-white focus-visible:outline-hidden pointer-coarse:py-2'
const iconChip =
  'grid size-7 shrink-0 place-items-center rounded-md bg-base-800 text-base-400 ring-1 ring-white/5 transition-colors group-hover:bg-base-750 group-hover:text-base-100 group-focus-visible:text-base-100'

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
          className="tap relative rounded text-xs font-medium text-accent-300 transition-colors hover:text-accent-300 focus-visible:text-accent-300 focus-visible:outline-hidden"
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
              className="flex min-h-10 items-center justify-between gap-2 rounded-lg bg-base-800 px-3 text-sm text-base-200 transition-colors hover:bg-base-750 hover:text-white focus-visible:bg-base-750 focus-visible:text-white focus-visible:outline-hidden pointer-coarse:min-h-11"
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

function InstallIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <rect x="6" y="2.5" width="12" height="19" rx="2.5" fill="none" stroke="currentColor" strokeWidth="1.7" />
      <path d="M12 7v7m0 0 3-3m-3 3-3-3" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
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

function KeyboardIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <rect x="3" y="6" width="18" height="12" rx="2" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path d="M7 10h.01M10.5 10h.01M14 10h.01M17 10h.01M8 14h8" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
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
