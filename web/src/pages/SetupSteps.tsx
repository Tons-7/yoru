import { useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, type Tracker } from '../lib/api'
import { bytes, cx } from '../lib/format'
import { useAvatarEdit, useProfile } from '../lib/profile'
import { setDeviceAlerts } from '../lib/push'
import type { SetupComponent, SetupState } from '../lib/queries'
import { SECRET, secretClass } from '../lib/secret'
import { AFTER, AUDIO, BUDGETS_GB, FIRST_PICKS, PARTS, PICTURE, TITLES, partMoving, type Guide, type Screen } from '../lib/setupGuide'
import { buttonClass } from '../lib/ui'
import { AvatarEditor } from '../components/AvatarEditor'
import { NameChoice } from '../components/NameChoice'
import { Card, Choices, Continue, Footer, Frame, Tick, ToggleRow } from '../components/SetupBits'
import { SitesEditor } from '../components/SitesEditor'
import { WhereThingsGo } from '../components/WhereThingsGo'
import { Avatar } from '../components/YouBits'
import { ProgressBar, Segmented, inputClass } from '../components/ui'
import { useUpdateStatus } from '../lib/update'

export interface StepProps {
  guide: Guide
  setup: SetupState
  back: Screen
  skip: Screen
  next: () => void
}

const GB = 1024 ** 3
const mark = 'grid size-6 shrink-0 place-items-center rounded-full'

export function Essentials({ guide, setup, back, next }: StepProps) {
  const qc = useQueryClient()
  const [picked, setPicked] = useState(() => new Set(FIRST_PICKS))
  const install = useMutation({
    meta: { inline: true },
    mutationFn: (name: string) => api.post(`/api/setup/install/${name}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['setup'] }),
  })
  const has = (name: string) => guide.parts.some((c) => c.name === name && c.present)
  const blocked = (c: SetupComponent) => !!c.needs && !has(c.needs) && !picked.has(c.needs)
  const wanted = guide.parts.filter(
    (c) => picked.has(c.name) && !c.present && !c.manual && !blocked(c) && !partMoving(guide.progress(c.name)),
  )
  const toggle = (name: string) =>
    setPicked((was) => {
      const now = new Set(was)
      if (!now.delete(name)) now.add(name)
      return now
    })

  return (
    <Frame
      title="Get the essentials"
      lead="Yoru is small because the big parts are fetched here. The first one is needed to play in Yoru; the rest are extras you can add now or later."
    >
      {setup.temporary && (
        <p className="rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-100">
          Yoru is running from inside the zip, so its settings are lost when the zip closes. Extract it to a folder of its own first.
        </p>
      )}
      <Card>
        <ul>
          {guide.parts.map((c) => {
            const part = PARTS[c.name] ?? { title: c.label, text: c.purpose }
            const progress = guide.progress(c.name)
            const running = partMoving(progress)
            const failed = progress?.stage === 'failed'
            const percent = progress && progress.total > 0 ? (progress.bytes / progress.total) * 100 : 0
            const fixed = c.present || running || !!c.manual
            const locked = blocked(c)
            const body = (
              <>
                <span className="min-w-0 flex-1">
                  <span className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
                    <span className="text-[15px] font-semibold text-white">{part.title}</span>
                    {c.name === 'ffmpeg' && !c.present && (
                      <span className="rounded-full bg-amber-400/15 px-2 py-0.5 text-[11px] font-semibold text-amber-200">Needed</span>
                    )}
                  </span>
                  <span className="mt-1 block text-[13px] text-base-400">
                    {locked ? `Needs the ${(PARTS[c.needs ?? '']?.title ?? c.needs ?? '').toLowerCase()}.` : part.text}
                  </span>
                  {running && <ProgressBar value={percent} label={`Downloading ${part.title}`} className="mt-2" />}
                  {failed && <span className="mt-1 block text-xs text-recap">{progress?.error ?? 'The download failed.'} Continue tries again.</span>}
                  {c.problem && <span className="mt-1 block text-xs text-recap">{c.problem}</span>}
                  {!c.present && c.manual && (
                    <span className="mt-2 block text-xs text-base-400">
                      Install it yourself, then reload: <code className="rounded bg-base-800 px-2 py-1 text-base-100 select-all">{c.manual}</code>
                    </span>
                  )}
                </span>
                <span className="shrink-0 text-[13px] whitespace-nowrap text-base-400">
                  {c.present
                    ? 'Installed'
                    : running
                      ? progress && progress.total > 0
                        ? `${bytes(progress.bytes)} of ${bytes(progress.total)}`
                        : 'Starting'
                      : c.size}
                </span>
              </>
            )
            return (
              <li key={c.name} className="border-b border-base-850 last:border-b-0">
                {fixed ? (
                  <div className="flex items-center gap-4 px-5 py-4 sm:px-6">
                    {c.present ? (
                      <span className={cx(mark, 'bg-emerald-400/15 text-emerald-300')}>
                        <Tick className="size-3.5" label="Installed" />
                      </span>
                    ) : running ? (
                      <span aria-hidden className="size-6 shrink-0 animate-spin rounded-full border-2 border-accent-400/25 border-t-accent-400 motion-reduce:animate-none" />
                    ) : (
                      <span aria-hidden className={cx(mark, 'border-2 border-dashed border-base-700')} />
                    )}
                    {body}
                  </div>
                ) : (
                  <label className={cx('flex items-center gap-4 px-5 py-4 sm:px-6', !locked && 'cursor-pointer')}>
                    <input
                      type="checkbox"
                      checked={picked.has(c.name) && !locked}
                      disabled={locked}
                      onChange={() => toggle(c.name)}
                      className="peer sr-only"
                    />
                    <span
                      aria-hidden
                      className={cx(
                        mark,
                        'border-2 border-base-600 text-transparent transition-colors peer-checked:border-accent-500 peer-checked:bg-accent-500 peer-checked:text-white peer-focus-visible:ring-2 peer-focus-visible:ring-white peer-disabled:opacity-50',
                      )}
                    >
                      <Tick className="size-3.5" />
                    </span>
                    {body}
                  </label>
                )}
              </li>
            )
          })}
        </ul>
      </Card>
      <p className="text-[13px] text-base-400">
        {install.isError ? <span className="text-recap">{(install.error as Error).message}</span> : "Setup carries on while these download. You don't have to wait here."}
      </p>
      <Footer back={back}>
        <Continue
          label={wanted.length > 0 ? 'Download and continue' : 'Continue'}
          onClick={() => {
            wanted.forEach((c) => install.mutate(c.name))
            next()
          }}
        />
      </Footer>
    </Frame>
  )
}

export function Sources({ guide, back, skip, next }: StepProps) {
  return (
    <Frame
      title="Where episodes come from"
      lead="Yoru searches the torrent sites you add here and plays what it finds. It comes with none, so add at least one to be able to play anything."
    >
      <Card className="px-5 py-1 sm:px-6">
        <SitesEditor />
      </Card>
      <p className={cx('text-[13px]', guide.notes.sources.warn ? 'text-amber-200' : 'text-base-400')}>
        {guide.notes.sources.warn ?? 'Saved as you add them. The first site in the list is searched first.'}
      </p>
      <Footer back={back} skip={guide.notes.sources.done ? undefined : skip}>
        <Continue onClick={next} />
      </Footer>
    </Frame>
  )
}

function TrackerCard({ tracker }: { tracker: Tracker }) {
  const qc = useQueryClient()
  const [pasting, setPasting] = useState(false)
  const [code, setCode] = useState('')
  const connect = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post('/api/auth/token', { token: code }),
    onSuccess: () => {
      setCode('')
      setPasting(false)
      void qc.invalidateQueries({ queryKey: ['trackers'] })
      void qc.invalidateQueries({ queryKey: ['profile'] })
    },
  })
  const anilist = tracker.provider === 'anilist'
  const login = `${anilist ? '/api/auth/login' : '/api/mal/auth/login'}?from=setup`
  const quick = tracker.configured ? undefined : tracker.tokenUrl
  const action = tracker.reconnect ? 'Sign in again' : 'Connect'
  const open = !tracker.connected || tracker.reconnect
  return (
    <Card label={tracker.name} className={cx('flex flex-col gap-3 px-5 py-4', tracker.connected && !tracker.reconnect && 'border-emerald-400/40')}>
      <div className="flex flex-wrap items-center gap-3.5">
        <span
          aria-hidden
          className={cx(
            'grid size-11 shrink-0 place-items-center rounded-full font-display font-bold',
            anilist ? 'bg-[#1a2740] text-base text-[#7ab8ff]' : 'bg-[#1c2a52] text-[13px] text-[#a9bdff]',
          )}
        >
          {anilist ? 'AL' : 'MAL'}
        </span>
        <span className="min-w-0 flex-1 basis-32">
          <span className="block text-base font-semibold text-white">{tracker.name}</span>
          {tracker.reconnect ? (
            <span className="block text-[13px] text-amber-200">It refused the saved sign-in</span>
          ) : tracker.connected ? (
            <span className="flex items-center gap-1.5 text-[13px] text-emerald-300">
              <span aria-hidden className="size-[7px] rounded-full bg-emerald-400" />
              {tracker.user ? `Connected as ${tracker.user}` : 'Connected'}
            </span>
          ) : (
            <span className="block text-[13px] text-base-400">
              {tracker.configured || quick ? 'Not connected' : 'Needs API keys of your own first'}
            </span>
          )}
        </span>
        {open && tracker.configured && (
          <a href={login} className={buttonClass('primary')}>
            {action}
          </a>
        )}
        {open && quick && (
          <a href={quick} target="_blank" rel="noreferrer" onClick={() => setPasting(true)} className={buttonClass('primary')}>
            {action}
          </a>
        )}
        {(!open || (!tracker.configured && !quick)) && (
          <Link to="/settings?tab=Accounts" className={buttonClass()}>
            {open ? 'Add keys in Settings' : 'Manage in Settings'}
          </Link>
        )}
      </div>
      {pasting && quick && open && (
        <div className="flex flex-wrap items-end gap-2.5 border-t border-base-850 pt-3">
          <label className="min-w-0 flex-1 basis-56 text-xs font-medium text-base-300">
            Approve Yoru on the page that opened, then paste its code
            <input
              {...SECRET}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              className={cx(inputClass, secretClass, 'mt-1.5 block w-full')}
            />
          </label>
          <button type="button" onClick={() => connect.mutate()} disabled={connect.isPending || code.trim() === ''} className={buttonClass('primary')}>
            {connect.isPending ? 'Connecting…' : 'Connect'}
          </button>
          {connect.isError && <p className="basis-full text-xs text-recap">{(connect.error as Error).message}</p>}
        </div>
      )}
    </Card>
  )
}

export function Account({ guide, back, skip, next }: StepProps) {
  const profile = useProfile().data
  const { upload } = useAvatarEdit()
  const file = useRef<HTMLInputElement>(null)
  const [picked, setPicked] = useState<File | null>(null)
  return (
    <Frame
      title="Your list and your name"
      lead="Connect the site where you keep your anime list and Yoru brings it in and keeps it up to date as you watch. Or carry on without one: Yoru keeps a list of its own."
    >
      <div className="grid gap-3.5 md:grid-cols-2">
        {guide.trackers.map((tracker) => (
          <TrackerCard key={tracker.provider} tracker={tracker} />
        ))}
      </div>
      <Card className="flex flex-col gap-4 px-5 py-5 sm:px-6">
        <div className="flex flex-wrap items-center gap-4">
          <Avatar name={profile?.shown} className="size-14" letter="text-2xl" />
          <div className="min-w-0 flex-1 basis-48">
            <h2 className="text-base font-semibold text-white">Your name in Yoru</h2>
            <p className="mt-0.5 text-[13px] text-base-400">Shown in Yoru and to friends in watch-together rooms.</p>
          </div>
          <input
            ref={file}
            type="file"
            accept="image/*"
            hidden
            onChange={(e) => {
              const chosen = e.target.files?.[0]
              e.target.value = ''
              if (chosen) setPicked(chosen)
            }}
          />
          <button type="button" onClick={() => file.current?.click()} disabled={upload.isPending} className={buttonClass()}>
            {upload.isPending ? 'Saving…' : 'Choose a picture'}
          </button>
        </div>
        {picked && (
          <AvatarEditor
            file={picked}
            onCancel={() => setPicked(null)}
            onSave={(image) => {
              setPicked(null)
              upload.mutate(image)
            }}
          />
        )}
        {upload.isError && <p className="text-xs text-recap">{(upload.error as Error).message}</p>}
        <NameChoice host />
      </Card>
      <Footer back={back} skip={guide.notes.account.done ? undefined : skip}>
        <Continue onClick={next} />
      </Footer>
    </Frame>
  )
}

export function Watching({ guide, back, next }: StepProps) {
  const skips = guide.value('playback.autoskip_op') === 'auto' && guide.value('playback.autoskip_ed') === 'auto'
  return (
    <Frame title="How you like to watch" lead="Yoru uses these for every show. Any show can still have its own.">
      <Choices label="Sound" name="sound" value={guide.value('audio.prefer') || 'sub'} options={AUDIO} onChange={(v) => guide.set('audio.prefer', v)} />
      <Choices label="Picture" name="picture" value={guide.value('quality.prefer') || 'best'} options={PICTURE} onChange={(v) => guide.set('quality.prefer', v)} />
      <Choices label="Show names in" name="titles" columns={2} value={guide.value('display.titles') || 'english'} options={TITLES} onChange={(v) => guide.set('display.titles', v)} />
      <Card>
        <ul>
          <ToggleRow
            title="Skip openings and endings by itself"
            hint={skips ? 'With an Undo each time.' : 'A Skip button shows instead.'}
            on={skips}
            onChange={(v) => {
              guide.set('playback.autoskip_op', v ? 'auto' : 'button')
              guide.set('playback.autoskip_ed', v ? 'auto' : 'button')
            }}
          />
          <ToggleRow
            title="Play the next episode by itself"
            on={guide.on('playback.autonext')}
            onChange={(v) => guide.set('playback.autonext', String(v))}
          />
        </ul>
      </Card>
      <Footer back={back}>
        <Continue onClick={next} />
      </Footer>
    </Frame>
  )
}

export function Storage({ guide, setup, back, next }: StepProps) {
  const budget = guide.value('cache.budget_bytes')
  return (
    <Frame
      title="Where things go"
      lead="Episodes are kept after you watch them, so a rewatch starts at once. Say how much room they may take."
    >
      <Card className="px-5 py-5 sm:px-6">
        <div className="flex flex-wrap items-baseline justify-between gap-3">
          <h2 className="text-[15px] font-semibold text-white">Space watched episodes may use</h2>
          <span className="text-[13px] text-base-400">Your own downloads don't count.</span>
        </div>
        <div role="radiogroup" aria-label="Space watched episodes may use" className="mt-3.5 flex flex-wrap gap-2">
          {BUDGETS_GB.map((gb) => {
            const active = budget === String(gb * GB)
            return (
              <button
                key={gb}
                type="button"
                role="radio"
                aria-checked={active}
                onClick={() => guide.set('cache.budget_bytes', String(gb * GB))}
                className={cx(
                  'h-11 rounded-full border px-5 text-sm transition-colors',
                  active ? 'border-accent-500 bg-accent-500/10 font-semibold text-white' : 'border-base-700 text-base-200 hover:border-base-500',
                )}
              >
                {gb} GB
              </button>
            )
          })}
        </div>
        <p className="mt-3.5 text-[13px] text-base-400">When it fills up, the oldest watched episodes go first.</p>
      </Card>
      <Choices
        label="After you finish an episode"
        name="after"
        value={guide.value('cache.autodelete') || 'off'}
        options={AFTER}
        onChange={(v) => guide.set('cache.autodelete', v)}
      />
      <Card label="Folders" className="px-5 py-1 sm:px-6">
        <WhereThingsGo setup={setup} />
      </Card>
      <Footer back={back}>
        <Continue onClick={next} />
      </Footer>
    </Frame>
  )
}

export function Computer({ guide, back, next }: StepProps) {
  const qc = useQueryClient()
  const fromSource = !!useUpdateStatus().data?.fixed
  const startup = useQuery({
    queryKey: ['app-startup'],
    queryFn: () => api.get<{ on: boolean; tray: boolean }>('/api/app/startup'),
  })
  const setStartup = useMutation({
    meta: { inline: true },
    mutationFn: (on: boolean) => api.post<{ on: boolean; tray: boolean }>('/api/app/startup', { on }),
    onSuccess: (data) => qc.setQueryData(['app-startup'], data),
  })
  const [alertNote, setAlertNote] = useState('')
  const tray = startup.data?.tray !== false
  const closes = guide.value('window.on_close') === 'tray' ? 'tray' : 'exit'
  const alerts = async (on: boolean) => {
    guide.set('notify.enabled', String(on))
    setAlertNote('')
    try {
      if (on && !(await setDeviceAlerts(true))) setAlertNote('Notifications are blocked for Yoru, so alerts only show inside it.')
    } catch {
      setAlertNote('Alerts show while Yoru is open.')
    }
  }
  return (
    <Frame
      title="Yoru on this computer"
      lead="Last step. Yoru has to be running for alerts, downloads and your other devices to work, so these decide how it stays around."
    >
      <Card>
        <ul>
          <ToggleRow
            title="Start Yoru when I sign in"
            hint={
              startup.isError
                ? (startup.error as Error).message
                : setStartup.isError
                  ? (setStartup.error as Error).message
                  : tray
                    ? 'Starts quietly in the tray, with no window.'
                    : 'Starts quietly in the background, with no window.'
            }
            on={startup.data?.on ?? false}
            disabled={!startup.data || setStartup.isPending}
            onChange={(v) => setStartup.mutate(v)}
          />
          {tray && (
            <ToggleRow
              title="When I close the window"
              hint={closes === 'tray' ? 'Downloads and your other devices keep going.' : 'Downloads and your other devices stop with it.'}
            >
              <Segmented
                label="When I close the window"
                value={closes}
                onChange={(v) => guide.set('window.on_close', v)}
                options={[
                  { value: 'tray', label: 'Keep in the tray' },
                  { value: 'exit', label: 'Exit Yoru' },
                ]}
              />
            </ToggleRow>
          )}
          <ToggleRow
            title="Tell me when a new episode can be played"
            hint={alertNote || "For shows you're watching. Yoru asks once to show notifications."}
            on={guide.on('notify.enabled')}
            onChange={(v) => void alerts(v)}
          />
          <ToggleRow
            title="Update automatically"
            hint={
              fromSource
                ? "A copy built from source doesn't update. Released versions do."
                : 'Downloads new versions as they come out. You choose when to restart.'
            }
            on={!fromSource && guide.on('update.auto')}
            disabled={fromSource}
            onChange={(v) => guide.set('update.auto', String(v))}
          />
        </ul>
      </Card>
      <Footer back={back}>
        <Continue label="Finish" onClick={next} />
      </Footer>
    </Frame>
  )
}
