import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { Button, Centered, Empty, Spinner } from '../components/ui'
import { bytes } from '../lib/format'
import { SUB_LOOKS } from '../player/subLook'
import { noteWatched, resumeAt, useSaved, useWatched, type SavedEpisode } from '../lib/offline'
import { Player } from '../player/Player'

export function SavedWatch({ offline = false }: { offline?: boolean }) {
  const { key } = useParams()
  const navigate = useNavigate()
  const { saved, loaded } = useSaved()
  const watched = useWatched()
  const e = saved.find((s) => s.key === key)
  const suffix = offline ? '?offline=1' : ''
  // Once per episode: the place moves as it plays.
  const [start, setStart] = useState<{ key: string; at: number } | null>(null)
  if (e && start?.key !== e.key) setStart({ key: e.key, at: resumeAt(e, watched) })

  const later = saved.filter((s) => s.animeId === e?.animeId && s.episode !== e?.episode)
  const next = later.filter((s) => s.episode > (e?.episode ?? 0)).sort((a, b) => a.episode - b.episode)[0]
  const previous = later.filter((s) => s.episode < (e?.episode ?? 0)).sort((a, b) => b.episode - a.episode)[0]
  const go = (to: SavedEpisode) => navigate(`/saved/${to.key}${suffix}`)

  const look = useMemo(() => (e ? lookFrom(e) : null), [e])
  const [ended, setEnded] = useState<string | null>(null)

  if (!loaded) {
    return (
      <Centered>
        <Spinner />
      </Centered>
    )
  }
  if (!e || !look || !start) {
    return <Empty title="This episode isn't saved on this device" action={<Link to={`/saved${suffix}`}>On this device</Link>} />
  }

  return (
    <div className="mx-auto w-full max-w-[1600px] px-0 sm:px-6 sm:py-4">
      <div className="relative bg-black sm:rounded-xl">
        <Player
          key={e.key}
          stream={e.stream}
          startAt={start.at}
          skips={e.skips}
          autoSkip={look.autoSkip}
          autoPlay={look.autoPlay}
          subLanguages={look.subLanguages}
          withDub={look.withDub}
          subLook={look.subLook}
          title={e.title}
          subtitle={e.label}
          artwork={e.cover}
          onProgress={(position, duration, played, final) => noteWatched(e, position, duration, played, final)}
          onEnded={() => setEnded(e.key)}
          onResume={() => setEnded(null)}
          onNext={next ? () => go(next) : undefined}
          onPrevious={previous ? () => go(previous) : undefined}
          overlay={
            ended === e.key && next && look.autoNext ? (
              <NextUp next={next} onGo={() => go(next)} onCancel={() => setEnded(null)} />
            ) : undefined
          }
        />
      </div>
      <div className="space-y-1 px-3 py-3 sm:px-0">
        <Link to={`/saved${suffix}`} className="text-sm text-accent-300 underline-offset-2 hover:underline">
          ‹ On this device
        </Link>
        <h1 className="text-lg font-semibold text-white">{e.title}</h1>
        <p className="text-sm text-base-300">{e.label}</p>
        <p className="text-xs text-base-500">
          Saved on this device · {bytes(e.bytes)} · {e.quality === '720' ? '720p' : 'Original quality'}
        </p>
      </div>
    </div>
  )
}

// As the watch page reads them.
function lookFrom(e: SavedEpisode) {
  const p = e.prefs
  const flag = (k: string) => p[k] === 'true'
  let subLanguages = ['en']
  try {
    const parsed = JSON.parse(p['subtitle.languages'] || '["en"]') as unknown
    if (Array.isArray(parsed) && parsed.length > 0) subLanguages = parsed as string[]
  } catch {
    // English.
  }
  return {
    subLanguages,
    withDub: (['signs', 'off', 'same'] as const).find((v) => v === p['subtitle.with_dub']) ?? 'signs',
    subLook: SUB_LOOKS.find((l) => l.value === p['subtitle.look'])?.value ?? '',
    autoPlay: flag('playback.autoplay'),
    autoNext: flag('playback.autonext'),
    autoSkip: {
      op: flag('playback.autoskip_op') && !(e.bookend && p['playback.skip_keep_bookends'] !== 'false'),
      ed: flag('playback.autoskip_ed') && !(e.bookend && p['playback.skip_keep_bookends'] !== 'false'),
      recap: flag('playback.autoskip_recap'),
      preview: flag('playback.autoskip_preview'),
    },
  }
}

function NextUp({ next, onGo, onCancel }: { next: SavedEpisode; onGo: () => void; onCancel: () => void }) {
  const [left, setLeft] = useState(5)
  useEffect(() => {
    if (left <= 0) {
      onGo()
      return
    }
    const t = window.setTimeout(() => setLeft((n) => n - 1), 1000)
    return () => window.clearTimeout(t)
  }, [left, onGo])
  return (
    <div className="absolute right-4 bottom-20 z-20 flex flex-wrap items-center gap-2 rounded-xl bg-base-900/90 p-3 ring-1 ring-white/10">
      <p className="text-sm text-base-100">
        {next.label} in {left} s
      </p>
      <Button size="sm" variant="primary" onClick={onGo}>
        Play now
      </Button>
      <Button size="sm" onClick={onCancel}>
        Cancel
      </Button>
    </div>
  )
}
