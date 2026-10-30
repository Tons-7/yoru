import {
  createContext,
  memo,
  useCallback,
  useContext,
  useDeferredValue,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { api, ApiError, withToken, type Tracker } from '../lib/api'
import { bytes, cx, relativeTime, speedLimit, waitLabel, type SpeedUnit } from '../lib/format'
import { castImpossible } from '../player/cast'
import {
  refreshDownloads,
  refreshListState,
  useAccess,
  useAccessDevices,
  useDecideDevice,
  usePrefs,
  useSetPref,
  useSetup,
  useTrackers,
  type AccessDevice,
  type AccessState,
  type ApprovalMode,
} from '../lib/queries'
import { ANIME4K_MODES, ANIME4K_SIZES } from '../lib/anime4k'
import { ComponentState } from '../components/ComponentState'
import { ReleaseNotesList, type Notes } from '../components/ReleaseNotes'
import { SitesEditor } from '../components/SitesEditor'
import { WhereThingsGo } from '../components/WhereThingsGo'
import { WaitSelect } from '../components/WaitSelect'
import { SHOW_SHORTCUTS } from '../components/keys'
import { install, INSTALL_HELP, useInstall } from '../lib/install'
import { asSubLook, previewStyle, SUB_LOOKS } from '../player/subLook'
import { ON_MOBILE, QUALITIES, setDeviceQuality, setOnMobile, useNetwork, type OnMobile, type Quality } from '../player/network'
import { ENCODERS } from '../player/encoders'
import { SEEN_KEY, useNewSettings } from '../lib/whatsNew'
import { HOME_ROWS, hiddenRows } from '../lib/homeRows'
import { ReleasePicker } from '../components/ReleasePicker'
import { DESKTOP_NOTIFY_KEY, desktopNotifyWanted } from '../lib/notifications'
import { disablePush, enablePush, pushState, testPush, type PushState } from '../lib/push'
import {
  Button,
  ConfirmInline,
  ErrorState,
  inputClass,
  PageHeader,
  ProgressBar,
  SaveHint,
  SearchInput,
  Segmented,
  Select,
  Skeleton,
  Spinner,
  Switch as UiSwitch,
  TextInput,
} from '../components/ui'
import { buttonClass, ControlLabel } from '../lib/ui'
import { useDebounced, useHeldData } from '../lib/hooks'
import { copyText } from '../lib/downloads'

// The id is what ?tab= says; the label is what a person reads.
const TABS = [
  // Only after an update brought settings (see whatsNew.ts).
  { id: 'New', label: 'New' },
  { id: 'Playback', label: 'Playback' },
  { id: 'Language', label: 'Language & display' },
  { id: 'Sound', label: 'Sound & picture' },
  { id: 'Downloads', label: 'Downloads' },
  { id: 'Storage', label: 'Storage' },
  { id: 'Accounts', label: 'Accounts' },
  { id: 'Library', label: 'Library' },
  { id: 'Notifications', label: 'Notifications' },
  { id: 'Devices', label: 'Devices' },
  { id: 'Computer', label: 'This computer' },
  { id: 'About', label: 'About' },
] as const
type Tab = (typeof TABS)[number]['id']
// Tabs before the regrouping, still in links, bookmarks and update notices.
const RENAMED: Record<string, Tab> = { Quality: 'Downloads', Trackers: 'Accounts', Access: 'Devices', Jobs: 'About' }
const isTab = (v: string | null): v is Tab => TABS.some((t) => t.id === v)

export function Settings() {
  // The OAuth callback and update notifications land here on their tab (?tab=).
  const [params, setParams] = useSearchParams()
  const connected = params.get('connected')
  const wanted = params.get('tab')
  const renamed = wanted ? RENAMED[wanted] : undefined
  const fresh = useNewSettings()
  const tabs = TABS.filter((t) => t.id !== 'New' || fresh.rows.length + fresh.changes.length > 0)
  // The URL holds the tab: a reload keeps it, and a link to another one always switches.
  const asked: Tab = isTab(wanted) ? wanted : (renamed ?? (connected ? 'Accounts' : 'Playback'))
  const tab: Tab = tabs.some((t) => t.id === asked) ? asked : 'Playback'
  // So does the search (?q=), so a link can open on the setting it is about. The field keeps its own text:
  // a router update lands in a transition, a keystroke behind, which moved the caret and dropped letters.
  const [text, setText] = useState(params.get('q') ?? '')
  const typed = useDebounced(text, 300)
  useEffect(() => {
    const next = new URLSearchParams(params)
    if (typed) next.set('q', typed)
    else next.delete('q')
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [typed])
  const fromURL = params.get('q') ?? ''
  useEffect(() => {
    // A link or a tab change rewrote it; not our own write, which would land mid-word.
    if (fromURL !== typed) setText(fromURL)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fromURL])
  const query = text.trim().toLowerCase()
  // Every tab renders under a search; deferred, typing stays quick while the results catch up.
  const shownQuery = useDeferredValue(query)
  const setTab = (t: Tab) => {
    const next = new URLSearchParams(params)
    next.set('tab', t)
    next.delete('q')
    setText('')
    setParams(next, { replace: true })
  }
  // The latest handlers behind steady ones, or the memoised results would redraw on every keystroke anyway.
  const handlers = useRef({ setTab, clear: () => setText('') })
  useLayoutEffect(() => {
    handlers.current = { setTab, clear: () => setText('') }
  })
  const openTab = useCallback((t: Tab) => handlers.current.setTab(t), [])
  const clearSearch = useCallback(() => handlers.current.clear(), [])

  return (
    // Centred as one block: the slack becomes margins, not a gulf between every label and its control.
    <div className="mx-auto max-w-[59rem] space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
        <PageHeader title="Settings" />
        <SearchInput
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => e.key === 'Escape' && setText('')}
          placeholder="Search settings"
          aria-label="Search settings"
          className="w-full sm:w-72"
        />
      </div>

      {connected && (
        <div className="animate-fade-in flex items-center justify-between gap-4 rounded-lg border border-accent-500/40 bg-accent-500/10 px-3 py-2">
          <p className="text-sm text-accent-200">
            Connected to {connected === 'mal' ? 'MyAnimeList' : 'AniList'}.
          </p>
          <button
            onClick={() => setParams({ tab }, { replace: true })}
            aria-label="Dismiss"
            className="tap relative rounded px-2 text-accent-300/70 hover:text-accent-100"
          >
            ✕
          </button>
        </div>
      )}

      {query ? (
        <SearchResults query={shownQuery || query} onOpen={openTab} onClear={clearSearch} />
      ) : (
        <>
          {/* Beside the settings on a desktop; on a phone a menu, as a strip hid the last tabs off-screen. */}
          <Select value={tab} onChange={(v) => setTab(v as Tab)} aria-label="Settings section" className="lg:hidden">
            {tabs.map((t) => (
              <option key={t.id} value={t.id}>
                {t.id === 'New' && fresh.unseen ? 'New ●' : t.label}
              </option>
            ))}
          </Select>

          <div className="flex gap-6">
            <SideTabs tab={tab} tabs={tabs} unseen={fresh.unseen} onChange={setTab} />
            <div className="animate-fade-in w-full max-w-[47rem] min-w-0">
              <TabBody tab={tab} />
            </div>
          </div>
        </>
      )}
    </div>
  )
}

function TabBody({ tab }: { tab: Tab }) {
  switch (tab) {
    case 'New':
      return <NewSettings />
    case 'Playback':
      return <PlaybackTab />
    case 'Language':
      return <LanguageTab />
    case 'Computer':
      return <ComputerTab />
    case 'Sound':
      return <SoundTab />
    case 'Downloads':
      return <DownloadsTab />
    case 'Storage':
      return <StorageTab />
    case 'Accounts':
      return <AccountsTab />
    case 'Library':
      return <LibraryTab />
    case 'Notifications':
      return <NotificationsTab />
    case 'Devices':
      return <DevicesTab />
    case 'About':
      return <AboutTab />
  }
}

// The search: lower-cased words, every one of which a setting's words must hold.
const SearchQuery = createContext('')
// The New tab: the labels of the rows it keeps.
const OnlyRows = createContext<Set<string> | null>(null)
// The section's own title matched, so all of it shows.
const SectionMatched = createContext(false)

function matches(query: string, ...texts: Array<string | undefined>) {
  const words = texts.filter(Boolean).join(' ').toLowerCase()
  return query.split(/\s+/).every((w) => words.includes(w))
}

// Every tab at once, each showing only its matching settings; a tab with none drops out.
const SearchResults = memo(function SearchResults({
  query,
  onOpen,
  onClear,
}: {
  query: string
  onOpen: (t: Tab) => void
  onClear: () => void
}) {
  return (
    <SearchQuery.Provider value={query}>
      <div className="animate-fade-in mx-auto max-w-[47rem] space-y-6 [&:not(:has([data-shown])):not(:has([data-loading]))>[data-empty]]:block">
        {TABS.filter((t) => t.id !== 'New').map((t) => (
          <div key={t.id} className="space-y-3 [&:not(:has([data-shown]))]:hidden">
            <button
              onClick={() => onOpen(t.id)}
              className="flex items-center gap-2 rounded-md text-sm font-medium text-base-300 transition-colors hover:text-white"
            >
              <TabIcon tab={t.id} />
              {t.label} ›
            </button>
            <SectionMatched.Provider value={matches(query, t.label)}>
              <TabBody tab={t.id} />
            </SectionMatched.Provider>
          </div>
        ))}
        <div data-empty className="surface hidden p-6 text-center">
          <p className="text-sm text-base-300">No setting matches “{query}”.</p>
          <button onClick={onClear} className={cx(buttonClass(), 'mt-3')}>
            Clear search
          </button>
        </div>
      </div>
    </SearchQuery.Provider>
  )
})

function SideTabs({ tab, tabs, unseen, onChange }: { tab: Tab; tabs: readonly (typeof TABS)[number][]; unseen: boolean; onChange: (t: Tab) => void }) {
  return (
    // Stays beside a long tab instead of scrolling away with its top.
    <div role="tablist" className="sticky top-20 hidden w-44 shrink-0 flex-col gap-0.5 self-start lg:flex">
      {tabs.map((t) => (
        <button
          key={t.id}
          role="tab"
          aria-selected={t.id === tab}
          onClick={() => onChange(t.id)}
          className={cx(
            'flex items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm transition-colors',
            t.id === tab
              ? 'bg-accent-500/15 font-medium text-accent-200 shadow-[inset_2px_0_0_var(--color-accent-500)]'
              : 'text-base-400 hover:bg-base-900 hover:text-base-100',
          )}
        >
          <TabIcon tab={t.id} />
          {t.label}
          {t.id === 'New' && unseen && <span aria-label="not seen yet" className="ml-auto size-2 rounded-full bg-accent-400" />}
        </button>
      ))}
    </div>
  )
}

// What this update added to Settings, as the real controls, and what changed with no setting of its own.
function NewSettings() {
  const fresh = useNewSettings()
  const setPref = useSetPref()
  const rows = useMemo(() => new Set(fresh.rows.map((r) => r.label)), [fresh.rows])
  return (
    <div className="space-y-4">
      <Section title={`Since ${fresh.from}`} hint="New and changed in the updates since the version you had. Tweak them here or in their own tabs.">
        {fresh.changes.map((c) => (
          <div key={c.what} data-row className="py-3">
            <p className="text-sm text-base-100">{c.what}</p>
            <p className="mt-0.5 text-xs text-base-500">
              Before: {c.before} · Now: {c.now}
            </p>
          </div>
        ))}
        <div data-row className="py-3">
          <Button size="sm" disabled={!fresh.unseen} onClick={() => setPref.mutate({ key: SEEN_KEY, value: fresh.current })}>
            {fresh.unseen ? 'Seen them' : 'Seen'}
          </Button>
        </div>
      </Section>
      <OnlyRows.Provider value={rows}>
        {TABS.filter((t) => t.id !== 'New').map((t) => (
          <div key={t.id} className="space-y-3 [&:not(:has([data-shown]))]:hidden">
            <p className="flex items-center gap-2 text-sm font-medium text-base-300">
              <TabIcon tab={t.id} />
              {t.label}
            </p>
            <TabBody tab={t.id} />
          </div>
        ))}
      </OnlyRows.Provider>
    </div>
  )
}

// One line icon per tab, drawn as the app's other glyphs are (24 grid, 1.6 stroke).
const TAB_ICONS: Record<Tab, ReactNode> = {
  New: <path d="M12 3.5 13.8 9l5.7.1-4.6 3.4 1.7 5.5L12 14.7 7.4 18l1.7-5.5-4.6-3.4 5.7-.1L12 3.5Z" strokeLinejoin="round" />,
  Playback: <path d="M8.5 5.5v13l10-6.5-10-6.5Z" strokeLinejoin="round" />,
  Language: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M3.5 12h17M12 3.5c2.3 2.3 3.5 5.3 3.5 8.5s-1.2 6.2-3.5 8.5c-2.3-2.3-3.5-5.3-3.5-8.5s1.2-6.2 3.5-8.5Z" />
    </>
  ),
  Computer: (
    <>
      <rect x="3.5" y="4.5" width="17" height="11.5" rx="1.5" />
      <path d="M9 20h6M12 16v4" strokeLinecap="round" />
    </>
  ),
  Sound: (
    <>
      <path d="M4.5 9.5h3l4-3.5v12l-4-3.5h-3v-5Z" strokeLinejoin="round" />
      <path d="M15 9.5a3.5 3.5 0 0 1 0 5M17.5 7a7 7 0 0 1 0 10" strokeLinecap="round" />
    </>
  ),
  Downloads: <path d="M12 4v10m0 0 4-4m-4 4-4-4M5 18.5h14" strokeLinecap="round" strokeLinejoin="round" />,
  Storage: (
    <>
      <ellipse cx="12" cy="6.5" rx="7" ry="2.5" />
      <path d="M5 6.5v11c0 1.4 3.1 2.5 7 2.5s7-1.1 7-2.5v-11M5 12c0 1.4 3.1 2.5 7 2.5s7-1.1 7-2.5" />
    </>
  ),
  Accounts: (
    <>
      <circle cx="12" cy="8.5" r="3.5" />
      <path d="M5 20c1-3.5 3.8-5 7-5s6 1.5 7 5" strokeLinecap="round" />
    </>
  ),
  Library: (
    <path
      d="M4 7.5A1.5 1.5 0 0 1 5.5 6h3.4a1.5 1.5 0 0 1 1.2.6l.9 1.2h7.5A1.5 1.5 0 0 1 20 9.3v8.2a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 17.5v-10Z"
      strokeLinejoin="round"
    />
  ),
  Notifications: (
    <>
      <path d="M6.5 16.5V11a5.5 5.5 0 0 1 11 0v5.5l1.5 1.5H5l1.5-1.5Z" strokeLinejoin="round" />
      <path d="M10 20.5a2 2 0 0 0 4 0" strokeLinecap="round" />
    </>
  ),
  Devices: (
    <>
      <rect x="7.5" y="3.5" width="9" height="17" rx="1.5" />
      <path d="M11 17.5h2" strokeLinecap="round" />
    </>
  ),
  About: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 11v5.5M12 7.75v.25" strokeLinecap="round" />
    </>
  ),
}

function TabIcon({ tab }: { tab: Tab }) {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden>
      {TAB_ICONS[tab]}
    </svg>
  )
}

function Section({ title, hint, keywords, children }: { title: string; hint?: string; keywords?: string; children: ReactNode }) {
  const query = useContext(SearchQuery)
  const tabMatched = useContext(SectionMatched)
  const only = useContext(OnlyRows)
  // Searching (or in New), an unmatched section keeps only its matching rows, and goes when none is left.
  const all = only ? false : !query || tabMatched || matches(query, title, hint, keywords)
  return (
    <section
      data-shown={query && all ? '' : undefined}
      className={cx('surface p-5', !all && '[&:not(:has([data-shown]))]:hidden')}
    >
      <h2 className="font-display text-base font-semibold text-white">{title}</h2>
      {hint && <p className="mt-1 text-xs leading-relaxed text-base-500">{hint}</p>}
      {/* A rule per row: a two-line hint otherwise runs into the row below it. */}
      <div className={cx('mt-2 divide-y divide-white/5', !all && '[&>:not([data-row])]:hidden')}>
        <SectionMatched.Provider value={all}>{children}</SectionMatched.Provider>
      </div>
    </section>
  )
}

// The row's label names its control for screen readers, as the controls show no text.
const RowLabel = ControlLabel

function Row({
  label,
  hint,
  keywords,
  stacked,
  children,
}: {
  label: string
  hint?: string
  /** Other words someone might search for it by. */
  keywords?: string
  /** The control under the words, for one too wide to sit beside them. */
  stacked?: boolean
  children: ReactNode
}) {
  const query = useContext(SearchQuery)
  const all = useContext(SectionMatched)
  const only = useContext(OnlyRows)
  const shown = only ? only.has(label) : !query || all || matches(query, label, hint, keywords)
  return (
    // Wraps on a narrow screen rather than clipping buttons off the edge.
    <div
      data-row
      data-shown={(query || only) && shown ? '' : undefined}
      hidden={!shown}
      className={cx('flex flex-wrap justify-between gap-x-5 gap-y-2 py-3', stacked ? 'flex-col' : 'items-center')}
    >
      {/* basis-48 is a width beside the control; in a stacked (column) row it became a 12rem height. */}
      <div className={cx('min-w-0', !stacked && 'flex-1 basis-48')}>
        <p className="text-sm text-base-100">
          {label}
          {only && <span className="ml-2 rounded bg-accent-500/20 px-1.5 py-0.5 text-[11px] font-medium text-accent-200">New</span>}
        </p>
        {hint && <p className="mt-0.5 text-xs leading-relaxed text-base-500">{hint}</p>}
      </div>
      {/* One track so the right edge doesn't zigzag (not on a phone, where it leaves the label). */}
      <div className={cx('flex max-w-full', stacked ? '' : 'justify-end sm:min-w-36')}>
        <RowLabel.Provider value={label}>{children}</RowLabel.Provider>
      </div>
    </div>
  )
}

function Switch({ on, onChange, label }: { on: boolean; onChange: (v: boolean) => void; label?: string }) {
  return <UiSwitch on={on} onChange={onChange} aria-label={label} />
}

function useFlag() {
  const prefs = usePrefs()
  const setPref = useSetPref()
  const values = prefs.data?.effective ?? {}
  return {
    loading: prefs.isPending,
    // Unloaded settings would show as all off, and a click would write over the real ones.
    failed: prefs.isError && !prefs.data ? <ErrorState error={prefs.error} retry={() => void prefs.refetch()} /> : null,
    get: (key: string) => values[key] === 'true',
    value: (key: string) => values[key] ?? '',
    set: (key: string, value: string) => setPref.mutate({ key, value }),
  }
}

// How Yoru runs on the machine it's installed on, so set there alone; another device is told where.
function WindowSection({ f }: { f: ReturnType<typeof useFlag> }) {
  const access = useAccess()
  if (access.isPending) return null
  if (!access.data?.host) {
    return (
      <Section title="This computer" hint="How Yoru runs on the computer it's installed on." keywords="window startup awake">
        <p className="py-2 text-sm text-base-400">These are set on the computer Yoru runs on, in its own window.</p>
      </Section>
    )
  }
  return (
    <Section title="This computer" hint="How Yoru runs on the computer it's installed on.">
      <Row label="Open Yoru" hint="From the next start. F11 switches either way." keywords="window fullscreen maximized">
        <Select value={f.value('window.mode') || 'fullscreen'} onChange={(v) => f.set('window.mode', v)}>
          <option value="fullscreen">Fullscreen</option>
          <option value="maximized">Maximized</option>
        </Select>
      </Row>
      <CloseWindowRow f={f} />
      <SignInRow />
      <Row
        label="Stay awake while another device watches"
        hint={
          f.get('power.awake_watching')
            ? "A phone, TV or cast keeps this computer from sleeping while it plays. The screen still turns off, and the lid and power button still sleep it."
            : 'This computer sleeps on its own schedule, even partway through an episode on another device.'
        }
        keywords="sleep awake power standby phone tv cast"
      >
        <Switch on={f.get('power.awake_watching')} onChange={(v) => f.set('power.awake_watching', String(v))} />
      </Row>
      <Row
        label="Stay awake while downloading"
        hint={
          f.get('power.awake_downloading')
            ? 'This computer stays awake while a download is receiving, and may sleep a couple of minutes after the last.'
            : 'Downloads stop while this computer sleeps, and carry on when it wakes.'
        }
        keywords="sleep awake power standby downloads overnight"
      >
        <Switch on={f.get('power.awake_downloading')} onChange={(v) => f.set('power.awake_downloading', String(v))} />
      </Row>
    </Section>
  )
}

// Started at sign-in, Yoru is there for alerts, downloads and other devices without being opened first.
function SignInRow() {
  const qc = useQueryClient()
  const startup = useQuery({
    queryKey: ['app-startup'],
    queryFn: () => api.get<{ on: boolean; tray: boolean }>('/api/app/startup'),
  })
  const set = useMutation({
    meta: { inline: true },
    mutationFn: (on: boolean) => api.post<{ on: boolean; tray: boolean }>('/api/app/startup', { on }),
    onSuccess: (data) => qc.setQueryData(['app-startup'], data),
  })
  const on = startup.data?.on ?? false
  const where = startup.data?.tray === false ? 'in the background' : 'in the tray'
  const hint = startup.isError
    ? (startup.error as Error).message
    : set.isError
      ? (set.error as Error).message
      : on
        ? `Yoru starts ${where} when you sign in, so downloads, alerts and your other devices work without opening it.`
        : 'Yoru starts when you open it.'
  return (
    <Row label={`Start Yoru when I sign in, ${where}`} hint={hint} keywords="startup sign in login boot autostart tray">
      <Switch on={on} onChange={(v) => set.mutate(v)} />
    </Row>
  )
}

// On Windows, where Yoru's tray icon is.
function CloseWindowRow({ f }: { f: ReturnType<typeof useFlag> }) {
  if (!navigator.userAgent.includes('Windows')) return null
  const tray = f.value('window.on_close') === 'tray'
  return (
    <Row
      label="When the window closes"
      hint={
        tray
          ? 'Yoru keeps downloading and serving your other devices; its tray icon opens or exits it.'
          : 'Yoru exits, and downloads and your other devices stop with it.'
      }
      keywords="close exit quit tray background minimize"
    >
      <Select value={tray ? 'tray' : 'exit'} onChange={(v) => f.set('window.on_close', v)}>
        <option value="exit">Exit Yoru</option>
        <option value="tray">Keep running in the tray</option>
      </Select>
    </Row>
  )
}

// Where casting starts is the player's button; here only how the TV shows subtitles. None on an iPhone.
function CastingSection({ f }: { f: ReturnType<typeof useFlag> }) {
  if (castImpossible()) return null
  return (
    <Section
      title="Casting"
      hint="To a Chromecast or Google TV, from the Cast button in the player's bar. If casting can't start, it says what's missing."
      keywords="chromecast google tv cast"
    >
      <Row
        label="Draw subtitles into the picture"
        hint={
          f.get('cast.burn_subtitles')
            ? 'The TV shows them as they look here, signs included; this PC re-encodes the video while casting. The episode must be downloaded.'
            : 'Off, the TV draws plain text it can turn off, without the fonts and signs.'
        }
        keywords="burn burned subtitles signs fonts"
      >
        <Switch on={f.get('cast.burn_subtitles')} onChange={(v) => f.set('cast.burn_subtitles', String(v))} />
      </Row>
    </Section>
  )
}

// Only when chosen and missing: Yoru does not install VLC.
function VLCNote() {
  const f = useFlag()
  const setup = useSetup()
  if (f.value('playback.player') !== 'vlc' || !setup.data || setup.data.vlc) return null
  return (
    <p className="text-xs text-recap">
      VLC was not found. Install it from videolan.org, or if it is installed somewhere unusual set{' '}
      <code>vlc_path</code> in {setup.data.configPath} to its folder, then restart Yoru.
    </p>
  )
}

function PlaybackTab() {
  const f = useFlag()
  if (f.loading) return <Skeleton className="h-64 w-full" />
  if (f.failed) return f.failed

  return (
    <div className="space-y-4">
      <Section title="Automatic" hint="Flipping one in the player changes it here too.">
        <Row label="Auto play" hint="Starts as soon as the page opens." keywords="autoplay">
          <Switch on={f.get('playback.autoplay')} onChange={(v) => f.set('playback.autoplay', String(v))} />
        </Row>
        <Row label="Auto next episode" hint="Counts down at the end, then plays the next." keywords="autonext countdown">
          <Switch on={f.get('playback.autonext')} onChange={(v) => f.set('playback.autonext', String(v))} />
        </Row>
        <Row
          label="Ask if still watching"
          hint={
            f.get('playback.autonext')
              ? 'After this many episodes in a row with no key, tap or mouse, Auto next stops and asks. A TV cast to stops there too.'
              : 'Only with Auto next episode on.'
          }
          keywords="still watching asleep sleep binge pause"
        >
          <Select value={f.value('playback.still_watching') || '3'} onChange={(v) => f.set('playback.still_watching', v)}>
            <option value="0">Never</option>
            <option value="2">After 2 episodes</option>
            <option value="3">After 3 episodes</option>
            <option value="5">After 5 episodes</option>
          </Select>
        </Row>
        <Row
          label="Skip filler and recap episodes"
          hint="Next and download ahead step over them; the list still shows them. Recap scenes are under Skipping."
        >
          <Switch on={f.get('playback.skip_filler')} onChange={(v) => f.set('playback.skip_filler', String(v))} />
        </Row>
        <Row
          label="Counts as watched at"
          hint={
            f.value('sync.progress_at') === 'ed'
              ? 'Ticked and sent to your tracker when the ending song starts. An episode with no known ending, or one that starts before 70%, counts at 90%.'
              : 'How far into an episode it is ticked and sent to your tracker. Lower if you stop at the credits.'
          }
          keywords="watched threshold progress percent credits ending"
        >
          <Select value={f.value('sync.progress_at') || '0.9'} onChange={(v) => f.set('sync.progress_at', v)}>
            <option value="0.8">80%</option>
            <option value="0.85">85%</option>
            <option value="0.9">90%</option>
            <option value="0.95">95%</option>
            <option value="ed">When the ending starts</option>
          </Select>
        </Row>
      </Section>

      <SkippingSection f={f} />

      <Section title="Player">
        <Row label="Default player" hint="mpv and VLC play on this computer only; a phone or TV uses the browser." keywords="mpv vlc external">
          <Select
            value={f.value('playback.player')}
            onChange={(v) => f.set('playback.player', v)}
          >
            <option value="browser">In browser</option>
            <option value="mpv">mpv</option>
            <option value="vlc">VLC</option>
          </Select>
        </Row>
        <VLCNote />
        <Row
          label="Keyboard shortcuts"
          hint="Change the player's keys, or keep them to the player so a screen reader's own keys work elsewhere. Kept on this device."
          keywords="keys hotkeys keyboard accessibility screen reader"
        >
          <Button size="sm" onClick={() => window.dispatchEvent(new Event(SHOW_SHORTCUTS))}>
            Change keys
          </Button>
        </Row>
      </Section>

      <CastingSection f={f} />
    </div>
  )
}

function LanguageTab() {
  const f = useFlag()
  if (f.loading) return <Skeleton className="h-64 w-full" />
  if (f.failed) return f.failed

  return (
    <div className="space-y-4">
      <Section title="Language">
        <Row label="Audio preference" keywords="sub dub dubbed subbed">
          <Select
            value={f.value('audio.prefer')}
            onChange={(v) => f.set('audio.prefer', v)}
          >
            <option value="sub">Subbed</option>
            <option value="dub">Dubbed</option>
            <option value="either">Either</option>
          </Select>
        </Row>
        <SubtitleLanguages f={f} />
        <SubtitlesWithDub f={f} />
        <Row
          label="Subtitle look"
          hint="For dialogue; signs and songs stay as made. Also on a TV, and in mpv for plain-text subtitles."
          keywords="subs captions style colour color outline box yellow readable"
        >
          <div className="flex flex-wrap items-center justify-end gap-3">
            <span
              aria-hidden
              className="grid h-9 w-36 place-items-center rounded bg-[linear-gradient(135deg,#384868,#6e5546)] text-xs font-semibold"
            >
              <span style={previewStyle(asSubLook(f.value('subtitle.look')))}>Where to next?</span>
            </span>
            <Select value={f.value('subtitle.look')} onChange={(v) => f.set('subtitle.look', v)}>
              {SUB_LOOKS.map((l) => (
                <option key={l.value} value={l.value}>
                  {l.label}
                </option>
              ))}
            </Select>
          </div>
        </Row>
        <Row
          label="Raise subtitles"
          hint="Dialogue higher on the screen, clear of text burned into the picture. In mpv, plain-text subtitles only; not on a TV you cast to."
          keywords="subs position height margin higher"
        >
          <Switch on={f.get('subtitle.raise')} onChange={(v) => f.set('subtitle.raise', String(v))} />
        </Row>
        <JimakuKey />
        <Row label="Show titles as" keywords="english romaji names">
          <Select
            value={f.value('display.titles')}
            onChange={(v) => f.set('display.titles', v)}
          >
            <option value="english">English</option>
            <option value="romaji">Romaji</option>
          </Select>
        </Row>
      </Section>

      <Section title="Spoilers">
        <Row
          label="Hide spoilers"
          hint="Unwatched episodes show a blurred picture and no title or synopsis; tap the eye to see one. Shows you finished or are rewatching show everything. A show's episode list can show all of its titles."
          keywords="spoilers blur hide titles synopsis thumbnails"
        >
          <Select value={f.value('display.spoilers') || 'after_next'} onChange={(v) => f.set('display.spoilers', v)}>
            <option value="after_next">After the next episode</option>
            <option value="unwatched">Every unwatched episode</option>
            <option value="off">Off</option>
          </Select>
        </Row>
      </Section>

      <Section title="Home">
        <HomeRowsRow f={f} />
        <Row
          label="Adult titles"
          hint="Off, For you, Schedule and Recent leave out 18+ shows that aren't on your list."
          keywords="adult 18+ nsfw hentai recommendations schedule recent"
        >
          <Switch on={f.get('home.adult_picks')} onChange={(v) => f.set('home.adult_picks', String(v))} />
        </Row>
      </Section>
    </div>
  )
}

// Off takes a row off Home on every device; folding one by its heading on Home is that device's alone.
function HomeRowsRow({ f }: { f: ReturnType<typeof useFlag> }) {
  const hidden = hiddenRows(f.value('home.hidden'))
  const set = (id: string, on: boolean) => {
    if (on) hidden.delete(id)
    else hidden.add(id)
    f.set('home.hidden', JSON.stringify(HOME_ROWS.map((r) => r.id).filter((r) => hidden.has(r))))
  }
  return (
    <Row
      label="Rows on Home"
      hint="Off takes a row off Home. To fold one away for now, press its heading on Home."
      keywords="home rows rails hide collapse for you sequels trending season upcoming"
      stacked
    >
      <div className="grid w-full gap-x-8 sm:grid-cols-2">
        {HOME_ROWS.map((row) => (
          <div key={row.id} className="flex items-center justify-between gap-3 py-1.5">
            <span className="text-sm text-base-300">{row.label}</span>
            <Switch label={row.label} on={!hidden.has(row.id)} onChange={(on) => set(row.id, on)} />
          </div>
        ))}
      </div>
    </Row>
  )
}

function ComputerTab() {
  const f = useFlag()
  if (f.loading) return <Skeleton className="h-64 w-full" />
  if (f.failed) return f.failed
  return <WindowSection f={f} />
}

function SoundTab() {
  const f = useFlag()
  if (f.loading) return <Skeleton className="h-64 w-full" />
  if (f.failed) return f.failed

  return (
    <div className="space-y-4">
      <Section title="Sound" hint="Browser player only; also in the player's sound menu.">
        <Row label="Voice boost" hint="Lifts dialogue over music and effects. Press V in the player." keywords="dialogue speech clear">
          <Select value={f.value('playback.voice') || 'off'} onChange={(v) => f.set('playback.voice', v)}>
            <option value="off">Off</option>
            <option value="medium">Medium (+6 dB)</option>
            <option value="high">High (+12 dB)</option>
          </Select>
        </Row>
        <Row label="Night mode" hint="Evens out loud effects and quiet lines." keywords="compress dynamic range">
          <Switch on={f.get('playback.night')} onChange={(v) => f.set('playback.night', String(v))} />
        </Row>
        <Row label="Headphones" hint="Eases hard-panned sound, as speakers in a room do." keywords="crossfeed">
          <Switch on={f.get('playback.headphones')} onChange={(v) => f.set('playback.headphones', String(v))} />
        </Row>
        <Row label="Mono" hint="Both channels in both ears, for voices mixed to one side.">
          <Switch on={f.get('playback.mono')} onChange={(v) => f.set('playback.mono', String(v))} />
        </Row>
        <Row label="Volume boost" hint="Raises a quiet release, limited so nothing clips." keywords="louder gain">
          <Select value={f.value('playback.boost') || 'off'} onChange={(v) => f.set('playback.boost', v)}>
            <option value="off">Off</option>
            <option value="medium">Medium (twice as loud)</option>
            <option value="high">High (four times)</option>
          </Select>
        </Row>
      </Section>

      <Section title="Picture">
        <Row
          label="Anime4K upscaling"
          hint="Sharpens the picture. Needs mpv or a browser with WebGPU; phones keep it off unless turned on there."
          keywords="upscale sharpen"
        >
          <Switch on={f.get('playback.anime4k')} onChange={(v) => f.set('playback.anime4k', String(v))} />
        </Row>
        <Row
          label="Anime4K mode"
          hint={ANIME4K_MODES.find((m) => m.id === f.value('playback.anime4k_mode'))?.hint}
          keywords="upscale"
        >
          <Select
            value={f.value('playback.anime4k_mode')}
            onChange={(v) => f.set('playback.anime4k_mode', v)}
          >
            {ANIME4K_MODES.map((m) => (
              <option key={m.id} value={m.id}>{m.title}</option>
            ))}
          </Select>
        </Row>
        <Row
          label="Anime4K size"
          hint={`mpv only; each step up roughly doubles GPU cost. ${
            ANIME4K_SIZES.find((s) => s.id === f.value('playback.anime4k_size'))?.hint ?? ''
          }`}
          keywords="upscale gpu"
        >
          <Select
            value={f.value('playback.anime4k_size')}
            onChange={(v) => f.set('playback.anime4k_size', v)}
          >
            {ANIME4K_SIZES.map((s) => (
              <option key={s.id} value={s.id}>{s.id}</option>
            ))}
          </Select>
        </Row>
        <Row
          label="Soften flashing"
          hint="Dims sudden bright flashes, on every device and in mpv. Not a guarantee against photosensitive epilepsy. Episodes are converted while it's on."
          keywords="photosensitive epilepsy seizure flash strobe"
        >
          <Switch on={f.get('playback.soften_flashing')} onChange={(v) => f.set('playback.soften_flashing', String(v))} />
        </Row>
        <Row
          label="Hardware decoding"
          hint="mpv only. Decodes on the graphics card where mpv knows it works; off if the picture glitches."
          keywords="hwdec gpu"
        >
          <Switch on={f.get('playback.mpv_hwdec')} onChange={(v) => f.set('playback.mpv_hwdec', String(v))} />
        </Row>
        <Row label="Smooth banding" hint="mpv only. Evens out stepped gradients in skies and shadows." keywords="deband">
          <Switch on={f.get('playback.mpv_deband')} onChange={(v) => f.set('playback.mpv_deband', String(v))} />
        </Row>
      </Section>
    </div>
  )
}

// The card is this computer's Discord account, so it is set here only.
function DiscordSection({ f }: { f: ReturnType<typeof useFlag> }) {
  const access = useAccess()
  if (!access.data?.host) return null
  const on = f.get('discord.enabled')
  return (
    <Section title="Discord">
      <Row
        label="Show what I'm watching"
        hint="Your profile shows the show, episode and time left. Needs the Discord desktop app here; adult titles never show."
        keywords="rich presence status"
      >
        <Switch on={on} onChange={(v) => f.set('discord.enabled', String(v))} />
      </Row>
      {on && (
        <Row
          label="Include other devices"
          hint="Also what plays on a phone or TV through this Yoru."
        >
          <Switch on={f.get('discord.everywhere')} onChange={(v) => f.set('discord.everywhere', String(v))} />
        </Row>
      )}
    </Section>
  )
}

const LANGUAGES = [
  { code: 'en', name: 'English' },
  { code: 'es', name: 'Spanish' },
  { code: 'pt', name: 'Portuguese' },
  { code: 'fr', name: 'French' },
  { code: 'de', name: 'German' },
  { code: 'it', name: 'Italian' },
  { code: 'ru', name: 'Russian' },
  { code: 'ar', name: 'Arabic' },
  { code: 'zh', name: 'Chinese' },
  { code: 'ko', name: 'Korean' },
  { code: 'id', name: 'Indonesian' },
  { code: 'vi', name: 'Vietnamese' },
  { code: 'th', name: 'Thai' },
]

/** Subtitle languages in order, which picks both the release and the track inside it. */
function SubtitleLanguages({ f }: { f: ReturnType<typeof useFlag> }) {
  const chosen: string[] = (() => {
    try {
      const parsed = JSON.parse(f.value('subtitle.languages') || '["en"]')
      return Array.isArray(parsed) ? parsed : ['en']
    } catch {
      return ['en']
    }
  })()

  const set = (next: string[]) => f.set('subtitle.languages', JSON.stringify(next))
  const toggle = (code: string) =>
    set(chosen.includes(code) ? chosen.filter((c) => c !== code) : [...chosen, code])

  return (
    <Row
      label="Subtitle languages"
      hint={
        chosen.length === 0
          ? 'With none chosen, subtitle language is ignored when ranking releases.'
          : 'In order, best first. A release in none of them ranks lower, never refused.'
      }
      keywords="subs captions"
      stacked
    >
      <div className="flex flex-wrap gap-1.5">
        {LANGUAGES.map((lang) => {
          const at = chosen.indexOf(lang.code)
          return (
            <button
              key={lang.code}
              aria-pressed={at >= 0}
              onClick={() => toggle(lang.code)}
              className={cx(
                'rounded-md px-2.5 py-1.5 text-xs font-medium transition-colors',
                at >= 0
                  ? 'bg-accent-500/20 text-accent-300 ring-1 ring-accent-500/40'
                  : 'bg-base-850 text-base-400 hover:bg-base-800 hover:text-base-200',
              )}
            >
              {at >= 0 && <span className="mr-1 tabular-nums opacity-70">{at + 1}</span>}
              {lang.name}
            </button>
          )
        })}
      </div>
    </Row>
  )
}

/** Japanese subtitles from jimaku.cc with the viewer's key, kept secret: only whether one is set shows. */
function JimakuKey() {
  const qc = useQueryClient()
  const state = useQuery({ queryKey: ['jimaku'], queryFn: () => api.get<{ set: boolean }>('/api/jimaku') })
  const [key, setKey] = useState('')
  const save = useMutation({
    meta: { inline: true },
    mutationFn: (value: string) => api.post<{ set: boolean }>('/api/jimaku/key', { key: value }),
    onSuccess: (res) => {
      qc.setQueryData(['jimaku'], res)
      setKey('')
    },
  })
  const set = state.data?.set
  return (
    <Row
      label="Jimaku (Japanese subtitles)"
      hint={
        save.isError
          ? (save.error as Error).message
          : set
            ? "Set. The player's subtitles menu lists jimaku.cc's files for each episode."
            : 'Paste an API key from your account page on jimaku.cc to list its Japanese subtitles in the player.'
      }
      keywords="japanese subtitles jimaku learning immersion api key"
    >
      <div className="flex flex-wrap justify-end gap-2">
        {set ? (
          <Button size="sm" onClick={() => save.mutate('')} disabled={save.isPending}>
            {save.isPending ? 'Removing…' : 'Remove key'}
          </Button>
        ) : (
          <>
            <TextInput
              type="password"
              aria-label="Jimaku API key"
              placeholder="API key"
              value={key}
              onChange={(e) => setKey(e.target.value)}
              className="w-48"
            />
            <Button size="sm" onClick={() => save.mutate(key)} disabled={!key.trim() || save.isPending}>
              {save.isPending ? 'Checking…' : 'Save'}
            </Button>
          </>
        )}
      </div>
    </Row>
  )
}

/** With a dub the viewer understands, full subtitles repeat it; signs and songs still translate the screen. */
function SubtitlesWithDub({ f }: { f: ReturnType<typeof useFlag> }) {
  return (
    <Row
      label="Subtitles with a dub"
      hint="When the audio is in one of your subtitle languages. Any track can still be picked in the player."
      keywords="dub signs songs forced captions"
    >
      <Select value={f.value('subtitle.with_dub') || 'signs'} onChange={(v) => f.set('subtitle.with_dub', v)}>
        <option value="signs">Signs and songs only</option>
        <option value="off">Off</option>
        <option value="same">Same as with sub</option>
      </Select>
    </Row>
  )
}

interface OrphanResult {
  files: { name: string; bytes: number }[]
  removed: number
  freedBytes: number
  /** Shown but kept: claimed by a download since the list was made. */
  kept?: string[]
}

function StorageTab() {
  const f = useFlag()
  const qc = useQueryClient()
  // Turning on the deletion of kept downloads asks: those files go for good.
  const [confirmPurge, setConfirmPurge] = useState(false)
  const cache = useQuery({ queryKey: ['cache'], queryFn: () => api.get<Record<string, number>>('/api/cache') })
  // The budget is kept every two minutes by itself; this frees the space now, as the Downloads page's Clear does.
  const [confirmClear, setConfirmClear] = useState(false)
  const clear = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<{ removed: number; freedBytes: number }>('/api/downloads/clear?scope=completed'),
    onSuccess: () => {
      setConfirmClear(false)
      void qc.invalidateQueries({ queryKey: ['cache'] })
      refreshDownloads(qc)
    },
  })
  // Looked at first, deleted only once the list has been seen.
  const preview = useMutation({
    meta: { inline: true },
    mutationFn: () => api.get<OrphanResult>('/api/cache/orphans'),
  })
  const orphans = useMutation({
    meta: { inline: true },
    // Exactly the names shown: anything that turned up or got claimed since stays.
    mutationFn: (names: string[]) => api.post<OrphanResult>('/api/cache/orphans', { names }),
    onSuccess: () => {
      preview.reset()
      qc.invalidateQueries({ queryKey: ['cache'] })
    },
  })
  const found = useHeldData(preview)
  const cleared = useHeldData(clear)
  const removedOrphans = useHeldData(orphans)

  if (f.loading) return <Skeleton className="h-48 w-full" />
  if (f.failed) return f.failed

  return (
    <div className="space-y-4">
      <Section
        title="Space"
        hint="Watched episodes stay so a rewatch is instant; the oldest go first. Episodes you download yourself don't count."
        keywords="cache disk"
      >
        <Row label="Budget" hint="How much watched episodes may take." keywords="size space gb cache">
          <Select
            value={f.value('cache.budget_bytes')}
            onChange={(v) => f.set('cache.budget_bytes', v)}
          >
            {[5, 10, 20, 40, 80].map((gb) => (
              <option key={gb} value={String(gb * 1024 ** 3)}>{gb} GB</option>
            ))}
          </Select>
        </Row>
        <Row
          label="Auto-delete watched"
          hint="Once two more are watched keeps the last one, so going back is still free."
          keywords="remove clean up"
        >
          <Select value={f.value('cache.autodelete') || 'off'} onChange={(v) => f.set('cache.autodelete', v)}>
            <option value="off">Off</option>
            <option value="now">Right after watching</option>
            <option value="keep2">Once two more are watched</option>
          </Select>
        </Row>
        {(f.value('cache.autodelete') || 'off') !== 'off' && (
          <Row
            label="Also delete downloaded episodes"
            hint="Episodes you chose to keep are deleted from disk too, once watched. Nothing else ever removes a download."
          >
            {confirmPurge ? (
              <div className="flex flex-wrap items-center justify-end gap-2">
                <ConfirmInline
                  label="Delete them too"
                  note="Kept episodes go for good once watched"
                  onConfirm={() => {
                    f.set('cache.autodelete_downloads', 'true')
                    setConfirmPurge(false)
                  }}
                  onCancel={() => setConfirmPurge(false)}
                />
              </div>
            ) : (
              <Switch
                on={f.get('cache.autodelete_downloads')}
                onChange={(v) => (v ? setConfirmPurge(true) : f.set('cache.autodelete_downloads', 'false'))}
              />
            )}
          </Row>
        )}
        <Row
          label="In use"
          hint={
            clear.isError
              ? (clear.error as Error).message
              : cleared
                ? cleared.removed > 0
                  ? `Cleared ${cleared.removed} download${cleared.removed === 1 ? '' : 's'} · freed ${bytes(cleared.freedBytes)}`
                  : "Nothing to clear: what's cached is playing, still downloading, or kept."
                : cache.data
                  ? `${bytes(Number(cache.data.bytes ?? 0))} cached · ${bytes(Number(cache.data.keptBytes ?? 0))} downloaded`
                  : 'Loading…'
          }
        >
          {confirmClear ? (
            <ConfirmInline
              label={clear.isPending ? 'Clearing…' : 'Clear cache'}
              note="Streamed episodes go; what's playing or downloading, and downloads you keep, stay"
              pending={clear.isPending}
              onConfirm={() => clear.mutate()}
              onCancel={() => setConfirmClear(false)}
            />
          ) : (
            <button onClick={() => setConfirmClear(true)} className={buttonClass()}>
              Clear cache
            </button>
          )}
        </Row>
        <Row
          label="Orphaned files"
          hint={
            removedOrphans
              ? `Removed ${removedOrphans.removed} · freed ${bytes(removedOrphans.freedBytes)}${
                  removedOrphans.kept?.length ? ` · kept ${removedOrphans.kept.length} now in use` : ''
                }`
              : found
                ? found.removed === 0
                  ? 'Nothing orphaned.'
                  : `${found.removed} found · ${bytes(found.freedBytes)}`
                : 'Files the torrent engine no longer knows, left by a crash or a delete outside Yoru.'
          }
          keywords="leftover clean up"
        >
          {found && found.removed > 0 ? (
            <div className="flex gap-2">
              <ConfirmInline
                label={orphans.isPending ? 'Deleting…' : `Delete ${found.removed}`}
                onConfirm={() => orphans.mutate(found.files.map((f) => f.name))}
                onCancel={() => preview.reset()}
                pending={orphans.isPending}
              />
            </div>
          ) : (
            <button
              onClick={() => {
                orphans.reset()
                preview.mutate()
              }}
              disabled={preview.isPending}
              className={buttonClass()}
            >
              {preview.isPending ? 'Looking…' : 'Find'}
            </button>
          )}
        </Row>
        {found && found.removed > 0 && (
          <ul className="max-h-40 overflow-y-auto rounded-md bg-base-950 px-3 py-2 font-mono text-[11px] text-base-400">
            {found.files.map((f) => (
              <li key={f.name} className="flex justify-between gap-4">
                <span className="truncate">{f.name}</span>
                <span className="shrink-0">{bytes(f.bytes)}</span>
              </li>
            ))}
          </ul>
        )}
        {(preview.error ?? orphans.error) && (
          <p className="text-xs text-recap">{((preview.error ?? orphans.error) as Error).message}</p>
        )}
      </Section>

      <WhereSection />
    </div>
  )
}

function DownloadsTab() {
  const f = useFlag()
  if (f.loading) return <Skeleton className="h-64 w-full" />
  if (f.failed) return f.failed

  return (
    <div className="space-y-4">
      <Section
        title="Release sites"
        hint="Where Yoru looks for episodes. Saved at once, no restart."
        keywords="indexer nyaa tokyotosho torrent sites sources search"
      >
        <SitesEditor />
      </Section>
      <Section title="While you watch">
        <Row
          label="Find the next episode ahead of time"
          hint="Looks up its release while you watch, so Next starts at once. Downloads nothing."
          keywords="prefetch prepare search"
        >
          <Switch on={f.get('playback.prepare_next')} onChange={(v) => f.set('playback.prepare_next', String(v))} />
        </Row>
        <Row
          label="Download ahead while watching"
          hint="Downloads the next episodes while one plays, sharing your connection. Leave off on a slow line."
          keywords="prefetch preload next"
        >
          <Select
            value={f.get('cache.prefetch_next') ? (f.value('cache.prefetch_count') === '2' ? '2' : '1') : '0'}
            onChange={(v) => {
              f.set('cache.prefetch_next', String(v !== '0'))
              if (v !== '0') f.set('cache.prefetch_count', v)
            }}
          >
            <option value="0">Off</option>
            <option value="1">Next episode</option>
            <option value="2">Next 2 episodes</option>
          </Select>
        </Row>
        <Row
          label="Download queue while watching"
          hint="Pausing gives the episode on screen the whole connection; one already on disk pauses nothing."
          keywords="pause queued wait"
        >
          <Select value={f.value('downloads.while_watching') || 'pause'} onChange={(v) => f.set('downloads.while_watching', v)}>
            <option value="pause">Pause while the episode is still arriving</option>
            <option value="continue">Never pause</option>
          </Select>
        </Row>
        <Row
          label="Keep whole episode"
          hint="Finishes downloading after you stop, so coming back is instant. Off, it pauses when you leave."
          keywords="finish complete"
        >
          <Switch on={f.get('cache.prefetch_full')} onChange={(v) => f.set('cache.prefetch_full', String(v))} />
        </Row>
      </Section>

      <Section title="Releases" hint="What Yoru picks when it finds an episode.">
        <Row
          label="Preferred quality"
          hint={
            (f.value('quality.prefer') || 'best') === 'best'
              ? 'The best release there is, 1080p first.'
              : 'The best release up to this; a bigger one only when nothing smaller exists.'
          }
          keywords="resolution 1080p 720p 480p quality data saver"
        >
          <Select value={f.value('quality.prefer') || 'best'} onChange={(v) => f.set('quality.prefer', v)}>
            <option value="best">Best available</option>
            <option value="720p">Up to 720p</option>
            <option value="480p">Up to 480p</option>
          </Select>
        </Row>
        <PreferGroups f={f} />
        <AvoidRows f={f} />
        {/* Was fixed at 3 GB, silently passing over remuxes on a line that could take them. */}
        <Row
          label="Largest automatic episode"
          hint="Bigger releases are left for you to choose by hand. Films scale by their runtime."
          keywords="size limit remux 4k gb"
        >
          <Select
            value={f.value('quality.max_auto_bytes') || '3221225472'}
            onChange={(v) => f.set('quality.max_auto_bytes', v)}
          >
            <option value="2147483648">2 GB</option>
            <option value="3221225472">3 GB</option>
            <option value="5368709120">5 GB</option>
            <option value="10737418240">10 GB</option>
            <option value="0">No limit</option>
          </Select>
        </Row>
        <Row label="Allow 10-bit H.264 (Hi10P)" hint="No hardware decodes it; expect high CPU." keywords="hi10p codec">
          <Switch on={f.get('quality.allow_hi10p')} onChange={(v) => f.set('quality.allow_hi10p', String(v))} />
        </Row>
      </Section>

      <Section title="Auto-download" hint="New episodes of the shows you follow, picked by the rules above." keywords="automatic">
        <Row label="Auto-download followed anime" hint="New episodes of shows you follow download by themselves." keywords="automatic new episodes">
          <Switch on={f.get('autodownload.enabled')} onChange={(v) => f.set('autodownload.enabled', String(v))} />
        </Row>
        <Row
          label="Wait for a better release"
          hint={
            (f.value('autodownload.wait_hours') || '0') === '0'
              ? "Auto-download takes a new episode's first good release. A group's fixed v2 still replaces it."
              : `Auto-download holds a new episode up to ${waitLabel(f.value('autodownload.wait_hours'))} for a preferred group or the top resolution, then takes the best there is. If that release turns up within two days, it replaces the episode while it's unwatched.`
          }
          keywords="wait upgrade preferred group better release v2 repack"
        >
          <WaitSelect value={f.value('autodownload.wait_hours') || '0'} onChange={(v) => f.set('autodownload.wait_hours', v)} />
        </Row>
        <AutoDownloadPreview />
      </Section>

      <Section title="Connection">
        <SpeedLimitRow f={f} />
        <Row
          label="Show speeds in"
          hint="Speed tests count megabits, file sizes megabytes. 8 Mbps is about 1 MB/s."
          keywords="mbps mb/s units"
        >
          <Select value={f.value('display.speed') || 'mbps'} onChange={(v) => f.set('display.speed', v)}>
            <option value="mbps">Mbps</option>
            <option value="mbs">MB/s</option>
          </Select>
        </Row>
        <TorrentNetworkRow f={f} />
        <Row
          label="Sharing"
          hint={
            f.value('downloads.upload') === 'never'
              ? 'Nothing is shared. Downloads get slower, as peers send most to those who share back.'
              : 'Pieces go to other viewers only while you download, which keeps them sending to you.'
          }
          keywords="upload seed seeding"
        >
          <Select value={f.value('downloads.upload') || 'while_downloading'} onChange={(v) => f.set('downloads.upload', v)}>
            <option value="while_downloading">While downloading</option>
            <option value="never">Never</option>
          </Select>
        </Row>
        <PeersFirewallRow />
      </Section>
    </div>
  )
}

interface PeersFirewall {
  status: { supported: boolean; firewall?: string; hint?: string; command?: string }
  port: number
  allowed: boolean
  blocked: boolean
}

// Whether torrent peers can connect in. Downloads work without it, from fewer
// peers. Host only: a paired device cannot change this machine's firewall.
function PeersFirewallRow() {
  const qc = useQueryClient()
  const access = useAccess()
  const host = !!access.data?.host
  const fw = useQuery({
    queryKey: ['torrent-firewall'],
    queryFn: () => api.get<PeersFirewall>('/api/torrent/firewall'),
    enabled: host,
  })
  const allow = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<PeersFirewall>('/api/torrent/firewall'),
    onSuccess: (data) => qc.setQueryData(['torrent-firewall'], data),
  })
  if (!host || !fw.data) return null
  const { status, port, allowed, blocked } = fw.data

  // Linux and macOS: Yoru only says what it found.
  if (!status.supported) {
    if (!status.firewall) return null
    return (
      <Row label="Incoming peers" hint={status.hint} keywords="firewall port" stacked>
        {status.command && (
          <code className="block rounded bg-base-900 px-2 py-1.5 text-xs break-all text-accent-300 select-all">
            {status.command}
          </code>
        )}
      </Row>
    )
  }

  const hint = allow.isError
    ? (allow.error as Error).message
    : allowed
      ? `Peers can connect to Yoru on port ${port}.`
      : blocked
        ? 'Windows Firewall is blocking Yoru, probably from an earlier Cancel on its prompt. Downloads find fewer peers.'
        : `Windows Firewall keeps peers from connecting in on port ${port}. Downloads still work, from fewer peers.`
  return (
    <Row label="Incoming peers" hint={hint} keywords="firewall port">
      {allowed ? (
        <span className="text-sm text-accent-400">✓ Allowed</span>
      ) : (
        <button onClick={() => allow.mutate()} disabled={allow.isPending} className={buttonClass('secondary', 'sm')}>
          {allow.isPending ? 'Waiting for Windows…' : 'Allow'}
        </button>
      )}
    </Row>
  )
}

// Bytes a second, as stored: round numbers in the unit Settings shows speeds in.
const SPEED_LIMITS: Record<SpeedUnit, number[]> = {
  mbps: [5, 10, 20, 40, 80].map((m) => (m * 1_000_000) / 8),
  mbs: [1, 2, 3, 5, 10].map((m) => m * 1024 ** 2),
}
// A 1080p episode is about 1.4 GB for 24 minutes: some 8 Mbps, 1 MB/s.
const STREAM_BYTES = 1_000_000

function SpeedLimitRow({ f }: { f: ReturnType<typeof useFlag> }) {
  const unit: SpeedUnit = f.value('display.speed') === 'mbs' ? 'mbs' : 'mbps'
  const limit = Number(f.value('downloads.limit_bytes')) || 0
  // One picked in the other unit stays listed, or the choice would read as nothing.
  const choices = limit && !SPEED_LIMITS[unit].includes(limit) ? [...SPEED_LIMITS[unit], limit].sort((a, b) => a - b) : SPEED_LIMITS[unit]
  return (
    <Row
      label="Download speed limit"
      keywords="bandwidth throttle cap"
      hint={
        limit > 0 && limit < STREAM_BYTES
          ? `Under about ${speedLimit(STREAM_BYTES, unit)} a 1080p episode plays faster than it downloads, so it stops to wait.`
          : 'Leaves room on your connection for everything else. Streaming counts against it too.'
      }
    >
      <Select value={String(limit)} onChange={(v) => f.set('downloads.limit_bytes', v)}>
        <option value="0">No limit</option>
        {choices.map((b) => (
          <option key={b} value={String(b)}>
            {speedLimit(b, unit)}
          </option>
        ))}
      </Select>
    </Row>
  )
}

interface TorrentNetwork {
  adapters: { name: string; addresses: string[]; vpn: boolean }[]
  network: { adapter: string; address?: string; waiting: boolean }
}

// Torrents kept to one adapter, a VPN's. Host only: it is this machine's network, as the firewall is.
function TorrentNetworkRow({ f }: { f: ReturnType<typeof useFlag> }) {
  const access = useAccess()
  const host = !!access.data?.host
  // Polled: a VPN connects and drops on its own.
  const net = useQuery({
    queryKey: ['network'],
    queryFn: () => api.get<TorrentNetwork>('/api/network'),
    enabled: host,
    refetchInterval: 5000,
  })
  if (!host || !net.data) return null
  const chosen = f.value('downloads.adapter')
  const { adapters, network } = net.data
  // A chosen adapter that's down isn't listed, but stays the choice.
  const missing = chosen && !adapters.some((a) => a.name === chosen)

  let hint =
    "Pick your VPN app's adapter to keep torrents to it. Only torrents use it; Yoru itself, AniList and searches don't."
  if (chosen && network.waiting) {
    hint = `Waiting for ${chosen} to connect. Downloads stay paused until it does; nothing goes out over your normal connection.`
  } else if (chosen) {
    hint =
      `Torrents use only ${chosen}${network.address ? ` (${network.address})` : ''} and pause if it disconnects. ` +
      'The VPN must allow torrent traffic, and peers can rarely connect in through one, so expect fewer peers and slower downloads.'
  }
  return (
    <Row label="Network for torrents" hint={hint} keywords="vpn adapter interface">
      <Select value={chosen} onChange={(v) => f.set('downloads.adapter', v)}>
        <option value="">Any network</option>
        {missing && <option value={chosen}>{chosen} (not connected)</option>}
        {adapters.map((a) => (
          <option key={a.name} value={a.name}>
            {a.name} · {a.addresses[0]}
            {a.vpn ? ' · VPN' : ''}
          </option>
        ))}
      </Select>
    </Row>
  )
}

interface PreviewRow {
  animeId: number
  title: string
  episode?: number
  state: 'download' | 'wait' | 'blocked' | 'none' | 'have' | 'queued' | 'taken' | 'current'
  release?: string
  size?: number
  reason?: string
}

function previewWords(row: PreviewRow): string {
  switch (row.state) {
    case 'download':
      return `Would download ${row.release} · ${bytes(row.size ?? 0)}`
    case 'wait':
      return `Would hold it: ${row.reason}. Best so far: ${row.release}`
    case 'blocked':
      return `Left for you to choose: ${row.reason}`
    case 'none':
      return `Nothing to take: ${row.reason}`
    case 'have':
      return 'Already downloaded'
    case 'queued':
      return 'In the download queue'
    case 'taken':
      return 'Found before but not downloaded, and auto-download takes an episode once. Choose a release to get it.'
    default:
      return 'Up to date: nothing new has aired'
  }
}

// What auto-download would take next, show by show, before it runs. Searching each show is all it does.
function AutoDownloadPreview() {
  const qc = useQueryClient()
  const preview = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<{ items: PreviewRow[] }>('/api/releases/preview'),
  })
  const pick = useMutation({
    meta: { inline: true },
    mutationFn: (p: { animeId: number; episode: number; infoHash: string; title: string }) =>
      api.post('/api/download/release', p),
    onSuccess: () => refreshDownloads(qc),
  })
  const [picking, setPicking] = useState<{ animeId: number; episode: number } | null>(null)
  const rows = useHeldData(preview)?.items

  return (
    <>
      <Row
        label="Preview auto-download"
        hint={
          preview.isError
            ? (preview.error as Error).message
            : pick.isError
              ? (pick.error as Error).message
              : 'What it would take next for each show it follows, and why. Nothing is downloaded.'
        }
        keywords="auto download check dry run why"
      >
        <button onClick={() => preview.mutate()} disabled={preview.isPending} className={buttonClass()}>
          {preview.isPending ? 'Searching…' : rows ? 'Preview again' : 'Preview'}
        </button>
      </Row>
      {rows &&
        (rows.length === 0 ? (
          <p className="pb-3 text-sm text-base-400">No show you follow has auto-download on.</p>
        ) : (
          <ul className="divide-y divide-white/[0.06] pb-2">
            {rows.map((row) => (
              <li key={row.animeId} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2">
                <div className="min-w-0 flex-1 basis-64">
                  <p className="truncate text-sm text-base-100">
                    {row.title}
                    {row.episode ? <span className="text-base-400"> · Episode {row.episode}</span> : null}
                  </p>
                  <p className="text-xs text-base-400">{previewWords(row)}</p>
                </div>
                {row.episode && row.state !== 'have' && row.state !== 'current' && (
                  <button
                    onClick={() => setPicking({ animeId: row.animeId, episode: row.episode! })}
                    className={cx(buttonClass(), 'pointer-coarse:min-h-11')}
                  >
                    Open picker
                  </button>
                )}
              </li>
            ))}
          </ul>
        ))}
      {picking && (
        <ReleasePicker
          animeId={picking.animeId}
          episode={picking.episode}
          purpose="download"
          onClose={() => setPicking(null)}
          onPick={(infoHash, title) => {
            pick.mutate({ ...picking, infoHash, title })
            setPicking(null)
          }}
        />
      )}
    </>
  )
}

function PreferGroups({ f }: { f: ReturnType<typeof useFlag> }) {
  return (
    <ListPref
      f={f}
      setting="release.prefer_groups"
      label="Preferred release groups"
      hint="Comma separated, e.g. SubsPlease, Erai-raws. A match ranks the release higher."
      placeholder="SubsPlease, Erai-raws"
      keywords="fansub uploader"
    />
  )
}

// Never taken on their own; the picker still lists them, saying why, so one can be chosen by hand.
function AvoidRows({ f }: { f: ReturnType<typeof useFlag> }) {
  return (
    <>
      <ListPref
        f={f}
        setting="release.avoid_groups"
        label="Avoided release groups"
        hint="Comma separated. Never picked on their own; still in the release list to choose by hand."
        placeholder="Group, Another"
        keywords="block ban fansub uploader exclude"
      />
      <ListPref
        f={f}
        setting="release.exclude_terms"
        label="Excluded words"
        hint="Comma separated, e.g. x265 mini, Hindi Dub. A release with them in its name is never picked on its own."
        placeholder="x265 mini, CamRip"
        keywords="block filter exclude words terms"
      />
      <Row
        label="Avoid low-quality groups"
        hint={
          f.get('release.avoid_lq')
            ? "Groups on TRaSH Guides' low-quality list are never picked on their own."
            : "Groups on TRaSH Guides' low-quality list rank below others of the same resolution."
        }
        keywords="trash guides lq low quality tier"
      >
        <Switch on={f.get('release.avoid_lq')} onChange={(v) => f.set('release.avoid_lq', String(v))} />
      </Row>
    </>
  )
}

function SkippingSection({ f }: { f: ReturnType<typeof useFlag> }) {
  const finder = useQuery({
    queryKey: ['skip-finder'],
    queryFn: () => api.get<{ available: boolean }>('/api/episode/skips/finder'),
    staleTime: 10 * 60_000,
  })
  return (
    <Section title="Skipping" hint="Opening and ending can be set per show on its watch page.">
      <Row label="Skip opening" keywords="intro op">
        <Switch on={f.get('playback.autoskip_op')} onChange={(v) => f.set('playback.autoskip_op', String(v))} />
      </Row>
      <Row label="Skip ending" keywords="outro ed credits">
        <Switch on={f.get('playback.autoskip_ed')} onChange={(v) => f.set('playback.autoskip_ed', String(v))} />
      </Row>
      <Row
        label="Keep the songs on a first and last episode"
        hint="A first episode's opening plays even when skipping, and so does a finale's ending."
        keywords="first episode finale opening ending songs"
      >
        <Switch
          on={f.get('playback.skip_keep_bookends')}
          onChange={(v) => f.set('playback.skip_keep_bookends', String(v))}
        />
      </Row>
      <Row label="Skip recap scenes" hint="The part of an episode that retells the last one." keywords="recap previously">
        <Switch on={f.get('playback.autoskip_recap')} onChange={(v) => f.set('playback.autoskip_recap', String(v))} />
      </Row>
      <Row
        label="Skip previews"
        hint="The next episode's preview after the ending, and the short eyecatch breaks."
        keywords="preview next episode eyecatch"
      >
        <Switch on={f.get('playback.autoskip_preview')} onChange={(v) => f.set('playback.autoskip_preview', String(v))} />
      </Row>
      <ListPref
        f={f}
        setting="playback.skip_chapters"
        label="More chapters to skip"
        hint="Comma separated. A chapter whose name has one of these in it is always skipped."
        placeholder="Sponsor, Logo"
        keywords="chapters skip names custom"
      />
      <Row
        label="Find openings and endings"
        hint={
          finder.data?.available === false
            ? "This computer's ffmpeg can't compare episodes' sound (it lacks chromaprint), so only chapters and AniSkip are used."
            : "When neither the file nor AniSkip knows them, Yoru finds them in the sound two episodes share. The player's skip times menu can mark your own."
        }
        keywords="detect find intro chromaprint"
      >
        <span className={cx('text-xs', finder.data?.available === false ? 'text-amber-300' : 'text-base-400')}>
          {finder.isPending ? '' : finder.data?.available ? 'Available' : 'Unavailable'}
        </span>
      </Row>
      <Row
        label="Share skip times with AniSkip"
        hint="Gives AniSkip the times it lacks (from chapters and your marks) and votes on its own, under an anonymous ID. Only the show, episode and times are sent."
        keywords="aniskip share submit vote contribute"
      >
        <Switch on={f.get('skips.share')} onChange={(v) => f.set('skips.share', String(v))} />
      </Row>
    </Section>
  )
}

// A comma-separated list setting, saved as a JSON array when the field is left.
function ListPref({
  f,
  setting,
  label,
  hint,
  placeholder,
  keywords,
}: {
  f: ReturnType<typeof useFlag>
  setting: string
  label: string
  hint: string
  placeholder: string
  keywords: string
}) {
  const stored: string[] = (() => {
    try {
      const parsed = JSON.parse(f.value(setting) || '[]')
      return Array.isArray(parsed) ? parsed : []
    } catch {
      return []
    }
  })()
  // The field follows the saved list when it changes, set during render rather than an effect after it.
  const saved = stored.join(', ')
  const [text, setText] = useState(saved)
  const [shown, setShown] = useState(saved)
  if (shown !== saved) {
    setShown(saved)
    setText(saved)
  }

  // Its own mutation, so the Saved hint is this field's and not every switch's on the page.
  const setGroups = useSetPref()
  const save = () => {
    const items = text.split(',').map((g) => g.trim()).filter(Boolean)
    if (items.join(',') !== stored.join(',')) setGroups.mutate({ key: setting, value: JSON.stringify(items) })
  }

  return (
    <Row label={label} hint={hint} keywords={keywords}>
      <TextInput
        value={text}
        onChange={(e) => setText(e.target.value)}
        onBlur={save}
        onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
        placeholder={placeholder}
        aria-label={label}
        className="w-56"
      />
      <SaveHint state={setGroups} />
    </Row>
  )
}

function AccountsTab() {
  const f = useFlag()
  const trackers = useTrackers()
  // Signing in or out and the keys are the host's; a paired device only reads the state.
  const access = useAccess()
  const host = access.data?.host !== false

  return (
    <div className="space-y-4">
      {trackers.isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : trackers.isError && !trackers.data ? (
        <ErrorState error={trackers.error} retry={() => void trackers.refetch()} />
      ) : (
        trackers.data?.trackers.map((tracker) => (
          <TrackerCard key={tracker.provider} tracker={tracker} host={host} />
        ))
      )}
      {f.failed ?? (!f.loading && <DiscordSection f={f} />)}
    </div>
  )
}

function TrackerCard({ tracker, host }: { tracker: Tracker; host: boolean }) {
  const qc = useQueryClient()
  const [clientId, setClientId] = useState(tracker.clientId)
  const [secret, setSecret] = useState('')
  // Keys are the host's to add: a paired device got the form open and a Save that always failed.
  const [editing, setEditing] = useState(!tracker.configured && host && !tracker.tokenUrl)
  // Yoru's own AniList client: AniList shows a code to paste back, no keys of one's own. An own client leads.
  const quick = !tracker.configured && host ? tracker.tokenUrl : undefined
  const [pasting, setPasting] = useState(false)
  const [pasted, setPasted] = useState('')
  const connect = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post('/api/auth/token', { token: pasted }),
    onSuccess: () => {
      setPasted('')
      setPasting(false)
      qc.invalidateQueries({ queryKey: ['trackers'] })
    },
  })

  const save = useMutation({
    meta: { inline: true },
    mutationFn: () =>
      api.post('/api/trackers', {
        provider: tracker.provider,
        clientId,
        // An untouched field must not wipe the stored secret.
        clientSecret: secret === '' && tracker.hasSecret ? undefined : secret,
      }),
    onSuccess: () => {
      setSecret('')
      setEditing(false)
      qc.invalidateQueries({ queryKey: ['trackers'] })
    },
  })

  // MAL's API has no favourites; the public profile can be read once.
  const importFavs = useMutation({
    meta: { inline: true },
    mutationFn: () =>
      api.post<{ found: number; matched: number; favourited: number; unmatched: number }>(
        '/api/mal/favourites/import',
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['bookmarks'] }),
  })
  const favs = useHeldData(importFavs)

  const status = tracker.reconnect
    ? `${tracker.name} refused the saved login. Reconnect to resume syncing.`
    : tracker.connected
      ? tracker.user ? `Connected as ${tracker.user}` : 'Connected'
      : tracker.configured
        ? 'Set up, not connected'
        : tracker.tokenUrl
          ? 'Not connected'
          : 'Not set up'

  const loginPath = tracker.provider === 'anilist' ? '/api/auth/login' : '/api/mal/auth/login'
  const logoutPath =
    tracker.provider === 'anilist' ? '/api/auth/logout' : '/api/mal/auth/logout'

  const [confirmingOut, setConfirmingOut] = useState(false)
  const logout = useMutation({
    mutationFn: () => api.post(logoutPath),
    onSuccess: () => {
      setConfirmingOut(false)
      qc.invalidateQueries({ queryKey: ['trackers'] })
    },
  })

  return (
    <Section
      title={tracker.name}
      hint={
        tracker.provider === 'anilist'
          ? 'Progress syncs automatically as you watch.'
          : 'Optional second tracker, kept in step from the same local progress.'
      }
      keywords="tracker sync list account login mal"
    >
      <Row label={status}>
        <div className="flex gap-2">
          {tracker.configured && host && (
            <a
              href={loginPath}
              className={buttonClass('primary')}
            >
              {tracker.connected ? 'Reconnect' : 'Connect'}
            </a>
          )}
          {quick && (
            <a
              href={quick}
              target="_blank"
              rel="noreferrer"
              onClick={() => {
                setPasting(true)
                setEditing(false)
              }}
              className={buttonClass('primary')}
            >
              {tracker.connected ? 'Reconnect' : 'Connect'}
            </a>
          )}
          {host && (
            <button
              onClick={() => setEditing((open) => !open)}
              className={buttonClass()}
            >
              {editing ? 'Cancel' : tracker.configured ? 'Edit keys' : quick ? 'Use my own client' : 'Add keys'}
            </button>
          )}
          {tracker.provider === 'mal' && tracker.connected && (
            <button
              onClick={() => importFavs.mutate()}
              disabled={importFavs.isPending}
              title="MyAnimeList's API cannot sync favourites; this copies them over once"
              className={buttonClass()}
            >
              {importFavs.isPending ? 'Importing…' : 'Import favourites'}
            </button>
          )}

          {tracker.connected &&
            host &&
            (confirmingOut ? (
              <ConfirmInline
                label="Disconnect"
                onConfirm={() => logout.mutate()}
                onCancel={() => setConfirmingOut(false)}
                pending={logout.isPending}
              />
            ) : (
              <button
                onClick={() => setConfirmingOut(true)}
                className={buttonClass()}
              >
                Disconnect
              </button>
            ))}
        </div>
      </Row>

      {favs && (
        <p className="px-3 pb-2 text-xs text-base-400">
          {favs.favourited} favourited of {favs.found} on MyAnimeList
          {favs.unmatched > 0 && `, ${favs.unmatched} not in the catalogue`}.
        </p>
      )}
      {importFavs.isError && (
        <p className="px-3 pb-2 text-xs text-recap">{(importFavs.error as Error).message}</p>
      )}

      {/* Neither service revokes tokens, so this says plainly what disconnecting does. */}
      {confirmingOut && (
        <p className="px-3 pb-2 text-xs text-base-500">
          Forgets the account on this machine. Watch history stays. The
          authorisation remains on your {tracker.name} account until you remove
          it there.
        </p>
      )}

      {tracker.connected && tracker.provider === 'anilist' && <ImportRow />}

      {pasting && quick && (
        <div className="space-y-3 rounded-lg border border-base-850 bg-base-950/40 p-3">
          <p className="text-xs text-base-400">
            Approve Yoru on the AniList page that opened, then paste the code it shows here.
          </p>
          <Field label="Code from AniList" value={pasted} onChange={setPasted} secret />
          <div className="flex flex-wrap items-center gap-2">
            <button
              onClick={() => connect.mutate()}
              disabled={connect.isPending || pasted.trim() === ''}
              className={buttonClass('primary')}
            >
              {connect.isPending ? 'Connecting…' : 'Connect'}
            </button>
            <button onClick={() => setPasting(false)} className={buttonClass()}>
              Cancel
            </button>
            {connect.isError && <span className="text-xs text-red-400">{(connect.error as Error).message}</span>}
          </div>
        </div>
      )}

      {editing && (
        <div className="space-y-3 rounded-lg border border-base-850 bg-base-950/40 p-3">
          <p className="text-xs text-base-400">
            Create an API client at{' '}
            <a
              href={tracker.register}
              target="_blank"
              rel="noreferrer"
              className="text-accent-400 hover:underline"
            >
              {tracker.register.replace('https://', '')}
            </a>
            , then paste its keys here. Give it this redirect URL:
          </p>
          <CopyField value={tracker.redirect} />

          <Field label="Client ID" value={clientId} onChange={setClientId} placeholder="12345" />
          <Field
            label={tracker.secretRequired ? 'Client secret' : 'Client secret (optional)'}
            value={secret}
            onChange={setSecret}
            secret
            placeholder={tracker.hasSecret ? 'Saved. Leave blank to keep' : ''}
          />

          <div className="flex items-center gap-2">
            <button
              onClick={() => save.mutate()}
              disabled={save.isPending || clientId.trim() === ''}
              className={buttonClass('primary')}
            >
              {save.isPending ? 'Saving…' : 'Save'}
            </button>
            {save.isError && <span className="text-xs text-red-400">Could not save</span>}
          </div>
        </div>
      )}
    </Section>
  )
}

function Field({
  label,
  value,
  onChange,
  placeholder,
  secret,
}: {
  label: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  secret?: boolean
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-xs text-base-400">{label}</span>
      <TextInput
        type={secret ? 'password' : 'text'}
        value={value}
        placeholder={placeholder}
        spellCheck={false}
        autoComplete="off"
        onChange={(e) => onChange(e.target.value)}
        className="w-full font-mono"
      />
    </label>
  )
}

/** Copy, then what happened: plain-http LAN pages have no clipboard API, so copyText falls back or says no. */
function useCopy() {
  const [result, setResult] = useState<'Copied' | "Couldn't copy" | null>(null)
  const copy = async (text: string) => {
    setResult((await copyText(text)) ? 'Copied' : "Couldn't copy")
    window.setTimeout(() => setResult(null), 1500)
  }
  return { label: result ?? 'Copy', copy }
}

function CopyField({ value }: { value: string }) {
  const { label, copy } = useCopy()

  return (
    <div className="flex items-center gap-2">
      <code className="min-w-0 flex-1 truncate rounded-md border border-base-800 bg-base-900 px-2.5 py-1.5 text-xs text-base-200">
        {value}
      </code>
      <button
        onClick={() => void copy(value)}
        className="shrink-0 rounded-md bg-base-800 px-2.5 py-1.5 text-xs text-base-100 hover:bg-base-700"
      >
        {label}
      </button>
    </div>
  )
}

function ImportRow() {
  const qc = useQueryClient()
  const [mode, setMode] = useState<'merge' | 'replace'>('merge')
  const [confirming, setConfirming] = useState(false)

  const sync = useMutation({
    mutationFn: () => api.post(`/api/sync?mode=${mode}`),
    onSuccess: () => {
      setConfirming(false)
      refreshListState(qc)
    },
  })

  const result = useHeldData(sync) as { entries?: number; removed?: number } | undefined

  return (
    <div className="space-y-2 rounded-lg border border-base-850 bg-base-950/40 p-3">
      <p className="text-xs text-base-400">Bring your AniList list into Yoru.</p>

      <div className="flex gap-1 rounded-md bg-base-900 p-1">
        {(['merge', 'replace'] as const).map((option) => (
          <button
            key={option}
            onClick={() => {
              setMode(option)
              setConfirming(false)
            }}
            className={cx(
              'flex-1 rounded px-2 py-1 text-xs font-medium capitalize transition-colors',
              mode === option ? 'bg-base-750 text-white' : 'text-base-400 hover:text-base-200',
            )}
          >
            {option}
          </button>
        ))}
      </div>

      <p className="text-xs text-base-500">
        {mode === 'merge'
          ? 'Keeps anything AniList does not have and protects changes you have not pushed yet.'
          : 'Makes your list exactly what AniList holds. Entries only in Yoru are deleted and unpushed changes are overwritten.'}
      </p>

      {mode === 'replace' && confirming ? (
        <div className="flex gap-2">
          <ConfirmInline
            label={sync.isPending ? 'Replacing…' : 'Yes, replace'}
            onConfirm={() => sync.mutate()}
            onCancel={() => setConfirming(false)}
            pending={sync.isPending}
          />
        </div>
      ) : (
        <button
          onClick={() => (mode === 'replace' ? setConfirming(true) : sync.mutate())}
          disabled={sync.isPending}
          className={buttonClass()}
        >
          {sync.isPending ? 'Importing…' : mode === 'merge' ? 'Merge list' : 'Replace list'}
        </button>
      )}

      {result && (
        <p className="text-xs text-base-400">
          Imported {result.entries ?? 0} entries
          {result.removed ? `, removed ${result.removed}` : ''}.
        </p>
      )}
    </div>
  )
}

function LibraryTab() {
  const local = useQuery({
    queryKey: ['local'],
    queryFn: () => api.get<{ stats: Record<string, number>; roots: string[]; scanning: boolean }>('/api/local'),
    refetchInterval: (q) => (q.state.data?.scanning ? 2000 : false),
  })
  const [path, setPath] = useState('')
  const qc = useQueryClient()

  const save = useMutation({
    meta: { inline: true },
    mutationFn: (paths: string[]) => api.post('/api/local/paths', { paths }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['local'] }),
  })
  const scan = useMutation({
    mutationFn: () => api.post('/api/local/scan'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['local'] }),
  })
  // A finished scan changes which episodes show as on disk.
  const scanning = !!local.data?.scanning
  const wasScanning = useRef(scanning)
  useEffect(() => {
    if (wasScanning.current && !scanning) {
      for (const key of ['episodes', 'anime']) void qc.invalidateQueries({ queryKey: [key] })
    }
    wasScanning.current = scanning
  }, [scanning, qc])

  const roots = local.data?.roots ?? []
  // Which paths the server refused, and why, said beside the list instead of one vague line.
  const problems =
    save.error instanceof ApiError
      ? ((save.error.body as { paths?: Record<string, string> } | undefined)?.paths ?? {})
      : {}

  return (
    <div className="space-y-4">
    <Section title="Local files" hint="Anime already on disk plays instantly, with no torrent involved." keywords="folders library scan disk">
      <ul className="space-y-1">
        {roots.map((root) => (
          <li key={root} className="flex items-center justify-between gap-2 rounded-md bg-base-900 px-3 py-1.5">
            <span className="truncate text-sm text-base-200">{root}</span>
            <button
              onClick={() => save.mutate(roots.filter((r) => r !== root))}
              // One change at a time: a second click would start from the list before the first.
              disabled={save.isPending}
              className="tap relative text-xs text-base-500 hover:text-recap disabled:opacity-50"
            >
              Remove
            </button>
          </li>
        ))}
      </ul>

      <div className="flex gap-2">
        <TextInput
          value={path}
          onChange={(e) => setPath(e.target.value)}
          placeholder="D:\Anime"
          aria-label="Folder to add"
          className="min-w-0 flex-1"
        />
        <button
          onClick={() => {
            // Cleared only once saved, so a refused path can be corrected.
            if (path.trim()) save.mutate([...roots, path.trim()], { onSuccess: () => setPath('') })
          }}
          disabled={save.isPending}
          className={buttonClass()}
        >
          Add
        </button>
      </div>

      {save.isError &&
        (Object.keys(problems).length > 0 ? (
          <ul className="space-y-0.5 text-xs text-recap">
            {Object.entries(problems).map(([p, why]) => (
              <li key={p}>
                {p}: {why}
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-xs text-recap">{(save.error as Error).message}</p>
        ))}

      <Row
        label={
          local.data
            ? `${local.data.stats.files} files · ${local.data.stats.matched} matched · ${local.data.stats.unmatched} unmatched`
            : 'No scan yet'
        }
      >
        <button
          onClick={() => scan.mutate()}
          disabled={local.data?.scanning || roots.length === 0}
          className={buttonClass()}
        >
          {local.data?.scanning && <Spinner className="size-3.5" />}
          {local.data?.scanning ? 'Scanning…' : 'Scan now'}
        </button>
      </Row>
    </Section>

    <BackupSection />
    </div>
  )
}

function BackupSection() {
  const qc = useQueryClient()
  const [message, setMessage] = useState('')

  // A plain link so the browser saves the file, the token in the URL as a link has no header.
  const exportURL = (format: string) => withToken(`/api/library/export?format=${format}`)

  const load = useMutation({
    meta: { inline: true },
    mutationFn: (file: File) =>
      api.upload<{ entries: number; favourites: number; skipped: number }>(
        '/api/library/import',
        file,
      ),
    onSuccess: (rep) => {
      setMessage(
        `Imported ${rep.entries} ${rep.entries === 1 ? 'entry' : 'entries'}` +
          (rep.favourites ? `, ${rep.favourites} favourited` : '') +
          (rep.skipped ? `, ${rep.skipped} skipped` : '') +
          '. Connected trackers get them on the next sync.',
      )
      for (const key of ['library', 'continue', 'bookmarks', 'anime']) {
        void qc.invalidateQueries({ queryKey: [key] })
      }
    },
    onError: (err) => setMessage((err as Error).message),
  })

  return (
    <Section
      title="Backup"
      hint="Everything watched, rated and favourited, in one file. Importing merges: progress never moves backwards."
      keywords="export import myanimelist xml json"
    >
      <Row label="Export" hint="JSON to import again, text to read">
        <div className="flex gap-2">
          <a
            href={exportURL('json')}
            download
            className={buttonClass()}
          >
            JSON
          </a>
          <a
            href={exportURL('txt')}
            download
            className={buttonClass()}
          >
            Text
          </a>
        </div>
      </Row>

      <Row label="Import" hint="A file exported here, or a MyAnimeList export (.xml or .xml.gz)">
        <label className={cx(buttonClass(), 'cursor-pointer')}>
          {load.isPending ? 'Importing…' : 'Choose file'}
          <input
            type="file"
            accept=".json,.xml,.gz,application/json,text/xml,application/gzip"
            className="hidden"
            disabled={load.isPending}
            onChange={(e) => {
              const file = e.target.files?.[0]
              e.target.value = ''
              if (!file) return
              setMessage('')
              load.mutate(file)
            }}
          />
        </label>
      </Row>

      {message && (
        <p className={cx('text-xs', load.isError ? 'text-recap' : 'text-base-400')}>{message}</p>
      )}
    </Section>
  )
}

// Desktop alerts are a browser permission plus a switch, belonging to this browser.
const ALERT_HINT: Record<PushState, string> = {
  on: 'A new episode shows on this device, even with Yoru closed.',
  off: 'A new episode shows on this device, even with Yoru closed.',
  denied: 'Blocked by the browser: allow notifications for this site, then turn this on.',
  insecure:
    'Shows while Yoru is open. With Yoru closed, alerts need its HTTPS address (Settings, Devices, Install as an app).',
  install: 'On an iPhone, add Yoru to the Home Screen first (Share, Add to Home Screen), then turn this on there.',
  unsupported: 'Shows while Yoru is open; this browser cannot get alerts with Yoru closed.',
}

// One switch for both: alerts while a Yoru tab is open, and, where the browser can, pushed with Yoru closed.
function AlertsRow() {
  const [wanted, setWanted] = useState(desktopNotifyWanted())
  const state = useQuery({ queryKey: ['push-state'], queryFn: pushState, staleTime: Infinity })
  const qc = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const test = useMutation({ meta: { inline: true }, mutationFn: testPush })

  const toggle = async (next: boolean) => {
    setBusy(true)
    setError('')
    try {
      if (next && typeof Notification !== 'undefined' && Notification.permission !== 'granted') {
        if ((await Notification.requestPermission()) !== 'granted') return
      }
      // Alerts in an open tab first: a push service that refuses must not take those away too.
      try {
        localStorage.setItem(DESKTOP_NOTIFY_KEY, next ? '1' : '0')
      } catch {
        // Storage unavailable; the switch just will not stick.
      }
      setWanted(next)
      const can = state.data === 'on' || state.data === 'off'
      if (next && can) await enablePush()
      if (!next && can) await disablePush()
    } catch (e) {
      setError(next ? `Alerts show while Yoru is open, not with it closed: ${(e as Error).message}` : (e as Error).message)
    } finally {
      setBusy(false)
      void qc.invalidateQueries({ queryKey: ['push-state'] })
    }
  }

  if (!state.data) return null
  const pushed = state.data === 'on'
  const on = wanted && state.data !== 'denied' && state.data !== 'install'
  return (
    <Row label="Alerts on this device" hint={ALERT_HINT[state.data]} keywords="desktop push phone notifications closed">
      <div className="flex flex-wrap items-center justify-end gap-2">
        {on && pushed && (
          <Button size="sm" onClick={() => test.mutate()} disabled={test.isPending}>
            {test.isPending ? 'Sending…' : test.isSuccess ? 'Sent' : 'Send a test'}
          </Button>
        )}
        <Switch label="Alerts on this device" on={on} onChange={(v) => void toggle(v)} />
        {(error || test.isError) && (
          <p className="basis-full text-right text-xs text-recap">{error || (test.error as Error).message}</p>
        )}
        {busy && <span className="sr-only">Working…</span>}
      </div>
    </Row>
  )
}

function NotificationsTab() {
  const f = useFlag()
  if (f.loading) return <Skeleton className="h-40 w-full" />
  if (f.failed) return f.failed

  return (
    <Section title="Release notifications" hint="Checks for new episodes of anime you follow." keywords="alerts new episodes">
      <Row label="Enabled">
        <Switch on={f.get('notify.enabled')} onChange={(v) => f.set('notify.enabled', String(v))} />
      </Row>
      <AlertsRow />
      <Row label="Notify for">
        <Select
          value={f.value('notify.releases')}
          onChange={(v) => f.set('notify.releases', v)}
        >
          <option value="sub">Subbed</option>
          <option value="dub">Dubbed</option>
          <option value="both">Both</option>
        </Select>
      </Row>
    </Section>
  )
}

function DevicesTab() {
  return (
    <div className="space-y-4">
      <DevicesAccess />
      <ThisDeviceStreaming />
    </div>
  )
}

function DevicesAccess() {
  const qc = useQueryClient()
  const access = useAccess()
  const proxy = access.data?.host ? access.data.proxy : undefined

  // Moves the listener there and then, so the phone can be tried immediately.
  const setNetwork = useMutation({
    meta: { inline: true },
    mutationFn: (lan: boolean) => api.post('/api/access/network', { lan }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['access'] }),
  })

  return (
    <Section
      title="Watch on your phone or TV"
      hint="Opening this listens on every network this machine is on. The link carries a token; anything without it is refused."
      keywords="devices pair qr access remote"
    >
      {access.data?.canSwitch && (
        <Row
          label="Allow other devices"
          hint={
            setNetwork.isError
              ? (setNetwork.error as Error).message
              : `Listening on ${access.data.listening}`
          }
        >
          <Switch
            on={!!access.data.reachable}
            onChange={(v) => setNetwork.mutate(v)}
          />
        </Row>
      )}

      {access.isPending ? (
        <Skeleton className="h-40 w-full" />
      ) : access.isError ? (
        <p className="text-sm text-recap">{(access.error as Error).message}</p>
      ) : !access.data?.reachable ? (
        <p className="text-sm text-base-400">
          {proxy ? 'Only this machine, and the proxy on it, can reach Yoru.' : 'Only this machine can reach Yoru right now.'}
        </p>
      ) : access.data.urls.length === 0 ? (
        <p className="text-sm text-base-300">
          This machine has no home-network address right now (not on Wi-Fi or Ethernet, or only on a
          VPN), so there is nothing a phone could open yet.
        </p>
      ) : (
        <div className="flex flex-wrap items-center gap-5">
          <img
            // The link in the src: a new token or HTTPS coming up changes it, and the image with it.
            src={`/api/access/qr.svg?url=${encodeURIComponent(access.data.urls[0])}`}
            alt="Pairing QR code"
            className="size-40 rounded-lg bg-white p-2"
          />
          <div className="min-w-0 flex-1">
            <p className="mb-1 text-xs text-base-500">Scan, or open this on the device:</p>
            {access.data.urls.map((url) => (
              <div key={url} className="flex items-center gap-2">
                <code className="min-w-0 truncate text-xs text-accent-400">{url}</code>
                <CopyButton text={url} />
              </div>
            ))}
          </div>
        </div>
      )}
      {access.data?.reachable && access.data.urls.length > 0 && <PairDevice url={access.data.urls[0]} />}
      {/* Whatever device this is: the phone that installs is rarely the computer Yoru runs on. */}
      <InstallHere />
      {proxy && <ProxyRow proxy={proxy} token={access.data?.token ?? ''} />}
      {access.data?.reachable && access.data.host && <FirewallPanel />}
      {(access.data?.reachable || proxy) && access.data?.host && <DeviceApproval />}
      {access.data?.reachable && access.data.host && <InstallAsApp />}
      {(access.data?.reachable || proxy) && access.data?.host && <RevokeDevices />}
    </Section>
  )
}

// Kept on this device, as phones and laptops want different things away from home.
function ThisDeviceStreaming() {
  const net = useNetwork()
  // Only some browsers say they're on mobile data; elsewhere the rule follows being away from home.
  const knows = net.cellular !== null
  return (
    <Section title="This device" hint="Streaming here, away from the computer Yoru runs on." keywords="mobile data cellular quality wifi saver">
      <Row
        label="On mobile data"
        hint={`Ask first shows the episode's size before it plays. ${
          knows ? '' : "This device doesn't say when it's on mobile data, so this applies whenever it's away from home."
        }`}
      >
        <Select value={net.onMobile} onChange={(v) => setOnMobile(v as OnMobile)}>
          {ON_MOBILE.map((o) => (
            <option key={o.value} value={o.value}>{o.label}</option>
          ))}
        </Select>
      </Row>
      <Row
        label="Streaming quality"
        hint="Auto changes nothing at home; away, it lowers a connection that can't keep up. Downloads keep their quality."
      >
        <Select value={net.quality} onChange={(v) => setDeviceQuality(v as Quality)}>
          {QUALITIES.map((q) => (
            <option key={q.value} value={q.value}>{q.label}</option>
          ))}
        </Select>
      </Row>
    </Section>
  )
}

// Devices that come through a proxy on this machine pair with the link like any other.
function ProxyRow({ proxy, token }: { proxy: NonNullable<AccessState['proxy']>; token: string }) {
  const link = `${proxy.site}/?token=${token}`
  return (
    <Row
      label="Behind a proxy"
      hint={`Devices also come through a proxy on this computer, last ${proxy.client ? `from ${proxy.client} ` : ''}${relativeTime(proxy.seen)}. Each opens ${proxy.site ? 'this link' : `the proxy's address with ?token=${token} added`} once to pair.`}
      stacked={!!proxy.site}
    >
      {proxy.site && (
        <div className="flex min-w-0 items-center gap-2">
          <code className="min-w-0 truncate text-xs text-accent-400">{link}</code>
          <CopyButton text={link} />
        </div>
      )}
    </Row>
  )
}

const HTTPS_INPUT = cx(inputClass, 'min-w-0 flex-1 basis-48')

interface HTTPSStatus {
  domain: string
  state: 'off' | 'working' | 'ready' | 'error'
  problem?: string
  expires?: number
  /** This network's DNS answers the name with this PC's address; absent until asked. */
  atHome?: boolean
}

// The device this page is on: one tap where the browser offers it, else what to do there.
function InstallHere() {
  const state = useInstall()
  const [said, setSaid] = useState('')
  return (
    <Row label="Install on this device" hint={said || (state === 'ready' ? 'Opens in its own window, with its own icon.' : INSTALL_HELP[state])}>
      {state === 'ready' && (
        <Button size="sm" onClick={() => void install().then((yes) => setSaid(yes ? 'Installed.' : 'Not installed; ask again any time.'))}>
          Install Yoru
        </Button>
      )}
    </Row>
  )
}

// A phone only installs Yoru as an app over HTTPS: a free DuckDNS name and a certificate Yoru fetches for it.
function InstallAsApp() {
  const qc = useQueryClient()
  const https = useQuery({
    queryKey: ['access-https'],
    queryFn: () => api.get<HTTPSStatus>('/api/access/https'),
    staleTime: 0,
    // An error is retried in the background, so it can still turn into ready.
    refetchInterval: (q) => (q.state.data?.state === 'working' ? 3000 : q.state.data?.state === 'error' ? 10_000 : false),
  })
  const [editing, setEditing] = useState(false)
  const [turningOff, setTurningOff] = useState(false)
  const [domain, setDomain] = useState('')
  const [token, setToken] = useState('')
  const save = useMutation({
    meta: { inline: true },
    mutationFn: (body: { domain: string; token: string }) => api.post<HTTPSStatus>('/api/access/https', body),
    onSuccess: (status) => {
      qc.setQueryData(['access-https'], status)
      setEditing(false)
      setTurningOff(false)
      setToken('')
    },
    // The pairing links switch to https once the certificate is in.
    onSettled: () => qc.invalidateQueries({ queryKey: ['access'] }),
  })
  const check = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<HTTPSStatus>('/api/access/https/check'),
    onSuccess: (status) => qc.setQueryData(['access-https'], status),
    // The QR code goes back to the HTTPS link when the name answers.
    onSettled: () => qc.invalidateQueries({ queryKey: ['access'] }),
  })
  const state = https.data?.state ?? 'off'
  const blocked = state === 'ready' && https.data?.atHome === false
  useEffect(() => {
    if (state === 'ready') void qc.invalidateQueries({ queryKey: ['access'] })
  }, [state, qc])

  const hint = save.isError
    ? (save.error as Error).message
    : check.isError
      ? (check.error as Error).message
      : blocked
        ? `Your router won't answer ${https.data?.domain} with this PC's address (DNS rebind protection), so phones on this network can't open it. Allow the name in the router's rebind exceptions, then check again. Until then the QR code uses the plain link.`
        : state === 'ready'
          ? `Ready at https://${https.data?.domain}. Open it on the phone and choose Install app.${
              https.data?.expires ? ` Renews itself; current certificate ends ${relativeTime(https.data.expires)}.` : ''
            }`
          : state === 'working'
            ? `Getting a certificate for ${https.data?.domain}. This takes a minute or two.`
            : state === 'error'
              ? `Could not get a certificate yet: ${https.data?.problem}. Yoru keeps trying.`
              : 'Phones install Yoru as an app only over HTTPS. Make a free name at duckdns.org and paste it and its token here.'

  return (
    <>
      <Row label="Install as an app on a phone" hint={hint}>
        {state === 'off' || editing ? null : turningOff ? (
          // It forgets the name and token, and every phone that installed the app stops: asked first.
          <div className="flex flex-wrap items-center justify-end gap-2">
            <ConfirmInline
              label={save.isPending ? 'Turning off…' : 'Turn off HTTPS'}
              note="Phones that installed Yoru as an app stop working until it is set up again"
              pending={save.isPending}
              onConfirm={() => save.mutate({ domain: '', token: '' })}
              onCancel={() => setTurningOff(false)}
            />
          </div>
        ) : (
          <div className="flex flex-wrap justify-end gap-2">
            {blocked && (
              <button onClick={() => check.mutate()} disabled={check.isPending} className={buttonClass('primary')}>
                {check.isPending ? 'Checking…' : 'Check again'}
              </button>
            )}
            <button
              onClick={() => {
                setDomain(https.data?.domain ?? '')
                setEditing(true)
              }}
              className={buttonClass()}
            >
              Change
            </button>
            <button onClick={() => setTurningOff(true)} className={buttonClass()}>
              Turn off
            </button>
          </div>
        )}
      </Row>
      {(state === 'off' || editing) && (
        <form
          className="flex flex-wrap items-center gap-2 py-2.5"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate({ domain, token })
          }}
        >
          <input
            value={domain}
            onChange={(e) => setDomain(e.target.value)}
            placeholder="myyoru.duckdns.org"
            aria-label="DuckDNS name"
            autoComplete="off"
            className={HTTPS_INPUT}
          />
          <input
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder={editing ? 'Token (blank keeps the saved one)' : 'Token from duckdns.org'}
            aria-label="DuckDNS token"
            type="password"
            autoComplete="off"
            className={HTTPS_INPUT}
          />
          <button type="submit" disabled={save.isPending || !domain.trim()} className={buttonClass('primary')}>
            Save
          </button>
          {editing && (
            <button type="button" onClick={() => setEditing(false)} className={buttonClass()}>
              Cancel
            </button>
          )}
        </form>
      )}
    </>
  )
}

// A TV or console opening Yoru's address without the link's token shows a code; typed here, it lets it in.
function PairDevice({ url }: { url: string }) {
  const [code, setCode] = useState('')
  const pair = useMutation({
    meta: { inline: true },
    mutationFn: (typed: string) => api.post<{ paired: string }>('/api/pair', { code: typed }),
    onSuccess: () => setCode(''),
  })
  // The address alone, which a remote can type: the token stays in the link.
  const address = new URL(url).origin
  const complete = code.replace(/[\s-]/g, '').length === 6
  return (
    <Row
      label="Pair a device"
      hint={
        pair.isError
          ? (pair.error as Error).message
          : pair.data
            ? `Paired ${pair.data.paired}.`
            : `On a TV or console, open ${address}: it shows a code to type here.`
      }
      keywords="pair code tv console remote link quick connect"
    >
      <form
        className="flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          pair.mutate(code)
        }}
      >
        <input
          value={code}
          onChange={(e) => setCode(e.target.value.toUpperCase())}
          placeholder="K7Q-2MX"
          aria-label="The code the device shows"
          maxLength={8}
          autoComplete="off"
          autoCapitalize="characters"
          spellCheck={false}
          className={cx(inputClass, 'w-28 font-mono tracking-widest uppercase')}
        />
        <button type="submit" disabled={!complete || pair.isPending} className={buttonClass('primary')}>
          {pair.isPending ? 'Pairing…' : 'Pair'}
        </button>
      </form>
    </Row>
  )
}

const APPROVAL_MODES: ReadonlyArray<{ value: ApprovalMode; label: string }> = [
  { value: 'off', label: 'Off' },
  { value: 'once', label: 'Once' },
  { value: 'always', label: 'Every time' },
]

// The link alone, or the link plus the host's say per device.
function DeviceApproval() {
  const qc = useQueryClient()
  const devices = useAccessDevices(true)
  const decide = useDecideDevice()
  const setMode = useMutation({
    meta: { inline: true },
    mutationFn: (mode: ApprovalMode) => api.post('/api/access/approval', { mode }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['access-devices'] }),
  })
  const mode = devices.data?.mode ?? 'off'
  const hint =
    mode === 'off'
      ? 'Any device with the link gets in.'
      : mode === 'once'
        ? 'A new device waits until you accept it here, then is remembered.'
        : `A device waits until you accept it, and asks again after ${devices.data?.idleMinutes ?? 30} minutes away or a restart.`

  return (
    <>
      <Row label="Ask before letting a device in" hint={setMode.isError ? (setMode.error as Error).message : hint}>
        <Segmented options={APPROVAL_MODES} value={mode} onChange={(m) => setMode.mutate(m)} size="sm" />
      </Row>
      {(devices.data?.devices ?? []).map((d) => (
        <Row
          key={d.id}
          label={d.name}
          hint={
            // The answer is inline-only, so a failed one says so on its own row.
            decide.isError && decide.variables?.id === d.id
              ? (decide.error as Error).message
              : `${d.addr || 'through a proxy'} · ${DEVICE_STATUS[d.status]} · asked ${relativeTime(d.requestedAt)}`
          }
        >
          <div className="flex flex-wrap justify-end gap-2">
            {d.status !== 'approved' && d.status !== 'lapsed' && (
              <button
                onClick={() => decide.mutate({ id: d.id, status: 'approved' })}
                disabled={decide.isPending}
                className={buttonClass(d.status === 'pending' ? 'primary' : undefined)}
              >
                Accept
              </button>
            )}
            {d.status === 'pending' && (
              <button
                onClick={() => decide.mutate({ id: d.id, status: 'denied' })}
                disabled={decide.isPending}
                className={buttonClass()}
              >
                Decline
              </button>
            )}
            <button
              onClick={() => decide.mutate({ id: d.id, status: 'removed' })}
              disabled={decide.isPending}
              className={buttonClass()}
            >
              Remove
            </button>
          </div>
        </Row>
      ))}
    </>
  )
}

const DEVICE_STATUS: Record<AccessDevice['status'], string> = {
  pending: 'waiting for you',
  approved: 'accepted',
  denied: 'declined',
  lapsed: 'accepted, will ask again',
}

function CopyButton({ text }: { text: string }) {
  const { label, copy } = useCopy()
  return (
    <button
      onClick={() => void copy(text)}
      className="shrink-0 rounded px-1.5 py-0.5 text-[11px] text-base-400 hover:bg-base-800 hover:text-white"
    >
      {label}
    </button>
  )
}

// A new token signs every paired phone and TV out at once; they pair again with the new code.
function RevokeDevices() {
  const qc = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const rotate = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post('/api/access/rotate'),
    onSuccess: () => {
      setConfirming(false)
      void qc.invalidateQueries({ queryKey: ['access'] })
    },
  })
  return (
    <Row
      label="Paired devices"
      hint={
        rotate.isSuccess
          ? 'Every device was signed out. Scan the new code to pair again.'
          : rotate.isError
            ? (rotate.error as Error).message
            : 'Sign out every phone and TV that has the link.'
      }
    >
      {confirming ? (
        <div className="flex gap-2">
          <ConfirmInline
            label="Sign them out"
            onConfirm={() => rotate.mutate()}
            onCancel={() => setConfirming(false)}
            pending={rotate.isPending}
          />
        </div>
      ) : (
        <button
          onClick={() => setConfirming(true)}
          className={buttonClass()}
        >
          Sign out all devices
        </button>
      )}
    </Row>
  )
}

interface FirewallState {
  status: {
    supported: boolean
    networks: { name: string; interface: string; category: string }[]
    firewall?: string
    hint?: string
    command?: string
  }
  reachable: boolean
  blocked: boolean
  public: boolean
}

// Why a phone on the same Wi-Fi can or can't get in, and the fix (a Cancel or Public network blocks it).
function FirewallPanel() {
  const qc = useQueryClient()
  const [alsoPublic, setAlsoPublic] = useState(false)
  const fw = useQuery({
    queryKey: ['firewall'],
    queryFn: () => api.get<FirewallState>('/api/access/firewall'),
    refetchOnWindowFocus: true,
  })
  const allow = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<FirewallState>('/api/access/firewall', { public: alsoPublic }),
    onSuccess: (data) => qc.setQueryData(['firewall'], data),
  })
  const openSettings = useMutation({ mutationFn: () => api.post('/api/access/network-settings') })

  if (fw.isPending) return <Skeleton className="mt-4 h-16 w-full" />
  if (fw.isError || !fw.data) return null
  const { status, reachable, blocked } = fw.data
  const publicNets = status.networks.filter((n) => n.category === 'Public')

  // Linux and macOS: Yoru only says what it found.
  if (!status.supported) {
    if (!status.firewall) return null
    return (
      <div className="mt-4 rounded-md border border-base-800 bg-base-950/60 p-3 text-sm">
        <p className="text-base-200">{status.hint}</p>
        {status.command && (
          <code className="mt-2 block rounded bg-base-900 px-2 py-1.5 text-xs break-all text-accent-300 select-all">
            {status.command}
          </code>
        )}
      </div>
    )
  }

  return (
    <div
      className={cx(
        'mt-4 space-y-2.5 rounded-md border p-3 text-sm',
        reachable ? 'border-accent-500/30 bg-accent-500/5' : 'border-recap/40 bg-recap/10',
      )}
    >
      {reachable ? (
        <p className="text-base-200">
          ✓ Windows Firewall lets devices on {status.networks.map((n) => n.name).join(', ')} connect.
        </p>
      ) : blocked ? (
        <p className="text-base-100">
          Windows Firewall is blocking Yoru, probably from an earlier Cancel on its prompt. Allowing it
          fixes that.
        </p>
      ) : publicNets.length > 0 ? (
        <p className="text-base-100">
          Windows calls <strong>{publicNets.map((n) => n.name).join(', ')}</strong> a Public network and
          keeps other devices out. If it's your home Wi-Fi, set it to <strong>Private</strong>, then allow
          Yoru below.
        </p>
      ) : (
        <p className="text-base-100">Windows Firewall isn't letting other devices reach Yoru yet.</p>
      )}

      {!reachable && (
        <div className="flex flex-wrap items-center gap-2">
          {publicNets.length > 0 && (
            <button
              onClick={() => openSettings.mutate()}
              className={buttonClass('secondary', 'sm')}
            >
              Open network settings
            </button>
          )}
          <button
            onClick={() => allow.mutate()}
            disabled={allow.isPending}
            className={buttonClass('primary', 'sm')}
          >
            {allow.isPending ? 'Waiting for Windows…' : 'Allow through Windows Firewall'}
          </button>
          <button
            onClick={() => fw.refetch()}
            className="rounded-md px-2 py-1.5 text-xs text-base-400 hover:text-white"
          >
            Check again
          </button>
        </div>
      )}
      {!reachable && publicNets.length > 0 && (
        <label className="flex items-center gap-2 text-xs text-base-400">
          <input type="checkbox" checked={alsoPublic} onChange={(e) => setAlsoPublic(e.target.checked)} />
          Also allow on Public networks (cafés, hotels: anyone on them could try to connect; the link's
          token still keeps them out)
        </label>
      )}
      {allow.isError && <p className="text-xs text-recap">{(allow.error as Error).message}</p>}
      <p className="text-xs text-base-500">
        Windows will ask for permission. The rule only admits devices on the same local network. Still
        can't connect? Guest networks and routers with "client isolation" keep devices apart.
      </p>
    </div>
  )
}

interface UpdateStatus {
  current: string
  latest?: { version: string; url: string; notes?: string }
  available: boolean
  checkedAt?: string
  checkError?: string
  stage: 'idle' | 'downloading' | 'verifying' | 'restarting' | 'failed'
  bytes: number
  total: number
  error?: string
  pending?: Notes[]
  whatsNew?: Notes[]
  updatedFrom?: string
}

function AboutTab() {
  const qc = useQueryClient()
  const [stuck, setStuck] = useState(false)

  const status = useQuery({
    queryKey: ['update'],
    queryFn: () => api.get<UpdateStatus>('/api/update'),
    // A dev build has no updater; that is a plain answer, not an error to retry.
    retry: false,
    refetchInterval: (q) => {
      const stage = q.state.data?.stage
      return stage === 'downloading' || stage === 'verifying' || stage === 'restarting' ? 500 : false
    },
  })
  const check = useMutation({
    mutationFn: () => api.post<UpdateStatus>('/api/update/check'),
    onSuccess: (data) => qc.setQueryData(['update'], data),
  })
  const apply = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<UpdateStatus>('/api/update/apply'),
    onSuccess: (data) => qc.setQueryData(['update'], data),
  })

  const s = status.data
  const from = s?.current

  // Poll until a different version answers, then reload; a failed poll after verifying counts too.
  const handingOver =
    s?.stage === 'restarting' || (status.isError && (s?.stage === 'verifying' || s?.stage === 'downloading'))
  // Keyed on handingOver alone: one failed poll mid-download must not leave the restart unwatched.
  useEffect(() => {
    if (!handingOver) return
    let ticks = 0
    const timer = window.setInterval(async () => {
      // A new version that never comes up (or a rollback) must not leave the page spinning for good.
      if (++ticks === 90) setStuck(true)
      try {
        const health = await api.get<{ version?: string }>('/api/health')
        if (health.version && health.version !== from) window.location.reload()
      } catch {
        // Between the old process leaving and the new one binding.
      }
    }, 1000)
    return () => window.clearInterval(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [handingOver])

  if (status.isPending) return <Skeleton className="h-40 w-full" />

  // Only a 503 "updater unavailable" means a development build; other failures say what they are.
  if (!s && !(status.error instanceof ApiError && status.error.status === 503)) {
    return (
      <Section title="Yoru">
        <p className="text-sm text-recap">{(status.error as Error | null)?.message ?? 'Could not read the version.'}</p>
      </Section>
    )
  }
  if (!s) {
    return (
      <div className="space-y-4">
        <Section title="Yoru" hint="Development build: updates aren't checked.">
          <p className="text-sm text-base-400">Built from source.</p>
        </Section>
        <LogSection />
        <ComponentsSection />
        <JobsSection />
        <CreditsSection />
      </div>
    )
  }

  const busy = s.stage === 'downloading' || s.stage === 'verifying' || s.stage === 'restarting'
  const percent = s.total > 0 ? Math.round((s.bytes / s.total) * 100) : 0
  // Every release since this one, so a long jump reads all of it; the latest's own notes otherwise.
  const pendingNotes: Notes[] = s.pending?.length
    ? s.pending
    : s.latest?.notes
      ? [{ version: s.latest.version, notes: s.latest.notes }]
      : []

  const line = (() => {
    switch (s.stage) {
      case 'downloading':
        return s.total > 0 ? `Downloading… ${percent}%` : 'Downloading…'
      case 'verifying':
        return 'Verifying…'
      case 'restarting':
        return stuck
          ? 'The new version has not come up. Reload this page; if that fails, start Yoru again (it falls back to the previous version).'
          : 'Restarting into the new version…'
      case 'failed':
        return s.error ?? 'Update failed'
      default:
        if (s.checkError) return `Could not check: ${s.checkError}`
        if (s.available && s.latest) return `${s.latest.version} is available`
        if (s.checkedAt) return 'Up to date'
        return 'Not checked yet'
    }
  })()

  return (
    <div className="space-y-4">
      <Section title={`Yoru ${s.current}`} hint="Updates replace only yoru.exe. Downloads, settings and history stay.">
        <Row label={line} hint={s.checkedAt ? `Checked ${relativeTime(Date.parse(s.checkedAt) / 1000)}` : undefined}>
          <div className="flex gap-2">
            <button
              onClick={() => check.mutate()}
              disabled={busy || check.isPending}
              className={buttonClass()}
            >
              {check.isPending ? 'Checking…' : 'Check now'}
            </button>
            {s.available && (
              <button
                onClick={() => apply.mutate()}
                disabled={busy || apply.isPending}
                className={buttonClass('primary')}
              >
                {busy ? 'Updating…' : s.stage === 'failed' ? 'Try again' : `Update to ${s.latest?.version}`}
              </button>
            )}
          </div>
        </Row>
        {s.stage === 'downloading' && s.total > 0 && <ProgressBar value={percent} />}
        {apply.isError && <p className="text-xs text-recap">{(apply.error as Error).message}</p>}
        {s.available && pendingNotes.length > 0 && (
          <div className="space-y-2 py-3">
            <p className="text-xs text-base-400">
              {pendingNotes.length > 1 ? `What the update brings: ${pendingNotes.length} releases since yours` : 'What the update brings'}
            </p>
            <ReleaseNotesList notes={pendingNotes} />
          </div>
        )}
      </Section>

      {(s.whatsNew ?? []).some((n) => n.notes.trim()) && (
        <Section title={s.updatedFrom ? `What's new since ${s.updatedFrom}` : `What's new in ${s.current}`}>
          <div className="py-3">
            <ReleaseNotesList notes={s.whatsNew!} />
          </div>
        </Section>
      )}

      <LogSection />
      <ComponentsSection />
      <JobsSection />
      <CreditsSection />
    </div>
  )
}

// Every source the app's information comes from, credited as manami's Open Database License asks; TheTVDB's
// terms ask for a link to its site where its data shows.
const SOURCES: ReadonlyArray<{ name: string; gives: string; site?: string }> = [
  { name: 'AniList', gives: 'Show details, lists and schedules' },
  { name: 'MyAnimeList, through Jikan and Tenrai', gives: 'Episode titles, filler and recap flags, cast and staff' },
  { name: 'TheTVDB and AniDB, through ani.zip', gives: 'Episode numbering and pictures', site: 'https://thetvdb.com' },
  { name: 'AniSkip', gives: 'Opening and ending times' },
  { name: 'SeaDex', gives: 'The best release of a show' },
  { name: 'TRaSH Guides', gives: 'Release group grades (MIT License)' },
  { name: 'AniFiller and Anime Filler List', gives: 'Filler episodes' },
  { name: "manami-project's anime-offline-database", gives: 'Anime names and the links between sites (Open Database License)' },
  { name: "nattadasu's animeApi", gives: 'The links between sites' },
]

function CreditsSection() {
  return (
    <Section
      title="Data"
      hint="Where Yoru's anime information comes from. Yoru isn't affiliated with any of them."
      keywords="credits license sources"
    >
      {SOURCES.map((s) => (
        <Row key={s.name} label={s.name} hint={s.gives}>
          {s.site && (
            <a href={s.site} target="_blank" rel="noopener noreferrer" className="text-sm text-accent-300 hover:underline">
              {new URL(s.site).host}
            </a>
          )}
        </Row>
      ))}
    </Section>
  )
}

// Yoru's log, this run's and the last one's, to send when something goes wrong; no console shows it.
function LogSection() {
  const access = useAccess()
  const [copied, setCopied] = useState<'' | 'copied' | 'failed'>('')
  const row = useRef<HTMLDivElement>(null)
  const folder = useMutation({ meta: { inline: true }, mutationFn: () => api.post('/api/log/folder') })
  if (!access.data?.host) return null

  const copy = async () => {
    try {
      const res = await fetch('/api/log', { cache: 'no-store' })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      setCopied((await copyText(await res.text(), row.current ?? undefined)) ? 'copied' : 'failed')
    } catch {
      setCopied('failed')
    }
  }
  return (
    <Section
      title="Log"
      hint="What Yoru did this run and the last one. Send it when something goes wrong; it names your shows and files."
      keywords="logs debug report bug copy save send"
    >
      <Row label="Yoru's log">
        <div ref={row} className="flex flex-wrap justify-end gap-2">
          <button onClick={() => void copy()} className={buttonClass()}>
            {copied === 'copied' ? 'Copied' : 'Copy log'}
          </button>
          <a href="/api/log?download=1" download className={buttonClass()}>
            Save log
          </a>
          <button onClick={() => folder.mutate()} disabled={folder.isPending} className={buttonClass()}>
            Show in folder
          </button>
        </div>
      </Row>
      {copied === 'failed' && <p className="text-xs text-recap">Couldn't copy the log; Save log keeps it as a file.</p>}
      {folder.isError && <p className="text-xs text-recap">{(folder.error as Error).message}</p>}
    </Section>
  )
}

// The folders Yoru uses and the movable one, here too as only the first run opens Setup.
function WhereSection() {
  const setup = useSetup()
  if (!setup.data) return null
  return (
    <Section title="Where things go" hint="Folders Yoru reads and writes." keywords="folder path data cache location move">
      <WhereThingsGo setup={setup.data} />
    </Section>
  )
}

// Here too, or what the first run skipped is unreachable: only its nudge opens Setup.
function ComponentsSection() {
  const qc = useQueryClient()
  const setup = useSetup({
    refetchInterval: (q) =>
      (q.state.data?.progress ?? []).some((p) => p.stage !== 'done' && p.stage !== 'failed')
        ? 1000
        : false,
  })

  const install = useMutation({
    meta: { inline: true },
    mutationFn: (name: string) => api.post(`/api/setup/install/${name}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['setup'] }),
  })

  if (setup.isPending || setup.isError || !setup.data) return null
  const components = setup.data.components ?? []
  const progressFor = (name: string) =>
    (setup.data.progress ?? []).find((p) => p.component === name)

  return (
    <Section
      title="Programs Yoru uses"
      hint="Downloaded on demand. Installing one takes effect straight away."
      keywords="ffmpeg mpv components install"
    >
      {components.map((c) => {
        const progress = progressFor(c.name)
        return (
          <Row
            key={c.name}
            label={c.label}
            hint={
              c.problem ??
              (c.present
                ? `${c.system ? 'On this system' : 'Installed'}${c.version ? ` · ${c.version}` : ''}`
                : `${c.size}${c.required ? ' · required' : ' · optional'}`)
            }
          >
            {c.present && progress?.stage !== 'failed' && (!c.latest || c.latest === c.version) ? (
              <span className="text-sm text-accent-400">✓</span>
            ) : (
              <ComponentState
                progress={progress}
                present={c.present}
                manual={c.manual}
                version={c.version}
                latest={c.latest}
                onInstall={() => install.mutate(c.name)}
                pending={install.isPending && install.variables === c.name}
                requestError={
                  install.isError && install.variables === c.name ? (install.error as Error).message : undefined
                }
              />
            )}
          </Row>
        )
      })}
      {setup.data.encoder && <ConvertingRow encoder={setup.data.encoder} toneMapper={setup.data.toneMapper ?? ''} />}
      {install.isError && (
        <p className="px-3 pb-2 text-xs text-recap">{(install.error as Error).message}</p>
      )}
    </Section>
  )
}

// What converts the episodes a device can't play as they are, and what's missing when it's the processor alone.
function ConvertingRow({ encoder, toneMapper }: { encoder: string; toneMapper: string }) {
  const software = encoder === 'libx264'
  const hdr = { libplacebo: 'on the graphics card', zscale: 'on the processor' }[toneMapper]
  return (
    <Row
      label="Converting"
      keywords="encoder gpu hdr"
      hint={[
        software
          ? 'No graphics card encoder works here, so the processor converts: slower, and seeking waits longer.'
          : 'On the graphics card.',
        hdr ? `HDR is tone-mapped ${hdr}.` : "HDR can't be tone-mapped with this ffmpeg.",
      ].join(' ')}
    >
      <span className="text-sm text-base-300">{ENCODERS[encoder] ?? encoder}</span>
    </Row>
  )
}

function JobsSection() {
  const jobs = useQuery({
    queryKey: ['jobs'],
    queryFn: () => api.get<{ jobs: Array<Record<string, unknown>> }>('/api/jobs'),
    refetchInterval: 10_000,
  })
  const qc = useQueryClient()
  // Started, not awaited: the list shows it running at once, and a job that
  // was already running says so instead of looking like nothing happened.
  const run = useMutation({
    meta: { inline: true },
    mutationFn: (name: string) => api.post(`/api/jobs/${name}`),
    onSettled: () => qc.invalidateQueries({ queryKey: ['jobs'] }),
  })
  const busy = run.error instanceof ApiError && run.error.status === 409 ? run.variables : undefined
  // Any other refusal is said under its row too: inline keeps it out of the toasts.
  const failed = run.isError && busy === undefined ? run.variables : undefined

  return (
    <Section title="Background jobs" hint="Syncing, release checks and data mirrors." keywords="tasks schedule sync">
      {jobs.isPending ? (
        <Skeleton className="h-40 w-full" />
      ) : (
        jobs.data?.jobs?.map((job) => {
          const name = String(job.name)
          const note =
            busy === name && !job.running
              ? 'Already running.'
              : failed === name
                ? `Didn't start: ${(run.error as Error).message}`
                : job.lastError
                  ? String(job.lastError)
                  : ''
          return (
            <Row
              key={name}
              label={name}
              hint={[
                `every ${String(job.every)} · ${String(job.runs)} runs${Number(job.failures) > 0 ? ` · ${String(job.failures)} failed` : ''}`,
                note,
              ]
                .filter(Boolean)
                .join(' · ')}
            >
              <Button size="sm" onClick={() => run.mutate(name)} disabled={Boolean(job.running)}>
                {job.running ? 'Running' : 'Run'}
              </Button>
            </Row>
          )
        })
      )}
    </Section>
  )
}
