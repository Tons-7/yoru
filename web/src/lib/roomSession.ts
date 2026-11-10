import { createContext, useContext } from 'react'
import type { Room, Seat, TogetherLink } from './together'

export interface RoomSession {
  together: TogetherLink | null
  seat: Seat | null
  invite?: string
  room: Room
  inRoom: boolean
  isHost: boolean
  canPick: boolean
  open: (link: TogetherLink) => void
  sit: (link: TogetherLink, seat: Seat) => void
  leave: () => void
  drop: () => void
  attach: () => () => void
}

export const RoomSessionContext = createContext<RoomSession | null>(null)

export function useRoomSession(): RoomSession {
  const session = useContext(RoomSessionContext)
  if (!session) throw new Error('useRoomSession outside RoomProvider')
  return session
}
