import { useEffect, useRef, useState } from 'react'
import { cx } from '../lib/format'
import { setOverlay, type Room, type RoomLine } from '../lib/together'
import { Badge, Button, Switch, TextInput } from './ui'

const REACTIONS = ['😂', '😮', '❤️', '👏']

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
  const waiting = room.waitingFor.length > 0 && view.state.waiting
  const copy = (link: string) => void navigator.clipboard.writeText(link).then(() => setCopied(true), () => {})

  return (
    <section className="surface overflow-hidden" aria-label="Watch together">
      <header className="flex items-center justify-between gap-3 border-b border-line px-4 py-3">
        <div className="min-w-0">
          <h2 className="text-sm font-semibold text-white">Watch together</h2>
          <p className="text-xs text-base-400">
            {view.members.length} watching{host ? ' · you host' : ''}
          </p>
        </div>
        <Button size="sm" variant="ghost" onClick={onLeave} className="text-red-300 hover:text-red-200">
          Leave room
        </Button>
      </header>

      {(waiting || room.knocks.length > 0) && (
        <div className="space-y-2 border-b border-line px-4 py-3">
          {waiting && (
            <div role="status" className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-amber-500/10 px-3 py-2 text-sm text-amber-200">
              <span>Waiting for {room.waitingFor.length === 1 ? room.waitingFor[0] : `${room.waitingFor.length} people`} to load</span>
              {host && (
                <Button size="sm" variant="secondary" onClick={() => void room.act('start')}>
                  Start anyway
                </Button>
              )}
            </div>
          )}
          {room.knocks.map((k) => (
            <div key={k.member} className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-accent-500/10 px-3 py-2 text-sm">
              <span className="flex items-center gap-2.5 text-base-100">
                <Avatar name={k.name} />
                {k.name} wants to join
              </span>
              <span className="flex gap-2">
                <Button size="sm" onClick={() => room.answerKnock(k.member, false)}>
                  Not now
                </Button>
                <Button size="sm" variant="primary" onClick={() => room.answerKnock(k.member, true)}>
                  Let in
                </Button>
              </span>
            </div>
          ))}
        </div>
      )}

      <div className="grid md:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
        <div className="space-y-5 px-4 py-4 md:border-r md:border-line">
          {invite && (
            <div className="space-y-2">
              <h3 className="text-xs font-medium text-base-400">Invite friends</h3>
              <div className="flex items-center gap-2 rounded-xl bg-base-850 py-1.5 pr-1.5 pl-3">
                <span className="min-w-0 flex-1 truncate font-mono text-xs text-base-300" title={invite}>
                  {invite.replace(/^https?:\/\//, '')}
                </span>
                <Button size="sm" variant={copied ? 'secondary' : 'primary'} onClick={() => copy(invite)}>
                  {copied ? 'Copied' : 'Copy link'}
                </Button>
              </div>
              <p className="text-xs text-base-500">Anyone with this link can join from their own Yoru.</p>
            </div>
          )}

          <div className="space-y-1">
            <h3 className="text-xs font-medium text-base-400">In the room</h3>
            <ul>
              {view.members.map((m) => (
                <li key={m.id} className="flex min-h-11 items-center gap-3">
                  <Avatar name={m.name} state={!m.connected ? 'away' : m.ready ? 'ready' : 'loading'} />
                  <span className="min-w-0 flex-1 truncate text-sm text-base-100">
                    {m.name}
                    {m.id === room.me?.id && <span className="text-base-500"> (you)</span>}
                  </span>
                  {m.host && <Badge tone="accent">Host</Badge>}
                  <span className={cx('w-14 text-right text-xs', !m.connected ? 'text-base-500' : m.ready ? 'text-emerald-300' : 'text-amber-300')}>
                    {status(m)}
                  </span>
                </li>
              ))}
            </ul>
          </div>

          <div className="space-y-1">
            <h3 className="text-xs font-medium text-base-400">Settings</h3>
            <div className="divide-y divide-line">
              {host && (
                <>
                  <SettingRow
                    label="Ask before someone joins"
                    hint="You let each person in."
                    on={view.ask}
                    onChange={(on) => void room.act('settings', { ask: on })}
                  />
                  <SettingRow
                    label="Wait for everyone"
                    hint="Play holds while anyone is still loading."
                    on={view.wait}
                    onChange={(on) => void room.act('settings', { wait: on })}
                  />
                </>
              )}
              <SettingRow
                label="Messages on screen"
                hint="Chat and reactions over the video, on this device."
                on={overlay}
                onChange={(on) => {
                  setOverlay(on)
                  onOverlay(on)
                }}
              />
            </div>
          </div>
        </div>

        <div className="flex min-h-52 flex-col border-t md:min-h-72 border-line md:border-t-0">
          <ol
            ref={feed}
            aria-label="Room messages"
            aria-live="polite"
            onScroll={(e) => {
              const el = e.currentTarget
              atEnd.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
            }}
            className="max-h-80 min-h-0 flex-1 space-y-1.5 overflow-y-auto px-4 py-4 text-sm scrollbar-thin"
          >
            {room.lines.length === 0 ? (
              <li className="grid h-full place-items-center py-8 text-center text-xs text-base-500">
                No messages yet. Pauses and jumps show up here too.
              </li>
            ) : (
              room.lines.map((l) => <Line key={l.id} line={l} mine={l.from === room.me?.name} />)
            )}
          </ol>

          <div className="space-y-2 border-t border-line px-4 py-3">
            <div className="flex gap-1" role="group" aria-label="Reactions">
              {REACTIONS.map((r) => (
                <button
                  key={r}
                  type="button"
                  onClick={() => void room.act('react', { text: r }).catch(() => {})}
                  aria-label={`React ${r}`}
                  className="tap relative grid size-9 place-items-center rounded-full bg-base-850 text-lg transition-colors hover:bg-base-800 pointer-coarse:size-11"
                >
                  {r}
                </button>
              ))}
            </div>
            <form
              className="flex items-center gap-2"
              onSubmit={(e) => {
                e.preventDefault()
                say()
              }}
            >
              <TextInput
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder="Message the room"
                aria-label="Message"
                maxLength={500}
                className="min-w-0 flex-1"
              />
              <Button type="submit" variant="primary" disabled={!text.trim()}>
                Send
              </Button>
            </form>
          </div>
        </div>
      </div>
    </section>
  )
}

function SettingRow({ label, hint, on, onChange }: { label: string; hint: string; on: boolean; onChange: (on: boolean) => void }) {
  return (
    <label className="flex min-h-14 cursor-pointer items-center justify-between gap-4 py-2.5">
      <span className="min-w-0">
        <span className="block text-sm text-base-100">{label}</span>
        <span className="block text-xs text-base-500">{hint}</span>
      </span>
      <Switch on={on} onChange={onChange} aria-label={label} />
    </label>
  )
}

function Avatar({ name, state }: { name: string; state?: 'ready' | 'loading' | 'away' }) {
  return (
    <span aria-hidden className="relative grid size-8 shrink-0 place-items-center rounded-full bg-base-700 text-xs font-semibold text-base-100 uppercase">
      {[...name.trim()][0] ?? '?'}
      {state && (
        <span
          className={cx(
            'absolute -right-0.5 -bottom-0.5 size-3 rounded-full ring-2 ring-base-900',
            state === 'away' ? 'bg-base-500' : state === 'ready' ? 'bg-emerald-400' : 'bg-amber-400',
          )}
        />
      )}
    </span>
  )
}

function Line({ line, mine }: { line: RoomLine; mine: boolean }) {
  if (line.kind === 'event') return <li className="text-center text-xs text-base-500">{line.text}</li>
  return (
    <li className="flex items-start gap-2.5">
      <Avatar name={line.from ?? '?'} />
      <span className="min-w-0 pt-1">
        <span className={cx('mr-1.5 text-xs font-semibold', mine ? 'text-accent-300' : 'text-base-300')}>{line.from}</span>
        {line.kind === 'react' ? <span className="text-xl leading-none">{line.text}</span> : <span className="break-words text-base-100">{line.text}</span>}
      </span>
    </li>
  )
}

function Notice({ children }: { children: React.ReactNode }) {
  return <p className="surface p-4 text-sm text-base-300">{children}</p>
}

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
    <section className="surface space-y-4 p-4" aria-label={host ? 'Open a room' : 'Join the room'}>
      <div>
        <h2 className="text-sm font-semibold text-white">Watch {title} together</h2>
        <p className="mt-0.5 text-xs text-base-400">
          {host
            ? 'Friends with their own Yoru join from a link, and everyone plays, pauses and jumps in step.'
            : 'You are joining a room. Everyone plays, pauses and jumps in step.'}
        </p>
      </div>
      <form
        className="space-y-1.5"
        onSubmit={(e) => {
          e.preventDefault()
          void join()
        }}
      >
        <label htmlFor="together-name" className="block text-xs font-medium text-base-300">
          Your name
        </label>
        <div className="flex flex-wrap items-center gap-2">
          <TextInput
            id="together-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={fallback}
            maxLength={32}
            autoFocus
            className="min-w-0 flex-1 sm:max-w-xs"
          />
          <span className="flex gap-2">
            {onCancel && (
              <Button variant="ghost" onClick={onCancel} disabled={busy}>
                Cancel
              </Button>
            )}
            <Button type="submit" variant="primary" disabled={busy}>
              {host ? (busy ? 'Opening…' : 'Open room') : busy ? 'Joining…' : 'Join'}
            </Button>
          </span>
        </div>
        <p className="text-xs text-base-500">How everyone in the room sees you. Left empty, you're {fallback}.</p>
      </form>
      {error && <p className="text-sm text-recap">{error}</p>}
    </section>
  )
}

export function TogetherToasts({ room, on }: { room: Room; on: boolean }) {
  const [shown, setShown] = useState<RoomLine | null>(null)
  const [extra, setExtra] = useState(0)
  const seen = useRef(room.lines.length ? room.lines[room.lines.length - 1].id : 0)
  const timer = useRef(0)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  useEffect(() => {
    const fresh = room.lines.filter((l) => l.id > seen.current)
    if (fresh.length === 0) return
    seen.current = fresh[fresh.length - 1].id
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
