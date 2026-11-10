import { useEffect, useRef, useState } from 'react'
import { useMatch, useNavigate } from 'react-router-dom'
import { cx } from '../lib/format'
import { useDismiss } from '../lib/hooks'
import { useDockDrag, useMiniCorner, type Corner } from '../lib/miniplayer'
import { useRoomSession } from '../lib/roomSession'
import { memberState, memberStatus, overlayOn, useRoomToasts } from '../lib/together'
import { Line, RoomAvatar, RoomToast } from './Together'
import { Button, TextInput } from './ui'

const REACTIONS = ['😂', '😮', '❤️', '👏']

const PLACE: Record<Corner, string> = {
  tl: 'top-20 left-4',
  tr: 'top-20 right-4',
  bl: 'bottom-4 left-4 max-sm:bottom-[calc(4.5rem+env(safe-area-inset-bottom))]',
  br: 'right-4 bottom-32 max-sm:bottom-[calc(10.5rem+env(safe-area-inset-bottom))]',
}

const PAST_MINI: Record<Corner, string> = {
  tl: 'top-[21.5rem] left-4 max-sm:top-[10rem]',
  tr: 'top-[21.5rem] right-4 max-sm:top-[10rem]',
  bl: 'bottom-[18rem] left-4 max-sm:bottom-[calc(10.5rem+env(safe-area-inset-bottom))]',
  br: 'right-4 bottom-[23.5rem] max-sm:bottom-[calc(15.5rem+env(safe-area-inset-bottom))]',
}

export function RoomBubble() {
  const { room, inRoom, together, leave } = useRoomSession()
  const match = useMatch('/watch/:animeId/:episode')
  const navigate = useNavigate()
  const miniCorner = useMiniCorner()
  const { corner, ref: dockRef, onPointerDownCapture, onClickCapture } = useDockDrag(inRoom, 'yoru.together.corner', 'tl')
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const card = useDismiss<HTMLDivElement>(() => setOpen(false), open)

  const view = room.view
  const state = view?.state
  const onRoomPage =
    !!match && !!state && Number(match.params.animeId) === state.animeId && Number(match.params.episode) === state.episode
  const visible = inRoom && !!view && !!state && !!together && !onRoomPage
  const toasts = useRoomToasts(room, visible && !open && overlayOn())
  const lastId = room.lines.length ? room.lines[room.lines.length - 1].id : 0
  const [seenId, setSeenId] = useState(0)
  if ((open || !visible) && seenId !== lastId) setSeenId(lastId)
  const feed = useRef<HTMLOListElement>(null)
  useEffect(() => {
    if (open && feed.current) feed.current.scrollTop = feed.current.scrollHeight
  }, [open, lastId])
  if (!visible) return null
  const mine = room.me?.id
  const own = (l: { member?: string; from?: string }) => (l.member ? l.member === mine : l.from === room.me?.name)
  const unread = room.lines.filter((l) => l.id > seenId && l.kind === 'chat' && !own(l)).length
  const others = view.members.filter((m) => m.id !== mine)
  const faces = (others.length ? others : view.members).slice(0, others.length > 2 ? 1 : 2)
  const more = Math.max(0, others.length - faces.length)

  const left = corner.endsWith('l')
  const top = corner.startsWith('t')
  const people = view.members
  const say = () => {
    const t = text.trim()
    if (!t) return
    setText('')
    void room.act('chat', { text: t }).catch(() => {})
  }
  const openRoom = () => {
    setOpen(false)
    navigate(`/watch/${state.animeId}/${state.episode}?together=${encodeURIComponent(together.href)}`)
  }

  return (
    <div
      ref={dockRef}
      className={cx(
        'fixed z-40 flex gap-2',
        left ? 'flex-row' : 'flex-row-reverse',
        top ? 'items-start' : 'items-end',
        (miniCorner === corner ? PAST_MINI : PLACE)[corner],
      )}
    >
      <div ref={card} className="relative shrink-0">
        <div onPointerDownCapture={onPointerDownCapture} onClickCapture={onClickCapture}>
          <button
            type="button"
            onClick={() => setOpen((o) => !o)}
            aria-expanded={open}
            aria-label={`Watch together room, ${people.length} watching${unread ? `, ${unread} new ${unread === 1 ? 'message' : 'messages'}` : ''}`}
            title={`Watch together · ${people.length} watching`}
            className="tap relative grid size-12 cursor-grab touch-none place-items-center rounded-full bg-base-850 shadow-xl ring-1 shadow-black/60 ring-white/15 select-none hover:bg-base-800 active:cursor-grabbing"
          >
            <span className="flex -space-x-3">
              {faces.map((m) => (
                <span key={m.id} className="rounded-full ring-2 ring-base-850">
                  <RoomAvatar name={m.name} src={room.avatarOf(m.id)} />
                </span>
              ))}
              {more > 0 && (
                <span className="grid size-8 place-items-center rounded-full bg-base-700 text-xs font-semibold text-base-100 ring-2 ring-base-850">
                  +{more}
                </span>
              )}
            </span>
            {unread > 0 && (
              <span className="absolute -top-1 -right-1 grid h-5 min-w-5 place-items-center rounded-full bg-accent-500 px-1 text-[11px] font-semibold text-white ring-2 ring-base-950">
                {unread > 9 ? '9+' : unread}
              </span>
            )}
          </button>
        </div>

        {open && (
          <section
            aria-label="Watch together room"
            className={cx(
              'absolute z-10 w-80 max-w-[calc(100vw-2rem)] animate-rise overflow-hidden rounded-xl bg-base-900 shadow-2xl ring-1 shadow-black/60 ring-line',
              top ? 'top-full mt-2' : 'bottom-full mb-2',
              left ? 'left-0' : 'right-0',
            )}
          >
            <header className="border-b border-line px-4 py-3">
              <h2 className="text-sm font-semibold text-white">Watch together</h2>
              <p className="truncate text-xs text-base-400" title={view.title}>
                {[view.title, `Episode ${state.episode}`].filter(Boolean).join(' · ')}
              </p>
            </header>
            <ul className="border-b border-line px-4 py-2">
              {people.map((m) => (
                <li key={m.id} className="flex min-h-10 items-center gap-2.5">
                  <RoomAvatar name={m.name} src={room.avatarOf(m.id)} state={memberState(m)} />
                  <span className="min-w-0 flex-1 truncate text-sm text-base-100">
                    {m.name}
                    {m.id === room.me?.id && <span className="text-base-500"> (you)</span>}
                  </span>
                  <span className="text-xs text-base-400">{memberStatus(m)}</span>
                </li>
              ))}
            </ul>
            {room.lines.length > 0 && (
              <ol
                ref={feed}
                aria-label="Room messages"
                className="max-h-48 space-y-1.5 overflow-y-auto overscroll-contain border-b border-line px-4 py-3 text-sm scrollbar-thin"
              >
                {room.lines.slice(-30).map((l) => (
                  <Line key={l.id} line={l} mine={l.from === room.me?.name} src={room.avatarOf(l.member)} />
                ))}
              </ol>
            )}
            <div className="space-y-2 border-b border-line px-4 py-3">
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
            <div className="flex items-center justify-between gap-2 px-4 py-3">
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setOpen(false)
                  leave()
                }}
                className="text-red-300 hover:text-red-200"
              >
                Leave room
              </Button>
              <Button size="sm" variant="primary" onClick={openRoom}>
                Open room
              </Button>
            </div>
          </section>
        )}
      </div>

      {toasts.length > 0 && (
        <div aria-hidden className={cx('pointer-events-none flex w-72 max-w-[60vw] flex-col gap-1.5', left ? 'items-start' : 'items-end')}>
          {toasts.map((line) => (
            <RoomToast key={line.id} room={room} line={line} />
          ))}
        </div>
      )}
    </div>
  )
}
