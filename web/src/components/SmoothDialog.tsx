import { useCallback } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import { bytes, cx } from '../lib/format'
import { useDismiss, useModalFocus } from '../lib/hooks'
import {
  rateFps,
  timesEpisode,
  useKnownScreenHz,
  useKnownSourceHz,
  useRunningFps,
  type DeviceSmooth,
  type SmoothFiles,
  type SmoothRate,
  type SmoothState,
} from '../player/smooth'
import { formatFps, useScreenHz } from '../player/rates'
import { Segmented } from './ui'

const DEVICE_CHOICES: ReadonlyArray<{ value: DeviceSmooth; label: string }> = [
  { value: 'auto', label: 'Auto' },
  { value: 'off', label: 'Off' },
  { value: 'on', label: 'On' },
]

const DEVICE_HINTS: Record<DeviceSmooth, string> = {
  auto: 'Follows Settings; off on phones.',
  off: 'Never on this device.',
  on: 'Always on this device.',
}

function rateChoices(screenHz: number, sourceHz: number): Array<{ value: SmoothRate; label: string }> {
  const seen = new Set<number>()
  const fixed: Array<{ value: SmoothRate; label: string }> = []
  for (const tier of ['high', 'mid', 'low'] as const) {
    const fps = Math.round(rateFps(tier, screenHz, sourceHz))
    if (seen.has(fps)) continue
    seen.add(fps)
    fixed.push({ value: tier, label: String(fps) })
  }
  return [{ value: 'auto', label: 'Auto' }, ...fixed.reverse()]
}

function shownRate(rate: SmoothRate, screenHz: number, sourceHz: number): SmoothRate {
  if (rate === 'auto') return rate
  const fps = String(Math.round(rateFps(rate, screenHz, sourceHz)))
  return rateChoices(screenHz, sourceHz).find((c) => c.value !== 'auto' && c.label === fps)?.value ?? rate
}

function rateHint(rate: SmoothRate, screenHz: number, sourceHz: number, runningFps: number): string {
  const fps = Math.round(rateFps(rate, screenHz, sourceHz))
  if (rate === 'auto' && runningFps && runningFps < fps) return `${runningFps} fps for now, so this device keeps up.`
  if (rate === 'auto') return `${fps} fps, the best fit for this screen.`
  return `${fps} fps, ${timesEpisode(fps, sourceHz)}× the episode's frame rate.`
}

function FilesRow() {
  const status = useQuery({
    queryKey: ['smooth-files'],
    queryFn: () => api.get<SmoothFiles>('/api/smooth'),
    staleTime: 0,
    refetchInterval: (q) => {
      const stage = q.state.data?.progress?.stage
      return stage && stage !== 'done' && stage !== 'failed' ? 1000 : false
    },
  })
  const install = useMutation({
    mutationFn: () => api.post('/api/setup/install/flyhigh'),
    onSettled: () => void status.refetch(),
  })
  const s = status.data
  if (status.isError) {
    return <p className="text-xs text-amber-300">Couldn't ask the PC whether smooth motion is installed.</p>
  }
  if (!s) return null
  if (s.ready) return <p className="text-xs text-base-500">Installed on the PC: FlyHigh {s.version}.</p>

  const p = s.progress
  const busy = install.isPending || (!!p && p.stage !== 'done' && p.stage !== 'failed')
  const outdated = !!s.version && s.version !== s.wanted
  return (
    <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-amber-500/40 bg-amber-500/5 px-3 py-2.5">
      <p className="min-w-0 flex-1 basis-48 text-xs leading-snug text-base-300">
        {busy
          ? `Downloading FlyHigh ${s.wanted} to the PC${p?.total ? `: ${bytes(p.bytes)} of ${bytes(p.total)}` : '…'}`
          : p?.stage === 'failed'
            ? `The download failed: ${p.error ?? 'unknown error'}`
            : outdated
              ? `The PC has FlyHigh ${s.version}; this Yoru needs ${s.wanted} (about 2 MB).`
              : 'Needs FlyHigh on the PC first, about 2 MB.'}
        {!s.canInstall && !busy && ' Add it on the PC, from this menu or Settings › About › Programs Yoru uses.'}
      </p>
      {s.canInstall && (
        <button
          onClick={() => install.mutate()}
          disabled={busy}
          className="tap rounded-md bg-accent-500 px-3 py-1.5 text-sm font-medium text-white hover:bg-accent-400 disabled:opacity-60"
        >
          {busy ? 'Downloading' : p?.stage === 'failed' ? 'Try again' : outdated ? 'Update' : 'Download'}
        </button>
      )}
    </div>
  )
}

function SwitchCard({
  label,
  hint,
  on,
  onChange,
  dim = false,
}: {
  label: string
  hint: string
  on: boolean
  onChange: (on: boolean) => void
  dim?: boolean
}) {
  return (
    <button
      role="switch"
      aria-checked={on}
      aria-label={label}
      onClick={() => onChange(!on)}
      className={cx(
        'flex w-full items-center justify-between gap-3 rounded-lg border px-3 py-2.5 text-left text-sm transition-colors',
        on ? 'border-accent-500/50 bg-accent-500/10 text-white' : 'border-base-800 bg-base-950/40 text-base-300 hover:bg-base-850',
        dim && 'opacity-60',
      )}
    >
      <span className="min-w-0">
        <span className="block font-medium">{label}</span>
        <span className="mt-0.5 block text-xs leading-snug text-base-500">{hint}</span>
      </span>
      <span className={cx('relative h-5 w-9 shrink-0 rounded-full transition-colors', on ? 'bg-accent-500' : 'bg-base-700')}>
        <span className={cx('absolute top-0.5 size-4 rounded-full bg-white transition-all', on ? 'left-[1.125rem]' : 'left-0.5')} />
      </span>
    </button>
  )
}

export function SmoothDialog({
  open,
  on,
  held,
  stopped,
  onClose,
  onChange,
  onHeld,
  rate,
  onRate,
  device,
  onDevice,
  anime4k,
  episodeFps,
  running,
}: {
  open: boolean
  on: boolean
  held: boolean
  stopped: boolean
  onClose: () => void
  onChange: (on: boolean) => void
  onHeld: (on: boolean) => void
  rate: SmoothRate
  onRate: (rate: SmoothRate) => void
  device: DeviceSmooth
  onDevice: (choice: DeviceSmooth) => void
  anime4k: string | null
  episodeFps: number
  running: SmoothState
}) {
  const close = useCallback(() => onClose(), [onClose])
  const panel = useDismiss<HTMLDivElement>(close, open)
  useModalFocus(panel, open)
  const known = useKnownScreenHz()
  const measured = useScreenHz(open && !known)
  const screenHz = known || measured
  const playing = useKnownSourceHz()
  const runningFps = useRunningFps()
  const sourceHz = playing || episodeFps

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />

      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label="Smooth motion"
        className="animate-fade-in relative max-h-[85vh] w-full max-w-lg overflow-y-auto rounded-xl border border-base-800 bg-base-900 p-4 shadow-2xl scrollbar-thin"
      >
        <div className="mb-3 flex items-start justify-between gap-4">
          <div>
            <h2 className="text-base font-semibold text-white">Smooth motion</h2>
            <p className="mt-0.5 text-xs text-base-500">
              For this episode only. Your saved defaults in Settings › Sound and picture are not changed.
            </p>
          </div>
          <button
            onClick={close}
            aria-label="Close"
            className="tap relative grid size-9 shrink-0 place-items-center rounded-md text-base-400 hover:bg-base-800 hover:text-base-100"
          >
            ✕
          </button>
        </div>

        <div className="space-y-2">
          <FilesRow />
          <SwitchCard
            label={on && !stopped ? 'On' : 'Off'}
            hint={
              stopped && on
                ? 'Off for this episode: this device fell behind.'
                : on && running === 'unsupported'
                  ? 'This browser has no WebGPU.'
                  : on && running === 'failed'
                    ? "Couldn't start on this device."
                    : on && running === 'off'
                      ? 'Paused in picture-in-picture or AirPlay.'
                      : 'Smoother motion, made on this device.'
            }
            on={on && !stopped}
            onChange={onChange}
          />
          <div
            className={cx(
              'flex flex-wrap items-center justify-between gap-2 rounded-lg border border-base-850 px-3 py-2.5',
              !on && 'opacity-60',
            )}
          >
            <div className="min-w-0 flex-1 basis-48">
              <p className="text-sm font-medium text-base-200">Frame rate</p>
              <p className="mt-0.5 text-xs leading-snug text-base-500">{rateHint(rate, screenHz, sourceHz, runningFps)}</p>
            </div>
            <Segmented
              size="sm"
              label="Frame rate"
              value={shownRate(rate, screenHz, sourceHz)}
              onChange={onRate}
              options={rateChoices(screenHz, sourceHz)}
            />
          </div>
          <p className="px-1 text-xs text-base-500">
            {[episodeFps ? `Episode: ${formatFps(episodeFps)} fps` : '', screenHz ? `Screen: ${screenHz} Hz` : '']
              .filter(Boolean)
              .join(' · ')}
          </p>
          <SwitchCard
            label="Smooth held drawings"
            hint="Smooths characters too, not just the camera. Fast moves can look blurry."
            on={held}
            onChange={onHeld}
            dim={!on}
          />
        </div>

        <div className="mt-3 flex flex-wrap items-center justify-between gap-2 rounded-lg border border-base-850 px-3 py-2.5">
          <div className="min-w-0 flex-1 basis-40">
            <p className="text-sm text-base-200">On this device</p>
            <p className="text-xs text-base-500">{DEVICE_HINTS[device]}</p>
          </div>
          <Segmented size="sm" label="On this device" value={device} onChange={onDevice} options={DEVICE_CHOICES} />
        </div>

        <div className="mt-3 space-y-1.5 text-xs leading-snug text-base-500">
          {anime4k && <p>Anime4K ({anime4k}) runs too, before the frames are added.</p>}
          <p>Sound is delayed slightly to stay in sync.</p>
          <p>Slow in another browser on a laptop? Set it to High performance in Windows' graphics settings.</p>
          <p className="text-base-600">Frame generation by FlyHigh Engine (Apache 2.0); model under CC BY-NC 4.0.</p>
        </div>
      </div>
    </div>
  )
}
