import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api } from '../lib/api'
import { copyText } from '../lib/downloads'
import { cx } from '../lib/format'
import { useAccess } from '../lib/queries'
import { SCREENS, STEPS, nextClass, screenOf, stepPath, type Guide, type Screen, type StepId, useGuide } from '../lib/setupGuide'
import { buttonClass } from '../lib/ui'
import { Card, Footer, Frame, Tick } from '../components/SetupBits'
import { ErrorState, Skeleton, Switch } from '../components/ui'
import { Account, Computer, Essentials, Sources, Storage, Watching, type StepProps } from './SetupSteps'

const STEP_VIEW: Record<StepId, (props: StepProps) => ReactNode> = {
  essentials: Essentials,
  sources: Sources,
  account: Account,
  watching: Watching,
  storage: Storage,
  computer: Computer,
}

const ICONS: Record<StepId | 'phone', ReactNode> = {
  essentials: <path d="M12 4v11m-4.5-4.5L12 15l4.5-4.5M5 19h14" />,
  sources: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M3.5 12h17M12 3.5c2.5 2.6 2.5 14.4 0 17M12 3.5c-2.5 2.6-2.5 14.4 0 17" />
    </>
  ),
  account: (
    <>
      <circle cx="12" cy="8.5" r="3.6" />
      <path d="M5 19.5c1.2-3.4 3.8-5 7-5s5.8 1.6 7 5" />
    </>
  ),
  watching: <path d="M8.5 6.2v11.6l9.5-5.8z" />,
  storage: <path d="M3.5 8A1.5 1.5 0 0 1 5 6.5h4l2 2h8A1.5 1.5 0 0 1 20.5 10v7.5A1.5 1.5 0 0 1 19 19H5a1.5 1.5 0 0 1-1.5-1.5z" />,
  computer: (
    <>
      <rect x="3.5" y="5" width="17" height="11" rx="1.8" />
      <path d="M9 19.5h6M12 16v3.5" />
    </>
  ),
  phone: (
    <>
      <rect x="7" y="3" width="10" height="18" rx="2.2" />
      <path d="M11 18h2" />
    </>
  ),
}

function Chip({ icon, warn }: { icon: StepId | 'phone'; warn?: boolean }) {
  return (
    <span aria-hidden className={cx('grid size-11 shrink-0 place-items-center rounded-full', warn ? 'bg-amber-400/15 text-amber-300' : 'bg-accent-500/15 text-accent-300')}>
      <svg viewBox="0 0 24 24" className="size-[22px]" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
        {ICONS[icon]}
      </svg>
    </span>
  )
}

function Logo() {
  return (
    <span className="flex items-baseline gap-1">
      <span className="font-display text-3xl font-bold tracking-tight text-white">Yoru</span>
      <span aria-hidden className="size-[7px] rounded-full bg-accent-500" />
    </span>
  )
}

function caption(screen: Screen) {
  if (screen === 'welcome') return 'Six steps · about 2 minutes'
  if (screen === 'done') return 'Setup finished'
  return `Step ${SCREENS.indexOf(screen)} of ${STEPS.length}`
}

function Rail({ screen, guide }: { screen: Screen; guide: Guide }) {
  return (
    <nav aria-label="Setup steps" className="sticky top-0 hidden h-dvh w-80 shrink-0 flex-col overflow-y-auto border-r border-base-850 bg-base-900/50 px-8 pt-9 pb-6 lg:flex">
      <Logo />
      <div className="my-auto flex flex-col gap-[22px] py-8">
        <p className={cx('text-[13px] font-medium', screen === 'done' ? 'text-emerald-300' : 'text-base-400')}>{caption(screen)}</p>
        <ol className="flex flex-col">
          {STEPS.map((step, i) => {
            const note = guide.notes[step.id]
            const now = screen === step.id
            const last = i === STEPS.length - 1
            return (
              <li key={step.id}>
                <Link
                  to={stepPath(step.id)}
                  aria-current={now ? 'step' : undefined}
                  className="group flex gap-3.5 rounded-lg focus:outline-hidden focus-visible:ring-2 focus-visible:ring-accent-400"
                >
                  <span className="flex flex-none flex-col items-center">
                    <span
                      className={cx(
                        'grid size-[30px] place-items-center rounded-full text-[13px] transition-colors',
                        now
                          ? 'bg-accent-500 font-semibold text-white ring-4 ring-accent-500/25'
                          : note.done
                            ? 'bg-emerald-400/15 text-emerald-300'
                            : 'border-[1.5px] border-base-700 text-base-400 group-hover:border-base-500',
                      )}
                    >
                      {note.done && !now ? <Tick label="Done" /> : i + 1}
                    </span>
                    {!last && <span aria-hidden className={cx('my-1.5 w-0.5 flex-1 rounded-full', note.done ? 'bg-emerald-400/40' : 'bg-base-800')} />}
                  </span>
                  <span className={cx('flex min-w-0 flex-col gap-0.5', !last && 'pb-[26px]')}>
                    <span className={cx('text-sm transition-colors', now ? 'font-semibold text-white' : 'font-medium text-base-200 group-hover:text-white')}>
                      {step.title}
                    </span>
                    <span className="truncate text-xs text-base-400">{note.sub}</span>
                  </span>
                </Link>
              </li>
            )
          })}
        </ol>
      </div>
      {screen !== 'done' && screen !== 'welcome' && screen !== 'computer' ? (
        <Link to={stepPath('done')} className="py-3 text-[13px] text-base-400 transition-colors hover:text-white">
          Skip the rest of setup
        </Link>
      ) : (
        <span aria-hidden className="h-10" />
      )}
    </nav>
  )
}

function TopSteps({ screen, guide }: { screen: Screen; guide: Guide }) {
  return (
    <nav aria-label="Setup steps" className="flex flex-col gap-3 border-b border-base-850 px-5 pt-5 pb-4 sm:px-8 lg:hidden">
      <div className="flex items-baseline justify-between gap-3">
        <Logo />
        <p className={cx('text-[13px] font-medium', screen === 'done' ? 'text-emerald-300' : 'text-base-400')}>{caption(screen)}</p>
      </div>
      <ol className="flex items-center">
        {STEPS.map((step, i) => {
          const note = guide.notes[step.id]
          const now = screen === step.id
          return (
            <li key={step.id} className={cx('flex items-center', i > 0 && 'flex-1')}>
              {i > 0 && <span aria-hidden className={cx('h-0.5 flex-1 rounded-full', guide.notes[STEPS[i - 1].id].done ? 'bg-emerald-400/40' : 'bg-base-800')} />}
              <Link
                to={stepPath(step.id)}
                aria-current={now ? 'step' : undefined}
                aria-label={`Step ${i + 1}: ${step.title}${note.done ? ', done' : ''}`}
                className="grid size-11 place-items-center"
              >
                <span
                  className={cx(
                    'grid size-[30px] place-items-center rounded-full text-[13px]',
                    now
                      ? 'bg-accent-500 font-semibold text-white ring-4 ring-accent-500/25'
                      : note.done
                        ? 'bg-emerald-400/15 text-emerald-300'
                        : 'border-[1.5px] border-base-700 text-base-400',
                  )}
                >
                  {note.done && !now ? <Tick /> : i + 1}
                </span>
              </Link>
            </li>
          )
        })}
      </ol>
    </nav>
  )
}

const POINTS: ReadonlyArray<{ icon: StepId | 'phone'; title: string; text: string }> = [
  { icon: 'watching', title: 'Plays while it downloads', text: 'Pick an episode and it starts, with no waiting for a whole file.' },
  { icon: 'account', title: 'Keeps your list', text: 'Progress goes to AniList or MyAnimeList, and comes back from them.' },
  { icon: 'phone', title: 'Phone and TV too', text: 'Watch from other devices on your network once this computer is set.' },
]

function Welcome() {
  return (
    <div className="my-auto flex flex-col gap-7 py-10 lg:pl-4">
      <div className="flex max-w-[620px] flex-col gap-3.5">
        <h1 tabIndex={-1} className="font-display text-4xl leading-[1.05] font-bold tracking-tight text-white outline-none sm:text-[56px]">
          Welcome to Yoru
        </h1>
        <p className="text-lg leading-normal text-base-300">
          Six short steps and you're watching. Each one already has a sensible choice picked, and everything can be changed later in Settings.
        </p>
      </div>
      <ul className="grid max-w-[760px] gap-3.5 sm:grid-cols-3">
        {POINTS.map((point) => (
          <li key={point.title} className="flex flex-col gap-2.5 rounded-2xl border border-base-800 bg-base-900 p-[18px]">
            <Chip icon={point.icon} />
            <span className="text-[15px] font-semibold text-white">{point.title}</span>
            <span className="text-[13px] leading-snug text-base-400">{point.text}</span>
          </li>
        ))}
      </ul>
      <div className="flex flex-wrap items-center gap-4">
        <Link to={stepPath('essentials')} className={cx(nextClass, 'h-[52px] rounded-[14px] px-7 text-base')}>
          Start setup
          <svg viewBox="0 0 24 24" aria-hidden className="size-[18px]">
            <path d="M5 12h14M13 6l6 6-6 6" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </Link>
        <Link to="/" className="inline-flex h-11 items-center px-2 text-sm text-base-400 transition-colors hover:text-white">
          Skip for now
        </Link>
      </div>
    </div>
  )
}

function PhoneCard() {
  const qc = useQueryClient()
  const access = useAccess().data
  const allow = useMutation({
    meta: { inline: true },
    mutationFn: (lan: boolean) => api.post('/api/access/network', { lan }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['access'] }),
  })
  const [copied, setCopied] = useState('')
  if (!access) return null
  const url = access.reachable ? access.urls[0] : undefined
  const copy = async (text: string) => {
    setCopied((await copyText(text)) ? 'Copied' : "Couldn't copy")
    window.setTimeout(() => setCopied(''), 1500)
  }
  return (
    <Card label="Other devices" className="flex flex-wrap items-center gap-x-5 gap-y-3 px-5 py-4 sm:px-6">
      {url ? (
        <img src={`/api/access/qr.svg?url=${encodeURIComponent(url)}`} alt="Pairing QR code" className="size-24 shrink-0 rounded-xl bg-white p-1.5" />
      ) : (
        <Chip icon="phone" />
      )}
      <div className="min-w-0 flex-1 basis-64">
        <h2 className="text-base font-semibold text-white">Watch on your phone or TV too</h2>
        <p className="mt-1 text-sm leading-snug text-base-400">
          {url
            ? "Scan this with your phone's camera while it's on the same Wi-Fi."
            : access.reachable
              ? 'This computer has no home-network address right now.'
              : access.canSwitch
                ? 'Off for now: only this computer can open Yoru. Turn it on and a code to scan appears here.'
                : 'Only this computer can open Yoru right now.'}
        </p>
        {allow.isError && <p className="mt-1 text-xs text-recap">{(allow.error as Error).message}</p>}
      </div>
      {url && (
        <button type="button" onClick={() => void copy(url)} className={buttonClass()}>
          {copied || 'Copy the link'}
        </button>
      )}
      {access.canSwitch && (
        <span className="flex items-center gap-3 text-sm font-medium text-base-200">
          Allow other devices
          <Switch on={access.reachable} onChange={(v) => allow.mutate(v)} disabled={allow.isPending} aria-label="Allow other devices" />
        </span>
      )}
    </Card>
  )
}

function Done({ guide }: { guide: Guide }) {
  return (
    <Frame title="You're all set" lead="Everything here can be changed later in Settings.">
      <ul aria-label="What you chose" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        {STEPS.map((step) => {
          const note = guide.notes[step.id]
          return (
            <li key={step.id}>
              <Link
                to={stepPath(step.id)}
                className={cx(
                  'flex h-full min-h-[84px] items-center gap-3.5 rounded-2xl border px-4 py-3 transition-colors focus:outline-hidden focus-visible:ring-2 focus-visible:ring-accent-400',
                  note.warn ? 'border-amber-400/40 bg-amber-400/5 hover:border-amber-400/70' : 'border-base-800 bg-base-900 hover:border-base-600',
                )}
              >
                <Chip icon={step.id} warn={!!note.warn} />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="text-xs font-medium text-base-400">{step.title}</span>
                  <span className="truncate text-[15px] font-semibold text-white">{note.value}</span>
                  <span className={cx('text-xs', note.warn ? 'text-amber-200' : 'truncate text-base-400')}>{note.warn ?? note.detail}</span>
                </span>
                {note.warn && <span className="shrink-0 rounded-[10px] bg-amber-400/15 px-3 py-2 text-[13px] font-semibold text-amber-100">Fix</span>}
              </Link>
            </li>
          )
        })}
      </ul>
      <PhoneCard />
      <Footer back="computer">
        <Link to="/" className={nextClass}>
          Start watching
          <svg viewBox="0 0 24 24" aria-hidden className="size-4" fill="currentColor">
            <path d="M8 5.5v13a1 1 0 0 0 1.5.860l11-6.5a1 1 0 0 0 0-1.720l-11-6.5A1 1 0 0 0 8 5.5z" />
          </svg>
        </Link>
      </Footer>
    </Frame>
  )
}

export function SetupPage() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const guide = useGuide()
  const screen = screenOf(params.get('step'))
  const main = useRef<HTMLDivElement>(null)
  const first = useRef(true)

  useEffect(() => {
    if (first.current) {
      first.current = false
      return
    }
    window.scrollTo(0, 0)
    main.current?.querySelector<HTMLElement>('h1')?.focus({ preventScroll: true })
  }, [screen])

  if (guide.error) return <ErrorState error={guide.error} retry={guide.retry} />
  if (guide.pending || !guide.setup) return <Skeleton className="m-6 h-96" />
  if (!guide.host) {
    return (
      <div className="mx-auto flex min-h-dvh max-w-md flex-col items-start justify-center gap-4 p-6">
        <Logo />
        <h1 className="font-display text-2xl font-bold text-white">Setup happens on the computer</h1>
        <p className="text-sm text-base-300">Finish setting Yoru up on the computer it runs on. This device can watch once that is done.</p>
        <Link to="/" className={nextClass}>
          Open Yoru
        </Link>
      </div>
    )
  }

  const at = SCREENS.indexOf(screen)
  const View = screen !== 'welcome' && screen !== 'done' ? STEP_VIEW[screen] : null
  return (
    <div className="relative flex min-h-dvh bg-base-950">
      {screen === 'welcome' && (
        <div aria-hidden className="pointer-events-none absolute inset-0 overflow-hidden">
          <div className="absolute -top-64 left-1/3 h-[620px] w-[900px] rounded-full bg-[radial-gradient(closest-side,rgb(106_85_245/0.28),transparent)]" />
        </div>
      )}
      <Rail screen={screen} guide={guide} />
      <div className="relative flex min-w-0 flex-1 flex-col">
        <TopSteps screen={screen} guide={guide} />
        <div ref={main} className="mx-auto flex w-full max-w-[1000px] flex-1 flex-col gap-[22px] px-5 pt-[clamp(1.75rem,8vh,7.5rem)] sm:px-8 lg:px-[72px]">
          {screen === 'welcome' && <Welcome />}
          {screen === 'done' && <Done guide={guide} />}
          {View && screen !== 'welcome' && screen !== 'done' && (
            <View
              key={screen}
              guide={guide}
              setup={guide.setup}
              back={SCREENS[at - 1]}
              skip={SCREENS[at + 1]}
              next={() => {
                guide.pass(screen)
                navigate(stepPath(SCREENS[at + 1]))
              }}
            />
          )}
        </div>
      </div>
    </div>
  )
}
