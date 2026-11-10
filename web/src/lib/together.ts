import { useCallback, useEffect, useRef, useState } from 'react'
import { readKey, writeKey } from './storage'

export interface TogetherLink {
  base: string
  code: string
  key: string
  href: string
}

export function parseTogether(text: string): TogetherLink | null {
  const m = /(https?:\/\/[^\s/]+)\/together\/([a-z0-9]{6})\?k=([A-Za-z0-9_-]+)/.exec(text)
  return m ? { base: m[1], code: m[2], key: m[3], href: m[0] } : null
}

export interface RoomState {
  playing: boolean
  position: number
  at: number
  animeId: number
  episode: number
  infoHash?: string
  audio?: 'sub' | 'dub'
  waiting: boolean
}

export interface RoomPick {
  id: number
  member: string
  by: string
  animeId: number
  episode: number
  title: string
}

export interface RoomMember {
  id: string
  name: string
  host?: boolean
  ready: boolean
  connected: boolean
  browsing?: boolean
  avatar?: string
}

export interface RoomView {
  code: string
  title: string
  ask: boolean
  wait: boolean
  anyone?: boolean
  picks?: RoomPick[]
  state: RoomState
  members: RoomMember[]
}


export interface RoomLine {
  id: number
  kind: 'chat' | 'react' | 'event'
  from?: string
  member?: string
  text: string
}

export interface Seat {
  member: string
  token: string
  invite?: string
}

const api = (link: TogetherLink, path: string, init?: RequestInit) =>
  fetch(`${link.base}/api/together/${link.code}${path}${path.includes('?') ? '&' : '?'}k=${encodeURIComponent(link.key)}`, init)

async function answer<T>(res: Response): Promise<T> {
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error((body as { error?: string }).error ?? `The room answered ${res.status}`)
  return body as T
}

export const roomInfo = async (link: TogetherLink) => answer<RoomView>(await api(link, ''))

export async function joinRoom(link: TogetherLink, name: string, avatar?: string): Promise<Seat & { admitted: boolean }> {
  return answer(
    await api(link, '/join', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name, avatar }) }),
  )
}

const seatKey = (link: TogetherLink) => `yoru.together.${link.code}`

export function savedSeat(link: TogetherLink): Seat | null {
  try {
    return JSON.parse(sessionStorage.getItem(seatKey(link)) ?? 'null') as Seat | null
  } catch {
    return null
  }
}

export function saveSeat(link: TogetherLink, seat: Seat | null) {
  try {
    if (seat) sessionStorage.setItem(seatKey(link), JSON.stringify(seat))
    else sessionStorage.removeItem(seatKey(link))
  } catch {
    return
  }
}

const OVERLAY_KEY = 'yoru.together.overlay'

export function overlayOn(): boolean {
  return readKey(OVERLAY_KEY) !== 'off'
}

export function setOverlay(on: boolean) {
  writeKey(OVERLAY_KEY, on ? 'on' : 'off')
}

export type RoomStatus ='connecting' | 'pending' | 'in' | 'refused' | 'ended'

interface Change {
  by: string
  title: string
  state: RoomState
}

const QUIET_MS = 35_000
const ACT_MS = 10_000

const REASONS: Record<string, (c: Change) => string> = {
  pause: (c) => `${c.by} paused`,
  play: (c) => `${c.by} played`,
  seek: (c) => `${c.by} jumped`,
  episode: (c) => `${c.by} switched to episode ${c.state.episode}`,
  show: (c) => `${c.by} switched to ${c.title || 'another show'}, episode ${c.state.episode}`,
  audio: (c) => `${c.by} switched everyone to ${c.state.audio === 'dub' ? 'the English dub' : 'Japanese audio'}`,
}

export function useRoom(link: TogetherLink | null, seat: Seat | null) {
  const [view, setView] = useState<RoomView | null>(null)
  const [status, setStatus] = useState<RoomStatus>('connecting')
  const [lines, setLines] = useState<RoomLine[]>([])
  const [knocks, setKnocks] = useState<Array<{ member: string; name: string }>>([])
  const [waitingFor, setWaitingFor] = useState<string[]>([])
  const offset = useRef(0)
  const lineId = useRef(0)
  const push = useCallback((line: Omit<RoomLine, 'id'>) => {
    setLines((was) => [...was.slice(-199), { ...line, id: ++lineId.current }])
  }, [])

  useEffect(() => {
    if (!link || !seat) return
    const url = `${link.base.replace(/^http/, 'ws')}/api/together/${link.code}/socket?k=${encodeURIComponent(link.key)}&m=${seat.member}&t=${encodeURIComponent(seat.token)}`
    let socket: WebSocket | null = null
    let stopped = false
    let retry: ReturnType<typeof setTimeout> | undefined
    let wait = 1000
    let heard = false
    let heardAt = Date.now()
    let silent = 0
    const onMessage = (e: MessageEvent) => {
      heard = true
      heardAt = Date.now()
      silent = 0
      wait = 1000
      const event = JSON.parse(e.data as string) as Record<string, unknown>
      if (typeof event.now === 'number') offset.current = event.now - Date.now()
      switch (event.type) {
        case 'tick':
          if (socket?.readyState === WebSocket.OPEN) socket.send('1')
          break
        case 'pending':
          setStatus('pending')
          break
        case 'refused':
          setStatus('refused')
          stopped = true
          socket?.close()
          break
        case 'room':
          setView(event as unknown as RoomView)
          setStatus('in')
          break
        case 'state': {
          setView((was) => (was ? { ...was, state: event.state as RoomState } : was))
          setWaitingFor((event.waitingFor as string[] | undefined) ?? [])
          const says = REASONS[event.reason as string]
          if (says && event.by) {
            push({ kind: 'event', text: says({ by: event.by as string, title: (event.title as string) ?? '', state: event.state as RoomState }) })
          }
          break
        }
        case 'suggest': {
          const pick = event.pick as RoomPick
          push({ kind: 'event', text: `${pick.by} suggests ${pick.title}, episode ${pick.episode}` })
          break
        }
        case 'chat':
        case 'react':
          push({ kind: event.type, from: event.from as string, member: event.member as string | undefined, text: event.text as string })
          break
        case 'knock':
          setKnocks((was) => [...was, { member: event.member as string, name: event.name as string }])
          break
      }
    }
    const lost = () => {
      if (stopped) return
      if (!heard) silent++
      roomInfo(link)
        .then(() => {
          if (stopped) return
          if (silent >= 3) {
            setStatus('ended')
            return
          }
          setStatus((s) => (s === 'in' ? 'connecting' : s))
          retry = setTimeout(connect, wait)
          wait = Math.min(wait * 2, 10000)
        })
        .catch((err: unknown) => {
          if (stopped) return
          if (err instanceof TypeError) {
            retry = setTimeout(connect, wait)
            wait = Math.min(wait * 2, 10000)
            return
          }
          setStatus((s) => (s === 'refused' ? s : 'ended'))
        })
    }
    const connect = () => {
      const ws = new WebSocket(url)
      socket = ws
      heard = false
      heardAt = Date.now()
      ws.onmessage = onMessage
      ws.onclose = lost
    }
    connect()
    const watch = setInterval(() => {
      const ws = socket
      if (stopped || !ws || ws.readyState !== WebSocket.OPEN || Date.now() - heardAt < QUIET_MS) return
      ws.onclose = null
      ws.onmessage = null
      ws.close()
      lost()
    }, 5000)
    return () => {
      stopped = true
      clearTimeout(retry)
      clearInterval(watch)
      socket?.close()
    }
  }, [link, seat, push])

  const sending = useRef<Promise<unknown>>(Promise.resolve())
  const act = useCallback(
    (type: string, extra: Record<string, unknown> = {}): Promise<void> => {
      if (!link || !seat) return Promise.resolve()
      const run = async () => {
        await answer(await api(link, '/act', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ member: seat.member, token: seat.token, type, ...extra }),
          signal: AbortSignal.timeout(ACT_MS),
        }))
      }
      const next = sending.current.then(run)
      sending.current = next.catch(() => {})
      return next
    },
    [link, seat],
  )

  const answerKnock = useCallback(
    (member: string, allow: boolean) => {
      const knock = knocks.find((k) => k.member === member)
      setKnocks((was) => was.filter((k) => k.member !== member))
      void act('admit', { admit: member, allow }).catch(() => {
        if (knock) setKnocks((was) => (was.some((k) => k.member === member) ? was : [...was, knock]))
      })
    },
    [act, knocks],
  )

  const me = view?.members.find((m) => m.id === seat?.member)
  const avatarOf = (member?: string): string | undefined => {
    const m = member ? view?.members.find((o) => o.id === member) : undefined
    return link && m?.avatar
      ? `${link.base}/api/together/${link.code}/avatar/${m.id}?k=${encodeURIComponent(link.key)}&v=${m.avatar}`
      : undefined
  }
  return {
    view, status, lines, knocks, waitingFor, act, answerKnock, me, avatarOf,
    position: () => {
      const st = view?.state
      if (!st) return 0
      return st.playing ? st.position + (Date.now() + offset.current - st.at) / 1000 : st.position
    },
  }
}

export type Room = ReturnType<typeof useRoom>

export const memberState = (m: RoomMember): 'ready' | 'loading' | 'away' =>
  !m.connected || m.browsing ? 'away' : m.ready ? 'ready' : 'loading'

export const memberStatus = (m: RoomMember): string => (!m.connected ? 'Away' : m.browsing ? 'Browsing' : m.ready ? 'Ready' : 'Loading')

const TOASTS = 3

export function useRoomToasts(room: Room, on: boolean): RoomLine[] {
  const [shown, setShown] = useState<RoomLine[]>([])
  const seen = useRef(room.lines.length ? room.lines[room.lines.length - 1].id : 0)
  const timers = useRef(new Set<number>())
  useEffect(() => {
    const pending = timers.current
    return () => pending.forEach((t) => window.clearTimeout(t))
  }, [])
  useEffect(() => {
    const fresh = room.lines.filter((l) => l.id > seen.current)
    if (fresh.length === 0) return
    seen.current = fresh[fresh.length - 1].id
    const worth = fresh.filter((l) => l.kind !== 'event' || /paused|jumped|switched|suggests/.test(l.text)).slice(-TOASTS)
    if (!on || worth.length === 0) return
    setShown((was) => [...was, ...worth].slice(-TOASTS))
    for (const line of worth) {
      const t = window.setTimeout(
        () => {
          timers.current.delete(t)
          setShown((was) => was.filter((l) => l.id !== line.id))
        },
        line.kind === 'react' ? 1500 : 5000,
      )
      timers.current.add(t)
    }
  }, [room.lines, on])
  return on ? shown : []
}

export function useTogetherSync(video: HTMLVideoElement | null, room: Room | null) {
  const roomRef = useRef(room)
  roomRef.current = room
  const ready = useRef<boolean | null>(null)
  const playingRoom = () => !!roomRef.current?.view?.state.playing
  const atRoom = (t: number) => Math.abs(t - (roomRef.current?.position() ?? t)) < 1

  const state = room?.view?.state
  useEffect(() => {
    const r = roomRef.current
    if (!video || !state || !r) return
    const at = r.position()
    if (Math.abs(video.currentTime - at) > (state.playing ? 1 : 0.25)) video.currentTime = at
    if (state.playing && video.paused) void video.play().catch(() => {})
    if (!state.playing && !video.paused) video.pause()
    if (!state.playing) video.playbackRate = 1
  }, [video, state])

  useEffect(() => {
    if (!room) {
      ready.current = null
      return
    }
    const send = (type: string, extra?: Record<string, unknown>) => void roomRef.current?.act(type, extra).catch(() => {})
    const report = (on: boolean) => {
      if (ready.current === on) return
      ready.current = on
      send('ready', { ready: on })
    }
    if (!video) {
      if (ready.current) report(false)
      return
    }
    const onPlay = () => !playingRoom() && send('play')
    const onPause = () => playingRoom() && !video.ended && send('pause', { position: video.currentTime })
    const onSeeked = () => {
      const moved = !atRoom(video.currentTime)
      if (moved) send('seek', { position: video.currentTime })
      ready.current = null
      if (video.readyState >= 3) report(true)
      else if (!moved) report(false)
    }
    const onCanPlay = () => !video.seeking && report(true)
    const onWaiting = () => !video.seeking && report(false)
    const onEmptied = () => ready.current && report(false)
    video.addEventListener('play', onPlay)
    video.addEventListener('pause', onPause)
    video.addEventListener('seeked', onSeeked)
    video.addEventListener('canplay', onCanPlay)
    video.addEventListener('playing', onCanPlay)
    video.addEventListener('waiting', onWaiting)
    video.addEventListener('emptied', onEmptied)
    if (video.readyState >= 3) report(true)
    else if (ready.current) report(false)
    return () => {
      video.removeEventListener('play', onPlay)
      video.removeEventListener('pause', onPause)
      video.removeEventListener('seeked', onSeeked)
      video.removeEventListener('canplay', onCanPlay)
      video.removeEventListener('playing', onCanPlay)
      video.removeEventListener('waiting', onWaiting)
      video.removeEventListener('emptied', onEmptied)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [video, !!room])

  const roomSaysReady = room?.me?.ready
  useEffect(() => {
    if (!video || roomSaysReady !== false || ready.current !== true) return
    ready.current = null
    if (video.readyState >= 3 && !video.seeking) {
      ready.current = true
      void roomRef.current?.act('ready', { ready: true }).catch(() => {})
    }
  }, [video, roomSaysReady])

  useEffect(() => {
    if (!video || !room) return
    const id = window.setInterval(() => {
      const r = roomRef.current
      const st = r?.view?.state
      if (!r || !st?.playing || video.paused || video.seeking) return
      const diff = video.currentTime - r.position()
      if (Math.abs(diff) > 2) video.currentTime = r.position()
      else if (Math.abs(diff) > 0.3) video.playbackRate = diff > 0 ? 0.95 : 1.05
      else if (Math.abs(diff) < 0.1) video.playbackRate = 1
    }, 1000)
    return () => {
      window.clearInterval(id)
      video.playbackRate = 1
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [video, !!room])
}
