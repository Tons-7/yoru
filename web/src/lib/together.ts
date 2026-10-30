import { useCallback, useEffect, useRef, useState } from 'react'
import { readKey, writeKey } from './storage'

// Watch together: the room lives on the host's Yoru; this page talks to it directly, its own plays video.

export interface TogetherLink {
  /** The host's Yoru, as "https://name:port". */
  base: string
  code: string
  key: string
  /** The link as shared, for showing and copying. */
  href: string
}

/** A room link from pasted text, as createTogether makes it. */
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
  waiting: boolean
}

export interface RoomMember {
  id: string
  name: string
  host?: boolean
  ready: boolean
  connected: boolean
}

export interface RoomView {
  code: string
  title: string
  ask: boolean
  wait: boolean
  state: RoomState
  members: RoomMember[]
}

/** A line in the room's feed: a message, a reaction, or what someone did. */
export interface RoomLine {
  id: number
  kind: 'chat' | 'react' | 'event'
  from?: string
  text: string
}

export interface Seat {
  member: string
  token: string
  /** The host's, kept so a reload still has the invite to share. */
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

export async function joinRoom(link: TogetherLink, name: string): Promise<Seat & { admitted: boolean }> {
  return answer(await api(link, '/join', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name }) }))
}

const seatKey = (link: TogetherLink) => `yoru.together.${link.code}`

/** The seat this browser holds in a room, kept for the visit so a reload rejoins it. */
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
    // For this visit only.
  }
}

const OVERLAY_KEY = 'yoru.together.overlay'

/** Messages shown over the picture, chosen per device; on unless turned off. */
export function overlayOn(): boolean {
  return readKey(OVERLAY_KEY) !== 'off'
}

export function setOverlay(on: boolean) {
  writeKey(OVERLAY_KEY, on ? 'on' : 'off')
}

export type RoomStatus ='connecting' | 'pending' | 'in' | 'refused' | 'ended'

const REASONS: Record<string, (by: string) => string> = {
  pause: (by) => `${by} paused`,
  play: (by) => `${by} played`,
  seek: (by) => `${by} jumped`,
  episode: (by) => `${by} moved on to the next episode`,
}

/** The room's live line: what it is now, its feed, and a way to act in it. */
export function useRoom(link: TogetherLink | null, seat: Seat | null) {
  const [view, setView] = useState<RoomView | null>(null)
  const [status, setStatus] = useState<RoomStatus>('connecting')
  const [lines, setLines] = useState<RoomLine[]>([])
  const [knocks, setKnocks] = useState<Array<{ member: string; name: string }>>([])
  const [waitingFor, setWaitingFor] = useState<string[]>([])
  // The host's clock less this one's, from the times its events carry.
  const offset = useRef(0)
  const lineId = useRef(0)
  const push = useCallback((line: Omit<RoomLine, 'id'>) => {
    setLines((was) => [...was.slice(-199), { ...line, id: ++lineId.current }])
  }, [])

  useEffect(() => {
    if (!link || !seat) return
    const url = `${link.base}/api/together/${link.code}/events?k=${encodeURIComponent(link.key)}&m=${seat.member}&t=${encodeURIComponent(seat.token)}`
    const source = new EventSource(url)
    source.onmessage = (e) => {
      const event = JSON.parse(e.data as string) as Record<string, unknown>
      if (typeof event.now === 'number') offset.current = event.now - Date.now()
      switch (event.type) {
        case 'pending':
          setStatus('pending')
          break
        case 'refused':
          setStatus('refused')
          source.close()
          break
        case 'room':
          setView(event as unknown as RoomView)
          setStatus('in')
          break
        case 'state': {
          setView((was) => (was ? { ...was, state: event.state as RoomState } : was))
          setWaitingFor((event.waitingFor as string[] | undefined) ?? [])
          const says = REASONS[event.reason as string]
          if (says && event.by) push({ kind: 'event', text: says(event.by as string) })
          break
        }
        case 'chat':
        case 'react':
          push({ kind: event.type, from: event.from as string, text: event.text as string })
          break
        case 'knock':
          setKnocks((was) => [...was, { member: event.member as string, name: event.name as string }])
          break
      }
    }
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) setStatus((s) => (s === 'refused' ? s : 'ended'))
    }
    return () => source.close()
  }, [link, seat, push])

  const act = useCallback(
    async (type: string, extra: Record<string, unknown> = {}) => {
      if (!link || !seat) return
      await answer(await api(link, '/act', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ member: seat.member, token: seat.token, type, ...extra }),
      }))
    },
    [link, seat],
  )

  const answerKnock = useCallback(
    (member: string, allow: boolean) => {
      setKnocks((was) => was.filter((k) => k.member !== member))
      void act('admit', { admit: member, allow })
    },
    [act],
  )

  const me = view?.members.find((m) => m.id === seat?.member)
  return {
    view, status, lines, knocks, waitingFor, act, answerKnock, me,
    /** Where the room's episode is now, by the host's clock. */
    position: () => {
      const st = view?.state
      if (!st) return 0
      return st.playing ? st.position + (Date.now() + offset.current - st.at) / 1000 : st.position
    },
  }
}

export type Room = ReturnType<typeof useRoom>

/** Keeps this page's video with the room: presses out, the room's applied, readiness, drift evened out. */
export function useTogetherSync(video: HTMLVideoElement | null, room: Room | null) {
  const roomRef = useRef(room)
  roomRef.current = room
  const ready = useRef<boolean | null>(null)
  // A press is what differs from the room; what matches it is the room's own state being applied here.
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
    if (!video || !room) return
    const send = (type: string, extra?: Record<string, unknown>) => void roomRef.current?.act(type, extra).catch(() => {})
    const report = (on: boolean) => {
      if (ready.current === on) return
      ready.current = on
      send('ready', { ready: on })
    }
    const onPlay = () => !playingRoom() && send('play')
    const onPause = () => playingRoom() && !video.ended && send('pause', { position: video.currentTime })
    const onSeeked = () => {
      if (!atRoom(video.currentTime)) send('seek', { position: video.currentTime })
      ready.current = null
      if (video.readyState >= 3) report(true)
    }
    const onCanPlay = () => !video.seeking && report(true)
    const onWaiting = () => report(false)
    video.addEventListener('play', onPlay)
    video.addEventListener('pause', onPause)
    video.addEventListener('seeked', onSeeked)
    video.addEventListener('canplay', onCanPlay)
    video.addEventListener('playing', onCanPlay)
    video.addEventListener('waiting', onWaiting)
    if (video.readyState >= 3) report(true)
    return () => {
      video.removeEventListener('play', onPlay)
      video.removeEventListener('pause', onPause)
      video.removeEventListener('seeked', onSeeked)
      video.removeEventListener('canplay', onCanPlay)
      video.removeEventListener('playing', onCanPlay)
      video.removeEventListener('waiting', onWaiting)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [video, !!room])

  // The room forgets readiness on a reconnect or a new episode; said again if this page is loaded.
  const roomSaysReady = room?.me?.ready
  useEffect(() => {
    if (!video || roomSaysReady !== false || ready.current !== true) return
    ready.current = null
    if (video.readyState >= 3 && !video.seeking) {
      ready.current = true
      void roomRef.current?.act('ready', { ready: true }).catch(() => {})
    }
  }, [video, roomSaysReady])

  // A little faster or slower until level; a jump past two seconds.
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
