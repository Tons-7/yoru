import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { RoomSessionContext, type RoomSession } from '../lib/roomSession'
import { parseTogether, saveSeat, savedSeat, useRoom, type Seat, type TogetherLink } from '../lib/together'
import { toast } from '../lib/toast'

const ACTIVE_KEY = 'yoru.together.active'

function activeLink(): TogetherLink | null {
  try {
    return parseTogether(sessionStorage.getItem(ACTIVE_KEY) ?? '')
  } catch {
    return null
  }
}

function keepActive(link: TogetherLink | null) {
  try {
    if (link) sessionStorage.setItem(ACTIVE_KEY, link.href)
    else sessionStorage.removeItem(ACTIVE_KEY)
  } catch {
    return
  }
}

export function RoomProvider({ children }: { children: ReactNode }) {
  const [together, setTogether] = useState<TogetherLink | null>(activeLink)
  const [seat, setSeat] = useState<Seat | null>(() => (together ? savedSeat(together) : null))
  const [players, setPlayers] = useState(0)
  const room = useRoom(together, seat)
  const inRoom = !!together && !!seat && room.status === 'in'
  const isHost = !!room.me?.host
  const canPick = isHost || !!room.view?.anyone

  const open = useCallback(
    (link: TogetherLink) => {
      if (link.code === together?.code) return
      setTogether(link)
      setSeat(savedSeat(link))
      keepActive(link)
    },
    [together?.code],
  )
  const sit = useCallback((link: TogetherLink, next: Seat) => {
    saveSeat(link, next)
    setTogether(link)
    setSeat(next)
    keepActive(link)
  }, [])
  const drop = useCallback(() => {
    if (together) saveSeat(together, null)
    setTogether(null)
    setSeat(null)
    keepActive(null)
  }, [together])
  const act = room.act
  const leave = useCallback(() => {
    void act('leave').catch(() => {})
    drop()
  }, [act, drop])
  const attach = useCallback(() => {
    setPlayers((n) => n + 1)
    return () => setPlayers((n) => n - 1)
  }, [])

  const ended = !!seat && room.status === 'ended'
  useEffect(() => {
    if (!ended) return
    const soon = window.setTimeout(() => {
      toast('The room has ended.', 'info')
      drop()
    }, 0)
    return () => window.clearTimeout(soon)
  }, [ended, drop])

  const browsing = players === 0
  const saidBrowsing = !!room.me?.browsing
  useEffect(() => {
    if (!inRoom || saidBrowsing === browsing) return
    const soon = window.setTimeout(() => void act('browsing', { browsing }).catch(() => {}), browsing ? 400 : 0)
    return () => window.clearTimeout(soon)
  }, [inRoom, browsing, saidBrowsing, act])

  const value = useMemo<RoomSession>(
    () => ({ together, seat, invite: seat?.invite, room, inRoom, isHost, canPick, open, sit, leave, drop, attach }),
    [together, seat, room, inRoom, isHost, canPick, open, sit, leave, drop, attach],
  )
  return <RoomSessionContext.Provider value={value}>{children}</RoomSessionContext.Provider>
}
