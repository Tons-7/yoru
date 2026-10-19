import {
  createContext,
  memo,
  useCallback,
  useContext,
  useDeferredValue,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { api, ApiError, withToken } from '../lib/api'
import { bytes, cx, relativeTime, speedLimit, type SpeedUnit } from '../lib/format'
import {
  refreshListState,
  useAccessDevices,
  useDecideDevice,
  usePrefs,
  useSetPref,
  useSetup,
  type AccessDevice,
  type ApprovalMode,
} from '../lib/queries'
import { ANIME4K_MODES, ANIME4K_SIZES } from '../lib/anime4k'
import { ComponentState } from '../components/ComponentState'
import { ReleaseNotesList, type Notes } from '../components/ReleaseNotes'
import { WhereThingsGo } from '../components/WhereThingsGo'
import { DESKTOP_NOTIFY_KEY, desktopNotifyWanted } from '../lib/notifications'
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
  TextInput,
} from '../components/ui'
import { buttonClass, ControlLabel } from '../lib/ui'

// The id is what ?tab= says; the label is what a person reads.
const TABS = [
  { id: 'Playback', label: 'Playback' },
  { id: 'Sound', label: 'Sound & picture' },
  { id: 'Downloads', label: 'Downloads' },
  { id: 'Storage', label: 'Storage' },
  { id: 'Accounts', label: 'Accounts' },
  { id: 'Library', label: 'Library' },
  { id: 'Notifications', label: 'Notifications' },
  { id: 'Devices', label: 'Devices' },
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
  // The URL holds the tab: a reload keeps it, and a link to another one always switches.
  const tab: Tab = isTab(wanted) ? wanted : (renamed ?? (connected ? 'Accounts' : 'Playback'))
  // So does the search (?q=), so a link from elsewhere can open on the setting it is about.
  const query = (params.get('q') ?? '').trim().toLowerCase()
  // Every tab renders under a search; deferred, typing stays quick while the results catch up.
  const shownQuery = useDeferredValue(query)
  const setParam = (key: string, value: string | null) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    setParams(next, { replace: true })
  }
  const setTab = (t: Tab) => {
    const next = new URLSearchParams(params)
    next.set('tab', t)
    next.delete('q')
    setParams(next, { replace: true })
  }
  // The latest handlers behind steady ones, or the memoised results would redraw on every keystroke anyway.
  const handlers = useRef({ setTab, clear: () => setParam('q', null) })
  useLayoutEffect(() => {
    handlers.current = { setTab, clear: () => setParam('q', null) }
  })
  const openTab = useCallback((t: Tab) => handlers.current.setTab(t), [])
  const clearSearch = useCallback(() => handlers.current.clear(), [])

  return (
    // Centred as one block: the slack becomes margins, not a gulf between every label and its control.
    <div className="mx-auto max-w-[59rem] space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
        <PageHeader title="Settings" />
        <SearchInput
          value={params.get('q') ?? ''}
          onChange={(e) => setParam('q', e.target.value)}
          onKeyDown={(e) => e.key === 'Escape' && setParam('q', null)}
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
            {TABS.map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
              </option>
            ))}
          </Select>

          <div className="flex gap-6">
            <SideTabs tab={tab} onChange={setTab} />
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
    case 'Playback':
      return <PlaybackTab />
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
        {TABS.map((t) => (
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

function SideTabs({ tab, onChange }: { tab: Tab; onChange: (t: Tab) => void }) {
  return (
    // Stays beside a long tab instead of scrolling away with its top.
    <div role="tablist" className="sticky top-20 hidden w-44 shrink-0 flex-col gap-0.5 self-start lg:flex">
      {TABS.map((t) => (
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
        </button>
      ))}
    </div>
  )
}

// One line icon per tab, drawn as the app's other glyphs are (24 grid, 1.6 stroke).
const TAB_ICONS: Record<Tab, ReactNode> = {
  Playback: <path d="M8.5 5.5v13l10-6.5-10-6.5Z" strokeLinejoin="round" />,
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
  // Searching, an unmatched section keeps only its matching rows, and goes when none is left.
  const all = !query || tabMatched || matches(query, title, hint, keywords)
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
  const shown = !query || all || matches(query, label, hint, keywords)
  return (
    // Wraps on a narrow screen rather than clipping buttons off the edge.
    <div
      data-row
      data-shown={query && shown ? '' : undefined}
      hidden={!shown}
      className={cx('flex flex-wrap justify-between gap-x-5 gap-y-2 py-3', stacked ? 'flex-col' : 'items-center')}
    >
      {/* basis-48 is a width beside the control; in a stacked (column) row it became a 12rem height. */}
      <div className={cx('min-w-0', !stacked && 'flex-1 basis-48')}>
        <p className="text-sm text-base-100">{label}</p>
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
  const rowLabel = useContext(RowLabel)
  return (
    <button
      role="switch"
      aria-checked={on}
      aria-label={label ?? rowLabel}
      onClick={() => onChange(!on)}
      className={cx(
        'tap relative h-5 w-9 shrink-0 rounded-full transition-colors',
        on ? 'bg-accent-500' : 'bg-base-700',
      )}
    >
      <span
        className={cx(
          'absolute top-0.5 size-4 rounded-full bg-white transition-all',
          on ? 'left-[1.125rem]' : 'left-0.5',
        )}
      />
    </button>
  )
}

function useFlag() {
  const prefs = usePrefs()
  const setPref = useSetPref()
  const values = prefs.data?.effective ?? {}
  return {
    loading: prefs.isPending,
    get: (key: string) => values[key] === 'true',
    value: (key: string) => values[key] ?? '',
    set: (key: string, value: string) => setPref.mutate({ key, value }),
  }
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

  return (
    <div className="space-y-4">
      <Section title="Automatic" hint="Flipping one in the player changes it here too.">
        <Row label="Auto play" hint="Starts as soon as the page opens." keywords="autoplay">
          <Switch on={f.get('playback.autoplay')} onChange={(v) => f.set('playback.autoplay', String(v))} />
        </Row>
        <Row label="Auto next episode" hint="Counts down at the end, then plays the next." keywords="autonext countdown">
          <Switch on={f.get('playback.autonext')} onChange={(v) => f.set('playback.autonext', String(v))} />
        </Row>
        <Row label="Skip opening" keywords="intro op">
          <Switch on={f.get('playback.autoskip_op')} onChange={(v) => f.set('playback.autoskip_op', String(v))} />
        </Row>
        <Row label="Skip ending" keywords="outro ed credits">
          <Switch on={f.get('playback.autoskip_ed')} onChange={(v) => f.set('playback.autoskip_ed', String(v))} />
        </Row>
        <Row label="Skip filler and recaps" hint="Next and download ahead step over them; the list still shows them.">
          <Switch on={f.get('playback.skip_filler')} onChange={(v) => f.set('playback.skip_filler', String(v))} />
        </Row>
      </Section>

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
        <Row label="Open Yoru" hint="From the next start. F11 switches either way." keywords="window fullscreen maximized">
          <Select value={f.value('window.mode') || 'fullscreen'} onChange={(v) => f.set('window.mode', v)}>
            <option value="fullscreen">Fullscreen</option>
            <option value="maximized">Maximized</option>
          </Select>
        </Row>
      </Section>

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
    </div>
  )
}

function SoundTab() {
  const f = useFlag()
  if (f.loading) return <Skeleton className="h-64 w-full" />

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
      </Section>
    </div>
  )
}

// The card is this computer's Discord account, so it is set here only.
function DiscordSection({ f }: { f: ReturnType<typeof useFlag> }) {
  const access = useQuery({ queryKey: ['access'], queryFn: () => api.get<{ host?: boolean }>('/api/access') })
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
  const cache = useQuery({ queryKey: ['cache'], queryFn: () => api.get<Record<string, number>>('/api/cache') })
  const sweep = useMutation({
    mutationFn: () => api.post('/api/cache/sweep'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['cache'] }),
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
  const found = preview.data

  if (f.loading) return <Skeleton className="h-48 w-full" />

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
            hint="Downloads are never evicted, so this is their only automatic cleanup."
          >
            <Switch
              on={f.get('cache.autodelete_downloads')}
              onChange={(v) => f.set('cache.autodelete_downloads', String(v))}
            />
          </Row>
        )}
        <Row
          label="In use"
          hint={
            cache.data
              ? `${bytes(Number(cache.data.bytes ?? 0))} cached · ${bytes(Number(cache.data.keptBytes ?? 0))} downloaded`
              : 'Loading…'
          }
        >
          <button
            onClick={() => sweep.mutate()}
            disabled={sweep.isPending}
            className={buttonClass()}
          >
            {sweep.isPending ? 'Sweeping…' : 'Sweep now'}
          </button>
        </Row>
        <Row
          label="Orphaned files"
          hint={
            orphans.data
              ? `Removed ${orphans.data.removed} · freed ${bytes(orphans.data.freedBytes)}${
                  orphans.data.kept?.length ? ` · kept ${orphans.data.kept.length} now in use` : ''
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

  return (
    <div className="space-y-4">
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
        <Row label="Allow 10-bit H.264 (Hi10P)" hint="No hardware decodes it; expect high CPU." keywords="hi10p codec">
          <Switch on={f.get('quality.allow_hi10p')} onChange={(v) => f.set('quality.allow_hi10p', String(v))} />
        </Row>
        <Row label="Auto-download followed anime" hint="New episodes of shows you follow download by themselves." keywords="automatic new episodes">
          <Switch on={f.get('autodownload.enabled')} onChange={(v) => f.set('autodownload.enabled', String(v))} />
        </Row>
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
  const access = useQuery({ queryKey: ['access'], queryFn: () => api.get<{ host?: boolean }>('/api/access') })
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
  const access = useQuery({ queryKey: ['access'], queryFn: () => api.get<{ host?: boolean }>('/api/access') })
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

// Release groups to favour, typed as a comma list and stored as a JSON array.
function PreferGroups({ f }: { f: ReturnType<typeof useFlag> }) {
  const stored: string[] = (() => {
    try {
      const parsed = JSON.parse(f.value('release.prefer_groups') || '[]')
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
    const groups = text.split(',').map((g) => g.trim()).filter(Boolean)
    if (groups.join(',') !== stored.join(',')) setGroups.mutate({ key: 'release.prefer_groups', value: JSON.stringify(groups) })
  }

  return (
    <Row
      label="Preferred release groups"
      hint="Comma separated, e.g. SubsPlease, Erai-raws. A match ranks the release higher."
      keywords="fansub uploader"
    >
      <TextInput
        value={text}
        onChange={(e) => setText(e.target.value)}
        onBlur={save}
        onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
        placeholder="SubsPlease, Erai-raws"
        aria-label="Preferred release groups"
        className="w-56"
      />
      <SaveHint state={setGroups} />
    </Row>
  )
}

interface Tracker {
  provider: string
  name: string
  clientId: string
  hasSecret: boolean
  configured: boolean
  connected: boolean
  reconnect?: boolean
  user?: string
  redirect: string
  register: string
  secretRequired: boolean
}

function AccountsTab() {
  const f = useFlag()
  const trackers = useQuery({
    queryKey: ['trackers'],
    queryFn: () => api.get<{ trackers: Tracker[] }>('/api/trackers'),
  })
  // Signing in or out and the keys are the host's; a paired device only reads the state.
  const access = useQuery({ queryKey: ['access'], queryFn: () => api.get<{ host?: boolean }>('/api/access') })
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
      {!f.loading && <DiscordSection f={f} />}
    </div>
  )
}

function TrackerCard({ tracker, host }: { tracker: Tracker; host: boolean }) {
  const qc = useQueryClient()
  const [clientId, setClientId] = useState(tracker.clientId)
  const [secret, setSecret] = useState('')
  const [editing, setEditing] = useState(!tracker.configured)

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

  const status = tracker.reconnect
    ? `${tracker.name} refused the saved login. Reconnect to resume syncing.`
    : tracker.connected
      ? tracker.user ? `Connected as ${tracker.user}` : 'Connected'
      : tracker.configured
        ? 'Set up, not connected'
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
          {host && (
            <button
              onClick={() => setEditing((open) => !open)}
              className={buttonClass()}
            >
              {editing ? 'Cancel' : tracker.configured ? 'Edit keys' : 'Add keys'}
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

      {importFavs.data && (
        <p className="px-3 pb-2 text-xs text-base-400">
          {importFavs.data.favourited} favourited of {importFavs.data.found} on MyAnimeList
          {importFavs.data.unmatched > 0 && `, ${importFavs.data.unmatched} not in the catalogue`}.
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

function CopyField({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)

  return (
    <div className="flex items-center gap-2">
      <code className="min-w-0 flex-1 truncate rounded-md border border-base-800 bg-base-900 px-2.5 py-1.5 text-xs text-base-200">
        {value}
      </code>
      <button
        onClick={() => {
          navigator.clipboard.writeText(value)
          setCopied(true)
          window.setTimeout(() => setCopied(false), 1500)
        }}
        className="shrink-0 rounded-md bg-base-800 px-2.5 py-1.5 text-xs text-base-100 hover:bg-base-700"
      >
        {copied ? 'Copied' : 'Copy'}
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

  const result = sync.data as { entries?: number; removed?: number } | undefined

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
function DesktopNotifyRow() {
  const supported = typeof Notification !== 'undefined'
  const [on, setOn] = useState(desktopNotifyWanted())
  const [permission, setPermission] = useState(supported ? Notification.permission : 'denied')

  const toggle = async (next: boolean) => {
    if (next && permission !== 'granted') {
      const got = await Notification.requestPermission()
      setPermission(got)
      if (got !== 'granted') return
    }
    try {
      localStorage.setItem(DESKTOP_NOTIFY_KEY, next ? '1' : '0')
    } catch {
      // Storage unavailable; the switch just will not stick.
    }
    setOn(next)
  }

  if (!supported) return null
  return (
    <Row
      label="Desktop alerts"
      hint={
        permission === 'denied'
          ? 'Blocked by the browser; allow notifications for this site to use it.'
          : 'Pop up on this device when a new episode appears, even with Yoru in another tab.'
      }
    >
      <Switch label="Desktop alerts" on={on && permission === 'granted'} onChange={(v) => void toggle(v)} />
    </Row>
  )
}

function NotificationsTab() {
  const f = useFlag()
  if (f.loading) return <Skeleton className="h-40 w-full" />

  return (
    <Section title="Release notifications" hint="Checks for new episodes of anime you follow." keywords="alerts new episodes">
      <Row label="Enabled">
        <Switch on={f.get('notify.enabled')} onChange={(v) => f.set('notify.enabled', String(v))} />
      </Row>
      <DesktopNotifyRow />
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
  const qc = useQueryClient()
  const access = useQuery({
    queryKey: ['access'],
    queryFn: () =>
      api.get<{
        reachable: boolean
        listening: string
        urls: string[]
        canSwitch: boolean
        host?: boolean
      }>('/api/access'),
  })

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
          Only this machine can reach Yoru right now.
        </p>
      ) : access.data.urls.length === 0 ? (
        <p className="text-sm text-base-300">
          This machine has no home-network address right now (not on Wi-Fi or Ethernet, or only on a
          VPN), so there is nothing a phone could open yet.
        </p>
      ) : (
        <div className="flex flex-wrap items-center gap-5">
          <img
            src="/api/access/qr.svg"
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
      {access.data?.reachable && access.data.host && <FirewallPanel />}
      {access.data?.reachable && access.data.host && <DeviceApproval />}
      {access.data?.reachable && access.data.host && <InstallAsApp />}
      {access.data?.reachable && access.data.host && <RevokeDevices />}
    </Section>
  )
}

const HTTPS_INPUT = cx(inputClass, 'min-w-0 flex-1 basis-48')

interface HTTPSStatus {
  domain: string
  state: 'off' | 'working' | 'ready' | 'error'
  problem?: string
  expires?: number
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
  const [domain, setDomain] = useState('')
  const [token, setToken] = useState('')
  const save = useMutation({
    meta: { inline: true },
    mutationFn: (body: { domain: string; token: string }) => api.post<HTTPSStatus>('/api/access/https', body),
    onSuccess: (status) => {
      qc.setQueryData(['access-https'], status)
      setEditing(false)
      setToken('')
    },
    // The pairing links switch to https once the certificate is in.
    onSettled: () => qc.invalidateQueries({ queryKey: ['access'] }),
  })
  const state = https.data?.state ?? 'off'
  useEffect(() => {
    if (state === 'ready') void qc.invalidateQueries({ queryKey: ['access'] })
  }, [state, qc])

  const hint = save.isError
    ? (save.error as Error).message
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
        {state === 'off' || editing ? null : (
          <div className="flex gap-2">
            <button
              onClick={() => {
                setDomain(https.data?.domain ?? '')
                setEditing(true)
              }}
              className={buttonClass()}
            >
              Change
            </button>
            <button onClick={() => save.mutate({ domain: '', token: '' })} className={buttonClass()}>
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
          hint={`${d.addr} · ${DEVICE_STATUS[d.status]} · asked ${relativeTime(d.requestedAt)}`}
        >
          <div className="flex flex-wrap justify-end gap-2">
            {d.status !== 'approved' && d.status !== 'lapsed' && (
              <button
                onClick={() => decide.mutate({ id: d.id, status: 'approved' })}
                className={buttonClass(d.status === 'pending' ? 'primary' : undefined)}
              >
                Accept
              </button>
            )}
            {d.status === 'pending' && (
              <button onClick={() => decide.mutate({ id: d.id, status: 'denied' })} className={buttonClass()}>
                Decline
              </button>
            )}
            <button onClick={() => decide.mutate({ id: d.id, status: 'removed' })} className={buttonClass()}>
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
  const [copied, setCopied] = useState(false)
  return (
    <button
      onClick={() =>
        void navigator.clipboard?.writeText(text).then(() => {
          setCopied(true)
          window.setTimeout(() => setCopied(false), 1500)
        })
      }
      className="shrink-0 rounded px-1.5 py-0.5 text-[11px] text-base-400 hover:bg-base-800 hover:text-white"
    >
      {copied ? 'Copied' : 'Copy'}
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
        <ComponentsSection />
        <JobsSection />
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

      <ComponentsSection />
      <JobsSection />
    </div>
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
      {install.isError && (
        <p className="px-3 pb-2 text-xs text-recap">{(install.error as Error).message}</p>
      )}
    </Section>
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
