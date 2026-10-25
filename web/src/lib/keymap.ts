import { useSyncExternalStore } from 'react'

// The player's keys: each action has one key the shortcuts list can change, kept per device.

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
  | 'subtitles'
  | 'subEarlier'
  | 'subLater'
  | 'info'
  | 'markStart'
  | 'markEnd'

export interface KeyActionInfo {
  id: KeyAction
  does: string
  key: string
  /** Keys that always do it too, never changed: Space plays as on every player. */
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
  { id: 'fullscreen', does: 'Fullscreen', key: 'f' },
  { id: 'subtitles', does: 'Subtitles: next track, then off', key: 'c' },
  { id: 'subEarlier', does: 'Subtitles earlier', key: 'z' },
  { id: 'subLater', does: 'Subtitles later', key: 'x' },
  { id: 'info', does: 'Playback info', key: 'i' },
  { id: 'markStart', does: 'Mark where the opening or ending starts', key: '[' },
  { id: 'markEnd', does: 'Mark where it ends, and save it', key: ']' },
]

/** Keys no player action may take, and what they do instead; 0 to 9 jump through the episode. */
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
  // D opens the downloads with Shift held too.
  return page[key] ?? page[key.toLowerCase()]
}

const STORE = 'yoru.keys'

interface Saved {
  keys: Partial<Record<KeyAction, string>>
  /** "player": single keys only while the player has focus, so a screen reader's own keys pass through. */
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
  try {
    localStorage.setItem(STORE, JSON.stringify(next))
  } catch {
    // Storage blocked (a private window): kept for this visit.
  }
  for (const l of listeners) l()
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

export function keyOf(action: KeyAction): string {
  return saved.keys[action] ?? KEY_ACTIONS.find((a) => a.id === action)!.key
}

/** The key a press means: a letter as typed with Shift, or lowercase without it (Caps Lock too). */
export function pressedKey(e: KeyboardEvent): string {
  if (e.key.length === 1 && /[a-z]/i.test(e.key)) return e.shiftKey ? e.key.toUpperCase() : e.key.toLowerCase()
  return e.key
}

/** The action a press runs; an uppercase press falls back to its lowercase key, as N did next before. */
export function actionFor(e: KeyboardEvent): KeyAction | undefined {
  const key = pressedKey(e)
  const find = (k: string) => KEY_ACTIONS.find((a) => keyOf(a.id) === k || a.fixed?.includes(k))?.id
  return find(key) ?? (key !== key.toLowerCase() ? find(key.toLowerCase()) : undefined)
}

/** The action already holding a key, other than the one asking. */
export function holderOf(key: string, except?: KeyAction): KeyAction | undefined {
  return KEY_ACTIONS.find((a) => a.id !== except && (keyOf(a.id) === key || a.fixed?.includes(key)))?.id
}

export function setKey(action: KeyAction, key: string) {
  const keys = { ...saved.keys }
  const holder = holderOf(key, action)
  // Taken from another action, the two swap.
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

/** Changes with the shortcuts list, so tooltips and the list show the keys as they are now. */
export function useKeymap(): Saved {
  return useSyncExternalStore(subscribe, () => saved)
}

/** A key as it reads on a keycap: Space, arrows, Shift+P. */
export function keyLabel(key: string): string[] {
  const arrows: Record<string, string> = { ArrowLeft: '←', ArrowRight: '→', ArrowUp: '↑', ArrowDown: '↓' }
  if (key === ' ') return ['Space']
  if (arrows[key]) return [arrows[key]]
  if (key.length === 1 && /[A-Z]/.test(key)) return ['Shift', key]
  return [key.length === 1 ? key.toUpperCase() : key]
}

/** A key for a tooltip: "Shift+P", "K". */
export function keyHint(action: KeyAction): string {
  return keyLabel(keyOf(action)).join('+')
}
