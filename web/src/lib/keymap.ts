import { useSyncExternalStore } from 'react'
import { writeKey } from './storage'

export type KeyAction =
  | 'play'
  | 'back5'
  | 'forward5'
  | 'back10'
  | 'forward10'
  | 'slower'
  | 'faster'
  | 'frameBack'
  | 'frameForward'
  | 'skip'
  | 'next'
  | 'previous'
  | 'volumeUp'
  | 'volumeDown'
  | 'mute'
  | 'voice'
  | 'fullscreen'
  | 'focus'
  | 'subtitles'
  | 'subEarlier'
  | 'subLater'
  | 'subBigger'
  | 'subSmaller'
  | 'info'
  | 'markStart'
  | 'markEnd'

export interface KeyActionInfo {
  id: KeyAction
  does: string
  key: string
  fixed?: string[]
}

export const KEY_ACTIONS: KeyActionInfo[] = [
  { id: 'play', does: 'Play or pause', key: 'k', fixed: [' '] },
  { id: 'back5', does: 'Back 5 seconds', key: 'ArrowLeft' },
  { id: 'forward5', does: 'Forward 5 seconds', key: 'ArrowRight' },
  { id: 'back10', does: 'Back 10 seconds', key: 'j' },
  { id: 'forward10', does: 'Forward 10 seconds; held, plays at 2×', key: 'l' },
  { id: 'slower', does: 'Slower', key: '<' },
  { id: 'faster', does: 'Faster', key: '>' },
  { id: 'frameBack', does: 'One frame back, while paused', key: ',' },
  { id: 'frameForward', does: 'One frame forward, while paused', key: '.' },
  { id: 'skip', does: 'Skip the opening or ending, while the button shows', key: 's' },
  { id: 'next', does: 'Next episode', key: 'n' },
  { id: 'previous', does: 'Previous episode', key: 'P' },
  { id: 'volumeUp', does: 'Volume up', key: 'ArrowUp' },
  { id: 'volumeDown', does: 'Volume down', key: 'ArrowDown' },
  { id: 'mute', does: 'Mute', key: 'm' },
  { id: 'voice', does: 'Voice boost: off, medium, high', key: 'v' },
  { id: 'fullscreen', does: 'Full screen', key: 'f' },
  { id: 'focus', does: 'Focus: just the player', key: 't' },
  { id: 'subtitles', does: 'Subtitles: next track, then off', key: 'c' },
  { id: 'subEarlier', does: 'Subtitles earlier', key: 'z' },
  { id: 'subLater', does: 'Subtitles later', key: 'x' },
  { id: 'subBigger', does: 'Bigger subtitles', key: '+', fixed: ['='] },
  { id: 'subSmaller', does: 'Smaller subtitles', key: '-' },
  { id: 'info', does: 'Playback info', key: 'i' },
  { id: 'markStart', does: 'Mark where the opening or ending starts', key: '[' },
  { id: 'markEnd', does: 'Mark where it ends, and save it', key: ']' },
]

export function reservedFor(key: string): string | undefined {
  if (/^[0-9]$/.test(key)) return 'jumping through the episode'
  const page: Record<string, string> = {
    '/': 'Search',
    d: 'Downloads',
    '?': 'this list',
    Escape: 'closing panels',
    Tab: 'moving between controls',
    Enter: 'pressing buttons',
  }
  return page[key] ?? page[key.toLowerCase()]
}

const STORE = 'yoru.keys'

interface Saved {
  keys: Partial<Record<KeyAction, string>>
  scope: 'page' | 'player'
}

function read(): Saved {
  try {
    const parsed = JSON.parse(localStorage.getItem(STORE) ?? '{}') as Partial<Saved>
    return { keys: parsed.keys ?? {}, scope: parsed.scope === 'player' ? 'player' : 'page' }
  } catch {
    return { keys: {}, scope: 'page' }
  }
}

let saved = read()
const listeners = new Set<() => void>()

function write(next: Saved) {
  saved = next
  writeKey(STORE, JSON.stringify(next))
  for (const l of listeners) l()
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

export function keyOf(action: KeyAction): string {
  const own = saved.keys[action]
  if (own) return own
  const key = KEY_ACTIONS.find((a) => a.id === action)!.key
  return Object.entries(saved.keys).some(([id, k]) => id !== action && k === key) ? '' : key
}

export function pressedKey(e: KeyboardEvent): string {
  if (e.key.length === 1 && /[a-z]/i.test(e.key)) return e.shiftKey ? e.key.toUpperCase() : e.key.toLowerCase()
  return e.key
}

export function actionFor(e: KeyboardEvent): KeyAction | undefined {
  const key = pressedKey(e)
  const find = (k: string) => KEY_ACTIONS.find((a) => keyOf(a.id) === k || a.fixed?.includes(k))?.id
  return find(key) ?? (key !== key.toLowerCase() ? find(key.toLowerCase()) : undefined)
}

export function holderOf(key: string, except?: KeyAction): KeyAction | undefined {
  return KEY_ACTIONS.find((a) => a.id !== except && (keyOf(a.id) === key || a.fixed?.includes(key)))?.id
}

export function setKey(action: KeyAction, key: string) {
  const keys = { ...saved.keys }
  const holder = holderOf(key, action)
  if (holder) keys[holder] = keyOf(action)
  keys[action] = key
  write({ ...saved, keys })
}

export function resetKeys() {
  write({ ...saved, keys: {} })
}

export function setKeyScope(scope: Saved['scope']) {
  write({ ...saved, scope })
}

export function keyScope(): Saved['scope'] {
  return saved.scope
}

export function useKeymap(): Saved {
  return useSyncExternalStore(subscribe, () => saved)
}

export function keyLabel(key: string): string[] {
  if (!key) return ['None']
  const arrows: Record<string, string> = { ArrowLeft: '←', ArrowRight: '→', ArrowUp: '↑', ArrowDown: '↓' }
  if (key === ' ') return ['Space']
  if (arrows[key]) return [arrows[key]]
  if (key.length === 1 && /[A-Z]/.test(key)) return ['Shift', key]
  return [key.length === 1 ? key.toUpperCase() : key]
}

export function keyHint(action: KeyAction): string {
  return keyLabel(keyOf(action)).join('+')
}
