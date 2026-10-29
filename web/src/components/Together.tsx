import { useEffect, useRef, useState } from 'react'
import { cx } from '../lib/format'
import { setOverlay, type Room, type RoomLine } from '../lib/together'
import { Button, Switch, TextInput } from './ui'

const REACTIONS = ['😂', '😮', '❤️', '👏']

/** The room beside the player: who's here, the invite, the host's settings, and the chat. */
export function TogetherPanel({
  room,
  invite,
  onLeave,
  overlay,
  onOverlay,
}: {
  room: Room
  invite?: string
  onLeave: () => void
  overlay: boolean
  onOverlay: (on: boolean) => void
}) {
  const [text, setText] = useState('')
  const [copied, setCopied] = useState(false)
  const feed = useRef<HTMLOListElement>(null)
  // Follows new lines unless scrolled up to read; by id, as the feed stops growing at 200.
  const atEnd = useRef(true)
  const last = room.lines[room.lines.length - 1]?.id
  useEffect(() => {
    if (atEnd.current) feed.current?.scrollTo({ top: feed.current.scrollHeight })
  }, [last])
  const view = room.view
  const host = !!room.me?.host
  if (room.status === 'pending') return <Notice>Waiting for the host to let you in…</Notice>
  if (room.status === 'refused') return <Notice>The host didn't let you in this time.</Notice>
  if (room.status === 'ended') return <Notice>The room has ended, or its Yoru can't be reached.</Notice>
  if (!view) return <Notice>Connecting to the room…</Notice>

  const say = () => {
    const t = text.trim()
    if (!t) return
    setText('')
    atEnd.current = true
    void room.act('chat', { text: t }).catch(() => {})
  }
  const status = (m: (typeof view.members)[number]) => (!m.connected ? 'Away' : m.ready ? 'Ready' : 'Loading')

  return (
    <section className="surface space-y-3 p-4" aria-label="Watch together">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-semibold text-white">
          Watch together <span className="font-normal text-base-500">· {view.members.length} here</span>
        </h2>
        <div className="flex items-center gap-2">
          {invite && (
            <Button
              size="sm"
              onClick={() => void navigator.clipboard.writeText(invite).then(() => setCopied(true), () => {})}
            >
              {copied ? 'Link copied' : 'Copy invite link'}
            </Button>
          )}
          <Button size="sm" onClick={onLeave}>
            Leave
          </Button>
        </div>
      </div>

      {room.waitingFor.length > 0 && view.state.waiting && (
        <p role="status" className="rounded-lg bg-amber-500/10 px-3 py-2 text-sm text-amber-200">
          Waiting for {room.waitingFor.length === 1 ? room.waitingFor[0] : `${room.waitingFor.length} people`}…
          {host && (
            <button onClick={() => void room.act('start')} className="ml-2 font-medium underline-offset-2 hover:underline">
              Start anyway
            </button>
          )}
        </p>
      )}

      {room.knocks.map((k) => (
        <div key={k.member} className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-base-850 px-3 py-2 text-sm">
          <span className="text-base-100">{k.name} wants to join</span>
          <span className="flex gap-2">
            <Button size="sm" variant="primary" onClick={() => room.answerKnock(k.member, true)}>
              Let in
            </Button>
            <Button size="sm" onClick={() => room.answerKnock(k.member, false)}>
              Not now
            </Button>
          </span>
        </div>
      ))}

      <ul className="flex flex-wrap gap-1.5">
        {view.members.map((m) => (
          <li key={m.id} className="flex items-center gap-1.5 rounded-full bg-base-850 px-2.5 py-1 text-xs">
            <span
              aria-hidden
              className={cx('size-1.5 rounded-full', !m.connected ? 'bg-base-600' : m.ready ? 'bg-emerald-400' : 'bg-amber-400')}
            />
            <span className="text-base-100">{m.name}</span>
            <span className="text-base-500">
              {m.host ? 'Host · ' : ''}
              {status(m)}
            </span>
          </li>
        ))}
      </ul>

      {host && (
        <div className="grid gap-2 sm:grid-cols-2">
          <label className="flex items-center justify-between gap-3 text-sm text-base-300">
            Ask before someone joins
            <Switch on={view.ask} onChange={(on) => void room.act('settings', { ask: on })} aria-label="Ask before someone joins" />
          </label>
          <label className="flex items-center justify-between gap-3 text-sm text-base-300">
            Wait for everyone
            <Switch on={view.wait} onChange={(on) => void room.act('settings', { wait: on })} aria-label="Wait for everyone" />
          </label>
        </div>
      )}

      <ol
        ref={feed}
        aria-label="Room messages"
        aria-live="polite"
        onScroll={(e) => {
          const el = e.currentTarget
          atEnd.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
        }}
        className="max-h-56 space-y-1 overflow-y-auto text-sm">
        {room.lines.length === 0 ? (
          <li className="text-base-500">Messages and what everyone does show here.</li>
        ) : (
          room.lines.map((l) => <Line key={l.id} line={l} />)
        )}
      </ol>

      <div className="flex items-center gap-2">
        <TextInput
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && say()}
          placeholder="Say something"
          aria-label="Message"
          maxLength={500}
          className="min-w-0 flex-1"
        />
        <Button size="sm" onClick={say} disabled={!text.trim()}>
          Send
        </Button>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="flex gap-1">
          {REACTIONS.map((r) => (
            <button
              key={r}
              onClick={() => void room.act('react', { text: r }).catch(() => {})}
              aria-label={`React ${r}`}
              className="tap relative grid size-9 place-items-center rounded-full text-lg transition-colors hover:bg-base-800"
            >
              {r}
            </button>
          ))}
        </span>
        <label className="flex items-center gap-2 text-xs text-base-400">
          Messages on screen
          <Switch
            on={overlay}
            onChange={(on) => {
              setOverlay(on)
              onOverlay(on)
            }}
            aria-label="Messages on screen"
          />
        </label>
      </div>
    </section>
  )
}

function Line({ line }: { line: RoomLine }) {
  if (line.kind === 'event') return <li className="text-xs text-base-500">{line.text}</li>
  return (
    <li className="text-base-200">
      <span className="font-medium text-base-100">{line.from}</span> {line.kind === 'react' ? <span className="text-lg">{line.text}</span> : line.text}
    </li>
  )
}

function Notice({ children }: { children: React.ReactNode }) {
  return <p className="surface p-4 text-sm text-base-300">{children}</p>
}

/** The name the room shows, asked before joining or opening one. */
export function JoinCard({
  title,
  name: suggested,
  host = false,
  onJoin,
  onCancel,
}: {
  title: string
  name: string
  host?: boolean
  onJoin: (name: string) => Promise<unknown>
  onCancel?: () => void
}) {
  const [name, setName] = useState(suggested)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const fallback = host ? 'Host' : 'Friend'
  const join = async () => {
    setBusy(true)
    setError('')
    try {
      await onJoin(name.trim() || fallback)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setBusy(false)
    }
  }
  return (
    <section className="surface space-y-3 p-4" aria-label={host ? 'Open a room' : 'Join the room'}>
      <h2 className="text-sm font-semibold text-white">Watch {title} together</h2>
      <form
        className="flex flex-wrap items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          void join()
        }}
      >
        <TextInput
          value={name}
          onChange={(e) => setName(e.target.value)}
          aria-label="Your name"
          placeholder="Your name"
          maxLength={32}
          className="w-48"
        />
        <Button type="submit" variant="primary" disabled={busy}>
          {host ? (busy ? 'Opening…' : 'Open room') : busy ? 'Joining…' : 'Join'}
        </Button>
        {onCancel && (
          <Button onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
        )}
      </form>
      <p className="text-xs text-base-500">How everyone in the room sees you. Left empty, you're {fallback}.</p>
      {error && <p className="text-sm text-recap">{error}</p>}
    </section>
  )
}

/** Over the picture, in fullscreen too: the newest message for a moment, one at a time, then gone. */
export function TogetherToasts({ room, on }: { room: Room; on: boolean }) {
  const [shown, setShown] = useState<RoomLine | null>(null)
  const [extra, setExtra] = useState(0)
  const seen = useRef(room.lines.length ? room.lines[room.lines.length - 1].id : 0)
  // One timer, replaced only by a new toast: a line not worth one mustn't leave the last up for good.
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  useEffect(() => {
    const fresh = room.lines.filter((l) => l.id > seen.current)
    if (fresh.length === 0) return
    seen.current = fresh[fresh.length - 1].id
    // Pauses and jumps matter to what's on screen; joins and leaves stay in the panel.
    const worth = fresh.filter((l) => l.kind !== 'event' || /paused|jumped/.test(l.text))
    if (!on || worth.length === 0) return
    const last = worth[worth.length - 1]
    setShown(last)
    setExtra(worth.length - 1)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setShown(null), last.kind === 'react' ? 1500 : 3000)
  }, [room.lines, on])

  const waiting = room.view?.state.waiting && room.waitingFor.length > 0
  return (
    <div aria-hidden className="pointer-events-none absolute bottom-20 left-4 z-10 flex max-w-[60%] flex-col items-start gap-1.5">
      {waiting && (
        <span className="rounded-full bg-base-950/75 px-3 py-1 text-xs text-amber-200 backdrop-blur-sm">
          Waiting for {room.waitingFor.length === 1 ? room.waitingFor[0] : `${room.waitingFor.length} people`}…
        </span>
      )}
      {on && shown && (
        <span className="animate-enter rounded-xl bg-base-950/70 px-3 py-1.5 text-sm text-white backdrop-blur-sm motion-reduce:animate-none">
          {shown.from && <span className="font-medium">{shown.from} </span>}
          {shown.kind === 'react' ? <span className="text-lg">{shown.text}</span> : shown.text}
          {extra > 0 && <span className="ml-2 text-xs text-white/60">+{extra} more</span>}
        </span>
      )}
    </div>
  )
}
