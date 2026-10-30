import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { BottomSheet } from '../components/BottomSheet'
import {
  CheckBox,
  CheckGlyph,
  ChevronGlyph,
  DownloadIcon,
  FreeRow,
  iconButton,
  PanelCard,
  Poster,
  SelectBar,
  ShelfTools,
  StorageCard,
} from '../components/DownloadsBits'
import { MoreMenu } from '../components/MoreMenu'
import { SavingJob } from '../components/SavingJob'
import { Button, ConfirmInline, LinkButton, ProgressBar, Switch } from '../components/ui'
import { bytes, cx } from '../lib/format'
import {
  cannotSave,
  knowsNetwork,
  removeSaved,
  saveEpisodes,
  savedKey,
  setSaveQuality,
  setSaveRule,
  tidySaved,
  useSaved,
  useWatched,
  watchedThrough,
  type SavedEpisode,
  type SaveQuality,
} from '../lib/offline'
import { useAccess, useHome } from '../lib/queries'
import { episodeCount, LOW_SHARE, sortShelf, useDeviceWord, useSort } from '../lib/shelf'
import { removeWithUndo, useHiddenKeys } from '../lib/undo'

const hideKey = (key: string) => `saved:${key}`

type Watched = ReturnType<typeof useWatched>

interface ShowShelf {
  animeId: number
  title: string
  cover?: string
  episodes: SavedEpisode[]
  bytes: number
  recent: number
  watched: number
}

function shelves(saved: SavedEpisode[], watched: Watched): ShowShelf[] {
  const shows = new Map<number, ShowShelf>()
  for (const e of saved) {
    const show = shows.get(e.animeId) ?? {
      animeId: e.animeId,
      title: e.title,
      cover: e.cover,
      episodes: [],
      bytes: 0,
      recent: 0,
      watched: 0,
    }
    show.episodes.push(e)
    show.bytes += e.bytes
    show.recent = Math.max(show.recent, e.savedAt)
    if (watchedThrough(e, watched) >= 0.9) show.watched++
    shows.set(e.animeId, show)
  }
  for (const show of shows.values()) show.episodes.sort((a, b) => a.episode - b.episode)
  return [...shows.values()]
}

function useVisibleSaves() {
  const all = useSaved()
  const hidden = useHiddenKeys()
  return {
    ...all,
    saved: all.saved.filter((e) => !hidden.has(hideKey(e.key))),
    jobs: all.jobs.filter((j) => !hidden.has(hideKey(savedKey(j.animeId, j.episode)))),
  }
}

function useRoomHere(deps: unknown, writing: boolean) {
  const [room, setRoom] = useState<{ usage: number; quota: number } | null>(null)
  useEffect(() => {
    const look = () =>
      void navigator.storage
        ?.estimate?.()
        .then((e) => setRoom(e.quota ? { usage: e.usage ?? 0, quota: e.quota } : null))
        .catch(() => {})
    look()
    const timer = writing ? window.setInterval(look, 15_000) : 0
    return () => window.clearInterval(timer)
  }, [deps, writing])
  return room
}

function typicalSize(saved: SavedEpisode[], quality: SaveQuality): number | null {
  const sizes = saved
    .filter((e) => e.quality === quality)
    .map((e) => e.bytes)
    .sort((a, b) => a - b)
  return sizes.length > 0 ? sizes[Math.floor(sizes.length / 2)] : null
}

function removeEpisodes(episodes: SavedEpisode[], word: string) {
  if (episodes.length === 0) return
  const one = episodes.length === 1 ? episodes[0] : null
  removeWithUndo({
    keys: episodes.map((e) => hideKey(e.key)),
    message: one
      ? `Removed ${one.title} episode ${one.episode} from this ${word}`
      : `Removed ${episodeCount(episodes.length)} from this ${word}`,
    failed: "Couldn't remove it",
    run: async () => {
      for (const e of episodes) await removeSaved(e.key)
    },
  })
}

export function DeviceList({ offline = false }: { offline?: boolean }) {
  const { saved, jobs, status, loaded } = useVisibleSaves()
  const watched = useWatched()
  const word = useDeviceWord()
  const why = cannotSave()
  const [sort, setSort] = useSort()
  const [open, setOpen] = useState<ReadonlySet<number>>(new Set())
  const [selecting, setSelecting] = useState(false)
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set())
  const [confirming, setConfirming] = useState(false)
  const suffix = offline ? '?offline=1' : ''

  useEffect(() => {
    if (loaded) void tidySaved()
  }, [loaded])

  const shows = sortShelf(shelves(saved, watched), sort, (s) => ({ recent: s.recent, size: s.bytes, name: s.title }))
  const chosen = saved.filter((e) => picked.has(e.key))
  const toggle = (keys: string[], on: boolean) =>
    setPicked((was) => {
      const next = new Set(was)
      for (const k of keys) {
        if (on) next.add(k)
        else next.delete(k)
      }
      return next
    })
  const stopSelecting = () => {
    setSelecting(false)
    setPicked(new Set())
    setConfirming(false)
  }
  const removeChosen = () => {
    const wholeShow = shows.some((s) => s.episodes.length > 1 && s.episodes.every((e) => picked.has(e.key)))
    if (!confirming && (chosen.length > 5 || wholeShow)) return setConfirming(true)
    removeEpisodes(chosen, word)
    stopSelecting()
  }
  let place = 0
  const places = new Map(
    jobs.map((j) => {
      const s = status.get(savedKey(j.animeId, j.episode))
      const counted = !j.paused && (!s || s.phase === 'queued')
      return [savedKey(j.animeId, j.episode), counted ? ++place : 0]
    }),
  )

  return (
    <div className="space-y-4">
      {why && !offline && <CantSave why={why} />}

      {jobs.length > 0 && (
        <section aria-label="Saving" className="surface px-4 pt-3 pb-1">
          <h2 className="text-sm font-semibold text-base-100">Saving</h2>
          <p className="text-[13px] text-base-400">Keep Yoru open while it saves. It carries on where it stopped.</p>
          <ul className="divide-y divide-white/[0.06]">
            {jobs.map((j) => {
              const key = savedKey(j.animeId, j.episode)
              return <SavingJob key={key} job={j} status={status.get(key)} place={places.get(key) ?? 0} />
            })}
          </ul>
        </section>
      )}

      {loaded && saved.length === 0 && jobs.length === 0 && <NothingSaved offline={offline} why={why} word={word} />}

      {saved.length > 0 && (
        <>
          <ShelfTools
            sort={sort}
            onSort={setSort}
            selecting={selecting}
            onSelecting={(on) => (on ? setSelecting(true) : stopSelecting())}
          />
          <ul className="space-y-2">
            {shows.map((show) => {
              const single = show.episodes.length === 1
              const keys = show.episodes.map((e) => e.key)
              const showOpen = single || open.has(show.animeId)
              const failed = jobs.some(
                (j) => j.animeId === show.animeId && status.get(savedKey(j.animeId, j.episode))?.phase === 'failed',
              )
              const saving = jobs.filter((j) => j.animeId === show.animeId).length
              return (
                <li key={show.animeId} className="surface overflow-hidden">
                  {single ? (
                    <SavedRow
                      episode={show.episodes[0]}
                      through={watchedThrough(show.episodes[0], watched)}
                      to={`/saved/${show.episodes[0].key}${suffix}`}
                      word={word}
                      select={selecting ? { checked: picked.has(show.episodes[0].key), toggle: (on) => toggle(keys, on) } : undefined}
                    />
                  ) : (
                    <>
                      <ShowRow
                        show={show}
                        open={showOpen}
                        onToggle={() =>
                          setOpen((was) => {
                            const next = new Set(was)
                            if (!next.delete(show.animeId)) next.add(show.animeId)
                            return next
                          })
                        }
                        state={failed ? 'trouble' : saving > 0 ? `${saving} more saving` : 'saved'}
                        word={word}
                        onRemoveAll={() => removeEpisodes(show.episodes, word)}
                        select={
                          selecting
                            ? { checked: keys.every((k) => picked.has(k)), toggle: (on) => toggle(keys, on) }
                            : undefined
                        }
                      />
                      {showOpen && (
                        <ul>
                          {show.episodes.map((e) => (
                            <li key={e.key}>
                              <SavedRow
                                episode={e}
                                through={watchedThrough(e, watched)}
                                to={`/saved/${e.key}${suffix}`}
                                word={word}
                                nested
                                select={selecting ? { checked: picked.has(e.key), toggle: (on) => toggle([e.key], on) } : undefined}
                              />
                            </li>
                          ))}
                        </ul>
                      )}
                    </>
                  )}
                </li>
              )
            })}
          </ul>
          {selecting && (
            <SelectBar
              count={chosen.length}
              size={chosen.reduce((n, e) => n + e.bytes, 0)}
              confirming={confirming}
              onRemove={removeChosen}
              onConfirm={() => {
                removeEpisodes(chosen, word)
                stopSelecting()
              }}
              onKeep={() => setConfirming(false)}
              onCancel={stopSelecting}
            />
          )}
        </>
      )}
    </div>
  )
}

interface Selectable {
  checked: boolean
  toggle: (on: boolean) => void
}

function ShowRow({
  show,
  open,
  onToggle,
  state,
  word,
  onRemoveAll,
  select,
}: {
  show: ShowShelf
  open: boolean
  onToggle: () => void
  state: string
  word: string
  onRemoveAll: () => void
  select?: Selectable
}) {
  const [confirm, setConfirm] = useState(false)
  const facts = [episodeCount(show.episodes.length), bytes(show.bytes), show.watched > 0 && `${show.watched} watched`]
    .filter(Boolean)
    .join(' · ')
  return (
    <>
      <div className="flex items-center gap-2 p-3">
        {select && (
          <CheckBox checked={select.checked} onChange={select.toggle} label={`Select every episode of ${show.title}`} />
        )}
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          aria-label={`${open ? 'Hide' : 'Show'} episodes of ${show.title}`}
          className="flex min-w-0 flex-1 items-center gap-3 text-left"
        >
          <Poster src={show.cover} />
          <span className="min-w-0 flex-1">
            <span className="block truncate font-semibold text-base-100">{show.title}</span>
            <span className="block text-sm text-base-400 tabular-nums">{facts}</span>
          </span>
          {state === 'saved' ? (
            <span className="flex shrink-0 items-center gap-1 text-[13px] font-medium text-emerald-300">
              <CheckGlyph className="size-3.5" />
              Saved
            </span>
          ) : state === 'trouble' ? (
            <span className="flex shrink-0 items-center gap-1.5 text-[13px] font-medium text-amber-300">
              <span aria-hidden className="size-2 rounded-full bg-amber-400" />
              Needs attention
            </span>
          ) : (
            <span className="shrink-0 text-[13px] text-base-400">{state}</span>
          )}
          <span className="text-base-500">
            <ChevronGlyph open={open} />
          </span>
        </button>
        {!select && (
          <MoreMenu
            label={`More for ${show.title}`}
            title={show.title}
            detail={`${episodeCount(show.episodes.length)} · ${bytes(show.bytes)}`}
            className={iconButton}
            items={[{ label: `Remove all from this ${word}`, danger: true, run: () => setConfirm(true) }]}
          />
        )}
      </div>
      {confirm && (
        <div className="flex flex-wrap items-center justify-end gap-2 px-3 pb-3">
          <ConfirmInline
            size="sm"
            label={`Remove ${episodeCount(show.episodes.length)}`}
            note={`From this ${word} only`}
            onConfirm={() => {
              setConfirm(false)
              onRemoveAll()
            }}
            onCancel={() => setConfirm(false)}
          />
        </div>
      )}
    </>
  )
}

function SavedRow({
  episode: e,
  through,
  to,
  word,
  nested = false,
  select,
}: {
  episode: SavedEpisode
  through: number
  to: string
  word: string
  nested?: boolean
  select?: Selectable
}) {
  const done = through >= 0.9
  const facts = [done ? 'Watched' : through > 0 && `${Math.round(through * 100)}% watched`, bytes(e.bytes), e.quality === '720' && '720p']
    .filter(Boolean)
    .join(' · ')
  return (
    <div className={cx('flex items-center gap-2', nested ? 'border-t border-white/[0.06] py-2 pr-3 pl-4' : 'p-3')}>
      {select && <CheckBox checked={select.checked} onChange={select.toggle} label={`Select ${e.title} ${e.label}`} />}
      <Link to={to} className="flex min-w-0 flex-1 items-center gap-3 py-1">
        {!nested && <Poster src={e.cover} />}
        <span className="min-w-0 flex-1">
          {!nested && <span className="block truncate font-semibold text-base-100">{e.title}</span>}
          <span className="block truncate text-sm text-base-100">{e.label}</span>
          <span className="block text-[13px] text-base-400 tabular-nums">{facts}</span>
          {through > 0 && !done && <ProgressBar value={through * 100} className="mt-1.5 max-w-48" label="Watched" />}
        </span>
      </Link>
      {!select && (
        <>
          <LinkButton to={to} size="sm" variant="primary">
            Play
          </LinkButton>
          <MoreMenu
            label={`More for ${e.title} episode ${e.episode}`}
            title={e.title}
            detail={e.label}
            className={iconButton}
            items={[
              { label: 'Play', to },
              { label: `Remove from this ${word}`, danger: true, run: () => removeEpisodes([e], word) },
            ]}
          />
        </>
      )}
    </div>
  )
}

function NothingSaved({ offline, why, word }: { offline: boolean; why: string | null; word: string }) {
  return (
    <div className="space-y-4">
      <section className="surface flex flex-col items-center px-6 py-8 text-center">
        <span className="mb-4 grid size-14 place-items-center rounded-2xl bg-base-800 text-base-300">
          <DownloadIcon />
        </span>
        <p className="text-[17px] font-semibold text-base-100">Nothing saved yet</p>
        <p className="mt-1 max-w-sm text-[15px] text-balance text-base-400">
          Save episodes to watch with the PC off. Subtitles come too.
        </p>
      </section>
      {!offline && <ReadyToSave why={why} word={word} />}
      {!offline && (
        <LinkButton to="/browse" variant="primary" size="lg" className="w-full">
          Browse shows
        </LinkButton>
      )}
    </div>
  )
}

function ReadyToSave({ why, word }: { why: string | null; word: string }) {
  const home = useHome()
  const { saved, jobs, quality } = useSaved()
  const have = new Set([...saved.map((e) => e.key), ...jobs.map((j) => savedKey(j.animeId, j.episode))])
  const size = typicalSize(saved, quality)
  const ready = (home.data?.items ?? [])
    .map((item) => ({ item, episode: item.resume?.episode ?? item.progress + 1 }))
    .filter(
      ({ item, episode }) =>
        !have.has(savedKey(item.id, episode)) &&
        (item.episodes == null || episode <= item.episodes) &&
        (item.nextEpisode == null || episode < item.nextEpisode),
    )
    .slice(0, 5)
  if (ready.length === 0) return null
  return (
    <section aria-label="Ready to save" className="space-y-2">
      <h2 className="px-1 text-[13px] font-semibold text-base-300">Ready to save</h2>
      <ul className="surface divide-y divide-white/[0.06] px-3">
        {ready.map(({ item, episode }) => (
          <li key={item.id} className="flex min-h-16 items-center gap-3 py-2">
            <Poster src={item.cover} small />
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm text-base-100">
                {item.title} · Episode {episode}
              </p>
              <p className="text-[13px] text-base-400 tabular-nums">
                {why ? `Needs Yoru's secure address on this ${word}` : size ? `about ${bytes(size)}` : 'From Up next'}
              </p>
            </div>
            <Button
              size="sm"
              disabled={!!why}
              onClick={() => saveEpisodes([{ animeId: item.id, episode }])}
              aria-label={`Save episode ${episode} of ${item.title}`}
            >
              Save
            </Button>
          </li>
        ))}
      </ul>
    </section>
  )
}

export function DeviceStorage() {
  const { saved, jobs, status } = useVisibleSaves()
  const watched = useWatched()
  const word = useDeviceWord()
  const [freeing, setFreeing] = useState(false)
  const room = useRoomHere(
    `${saved.length}-${jobs.length}-${status.size}`,
    [...status.values()].some((s) => s.phase === 'saving'),
  )
  if (cannotSave()) return null
  const used = saved.reduce((n, e) => n + e.bytes, 0)
  const free = room ? Math.max(0, room.quota - room.usage) : null
  const low = !!room && free! / room.quota < LOW_SHARE
  const watchedBytes = saved.filter((e) => watchedThrough(e, watched) >= 0.9).reduce((n, e) => n + e.bytes, 0)
  const line = low
    ? watchedBytes > 0
      ? `Space is running low. Remove watched episodes to free ${bytes(watchedBytes)}.`
      : `Space is running low on this ${word}.`
    : [used > 0 ? `Saved ${bytes(used)}` : 'Nothing saved', free != null && `about ${bytes(free)} free on this ${word}`]
        .filter(Boolean)
        .join(' · ')
  return (
    <>
      <StorageCard
        used={used}
        other={room ? Math.max(0, room.usage - used) : 0}
        free={free}
        total={room?.quota}
        line={line}
        low={low}
        onFree={saved.length > 0 ? () => setFreeing(true) : undefined}
      />
      {freeing && (
        <BottomSheet title="Free up space" detail={`On this ${word}`} onClose={() => setFreeing(false)}>
          <FreeUpList word={word} onDone={() => setFreeing(false)} />
        </BottomSheet>
      )}
    </>
  )
}

export function DeviceFreeUp() {
  const { saved } = useVisibleSaves()
  const word = useDeviceWord()
  if (saved.length === 0 || cannotSave()) return null
  return (
    <PanelCard title="Free up space" className="max-lg:hidden">
      <FreeUpList word={word} />
    </PanelCard>
  )
}

function FreeUpList({ word, onDone }: { word: string; onDone?: () => void }) {
  const { saved } = useVisibleSaves()
  const watched = useWatched()
  const [confirm, setConfirm] = useState<number | null>(null)
  const seen = saved.filter((e) => watchedThrough(e, watched) >= 0.9)
  const shows = shelves(saved, watched).sort((a, b) => b.bytes - a.bytes)
  return (
    <ul className="divide-y divide-white/[0.06] px-3">
      <FreeRow
        title="Remove watched episodes"
        detail={seen.length > 0 ? `${episodeCount(seen.length)} · ${bytes(seen.reduce((n, e) => n + e.bytes, 0))}` : 'Nothing watched yet'}
        action={
          <Button
            size="sm"
            disabled={seen.length === 0}
            onClick={() => {
              removeEpisodes(seen, word)
              onDone?.()
            }}
          >
            Remove
          </Button>
        }
      />
      {shows.map((s) => (
        <FreeRow
          key={s.animeId}
          title={s.title}
          detail={`${episodeCount(s.episodes.length)} · ${bytes(s.bytes)}`}
          action={
            confirm === s.animeId ? (
              <ConfirmInline
                size="sm"
                label={`Remove ${episodeCount(s.episodes.length)}`}
                onConfirm={() => {
                  setConfirm(null)
                  removeEpisodes(s.episodes, word)
                }}
                onCancel={() => setConfirm(null)}
              />
            ) : (
              <Button size="sm" onClick={() => setConfirm(s.animeId)} aria-label={`Remove ${s.title} from this ${word}`}>
                Remove
              </Button>
            )
          }
        />
      ))}
    </ul>
  )
}

export function DeviceSettings() {
  const { saved, quality, rules } = useSaved()
  const word = useDeviceWord()
  const network = knowsNetwork()
  const best = typicalSize(saved, 'original')
  const small = typicalSize(saved, '720')
  const choices: Array<{ value: SaveQuality; title: string; detail: string }> = [
    { value: 'original', title: 'Best quality', detail: ['Same as on the PC', best && `about ${bytes(best)} an episode`].filter(Boolean).join(' · ') },
    { value: '720', title: 'Smaller', detail: ['720p', small && `about ${bytes(small)} an episode`, 'saves faster'].filter(Boolean).join(' · ') },
  ]
  return (
    <PanelCard title="Save settings">
      <div role="radiogroup" aria-label="Quality for new saves" className="space-y-2">
        <p className="text-[13px] text-base-400">Quality for new saves</p>
        {choices.map((c) => {
          const on = quality === c.value
          return (
            <button
              key={c.value}
              type="button"
              role="radio"
              aria-checked={on}
              onClick={() => setSaveQuality(c.value)}
              className={cx(
                'press flex min-h-16 w-full items-center gap-3 rounded-xl px-3 py-2 text-left transition-colors',
                on ? 'bg-accent-500/15' : 'bg-base-800 hover:bg-base-750',
              )}
            >
              <span
                aria-hidden
                className={cx('grid size-5 shrink-0 place-items-center rounded-full ring-2 ring-inset', on ? 'ring-accent-400' : 'ring-base-600')}
              >
                {on && <span className="size-2.5 rounded-full bg-accent-400" />}
              </span>
              <span className="min-w-0">
                <span className={cx('block text-sm font-medium', on ? 'text-accent-300' : 'text-base-100')}>{c.title}</span>
                <span className="block text-[13px] text-base-400">{c.detail}</span>
              </span>
            </button>
          )
        })}
      </div>
      <div className="space-y-1">
        <p className="pt-1 text-[13px] text-base-400">Auto tidy</p>
        <RuleRow label="Save the next episode when I finish one" hint="For shows saved here, while the PC can be reached." on={rules.next} onChange={(v) => setSaveRule('next', v)} />
        <RuleRow label="Remove episodes a day after I watch them" on={rules.tidy} onChange={(v) => setSaveRule('tidy', v)} />
        <RuleRow
          label="Save only on Wi-Fi"
          hint={network ? undefined : `This ${word} doesn't say which network it's on`}
          on={network && rules.wifi}
          disabled={!network}
          onChange={(v) => setSaveRule('wifi', v)}
        />
      </div>
    </PanelCard>
  )
}

function RuleRow({
  label,
  hint,
  on,
  disabled,
  onChange,
}: {
  label: string
  hint?: string
  on: boolean
  disabled?: boolean
  onChange: (on: boolean) => void
}) {
  return (
    <div className="flex min-h-13 items-center justify-between gap-3 py-1.5">
      <div className="min-w-0">
        <p className={cx('text-sm', disabled ? 'text-base-600' : 'text-base-100')}>{label}</p>
        {hint && <p className="text-[13px] text-base-400">{hint}</p>}
      </div>
      <Switch on={on} onChange={onChange} disabled={disabled} aria-label={label} />
    </div>
  )
}

function CantSave({ why }: { why: string }) {
  const access = useAccess()
  const secure = access.data?.urls.find((u) => u.startsWith('https://'))
  return (
    <div className="surface space-y-2 p-4 text-sm">
      <p className="text-base-100">{why}</p>
      <p className="text-base-400">
        {secure
          ? 'Open Yoru by its secure address on this device, and save from there.'
          : 'Set up HTTPS on the PC under Settings, Devices, then open Yoru by that address here.'}
      </p>
      {secure && (
        <a href={secure} className="inline-block text-accent-300 underline-offset-2 hover:underline">
          Open the secure address
        </a>
      )}
    </div>
  )
}
