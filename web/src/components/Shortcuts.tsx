import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { cx } from '../lib/format'
import { useDismiss, useModalFocus } from '../lib/hooks'
import {
  holderOf,
  KEY_ACTIONS,
  keyLabel,
  keyOf,
  pressedKey,
  reservedFor,
  resetKeys,
  setKey,
  setKeyScope,
  useKeymap,
  type KeyAction,
} from '../lib/keymap'
import { PAGE_SHORTCUTS, plainKey, SHOW_SHORTCUTS, usePortalHome } from './keys'

const MODIFIERS = new Set(['Shift', 'Control', 'Alt', 'Meta', 'CapsLock', 'AltGraph'])
const NAMED_KEYS = /^(Arrow(Up|Down|Left|Right)|Home|End|PageUp|PageDown|Insert|Delete|Backspace|F([1-9]|1[0-2]))$/

function Keycap({ children }: { children: ReactNode }) {
  return (
    <kbd className="min-w-7 rounded-md bg-base-700 px-1.5 py-0.5 text-center font-sans text-xs font-medium text-white shadow-[inset_0_-2px_0_rgb(0_0_0/0.35)] ring-1 ring-white/10">
      {children}
    </kbd>
  )
}

const does = (id: KeyAction) => KEY_ACTIONS.find((a) => a.id === id)!.does
const named = (key: string) => keyLabel(key).join('+')

type KeyGroup = { title: string; ids: KeyAction[]; tenths?: boolean }

const LEFT: KeyGroup[] = [
  {
    title: 'Playback',
    ids: ['play', 'back5', 'forward5', 'back10', 'forward10', 'slower', 'faster', 'frameBack', 'frameForward'],
    tenths: true,
  },
  { title: 'Episodes', ids: ['next', 'previous', 'skip'] },
  { title: 'Skip times', ids: ['markStart', 'markEnd'] },
]

const RIGHT: KeyGroup[] = [
  { title: 'Subtitles', ids: ['subtitles', 'subEarlier', 'subLater', 'subBigger', 'subSmaller'] },
  { title: 'Sound', ids: ['volumeUp', 'volumeDown', 'mute', 'voice'] },
  { title: 'Picture', ids: ['fullscreen', 'focus', 'info'] },
]

const LISTED = new Set([...LEFT, ...RIGHT].flatMap((g) => g.ids))
const OTHER: KeyGroup = { title: 'More', ids: KEY_ACTIONS.map((a) => a.id).filter((id) => !LISTED.has(id)) }

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="mb-3">
      <h3 className="mb-1 px-2 text-[11px] font-semibold tracking-wider text-base-400 uppercase">{title}</h3>
      <dl className="divide-y divide-white/[0.05]">{children}</dl>
    </section>
  )
}

export function Shortcuts() {
  const [open, setOpen] = useState(false)
  const [capturing, setCapturing] = useState<KeyAction | null>(null)
  const [said, setSaid] = useState('')
  const close = useCallback(() => {
    setOpen(false)
    setCapturing(null)
    setSaid('')
  }, [])
  const backdrop = useDismiss<HTMLDivElement>(close, open)
  const panel = useRef<HTMLDivElement>(null)
  useModalFocus(panel, open)
  const home = usePortalHome()
  const keymap = useKeymap()

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (plainKey(e) === '?') setOpen((v) => !v)
    }
    const show = () => setOpen(true)
    window.addEventListener('keydown', onKey)
    window.addEventListener(SHOW_SHORTCUTS, show)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener(SHOW_SHORTCUTS, show)
    }
  }, [])

  useEffect(() => {
    if (!capturing) return
    const onKey = (e: KeyboardEvent) => {
      if (MODIFIERS.has(e.key)) return
      e.preventDefault()
      e.stopPropagation()
      if (e.key === 'Escape') {
        setCapturing(null)
        setSaid(`${does(capturing)} keeps ${named(keyOf(capturing))}`)
        return
      }
      if ((e.ctrlKey || e.metaKey || e.altKey) && !e.getModifierState('AltGraph')) {
        setSaid('One key on its own, without Ctrl, Alt or ⌘')
        return
      }
      const key = pressedKey(e)
      if (key.length !== 1 && !NAMED_KEYS.test(key)) {
        setSaid("That key can't be used here; try another")
        return
      }
      const reserved = reservedFor(key)
      const fixed = KEY_ACTIONS.find((a) => a.fixed?.includes(key))
      if (reserved || fixed) {
        setSaid(`${named(key)} is kept for ${reserved ?? fixed!.does.toLowerCase()}; try another`)
        return
      }
      const holder = holderOf(key, capturing)
      const was = keyOf(capturing)
      setKey(capturing, key)
      setCapturing(null)
      setSaid(
        holder
          ? `${does(capturing)}: ${named(key)}. ${does(holder)} took ${named(was)} in its place`
          : `${does(capturing)}: ${named(key)}`,
      )
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [capturing])

  if (!open) return null
  const changed = Object.keys(keymap.keys).length > 0
  const message = said || (capturing && `Press the new key for ${does(capturing).toLowerCase()}, or Esc to keep ${named(keyOf(capturing))}`)
  const row = 'flex min-h-9 items-center justify-between gap-4 px-2'
  const group = (g: KeyGroup) => (
    <Group key={g.title} title={g.title}>
      {g.ids.map((id) => {
        const a = KEY_ACTIONS.find((x) => x.id === id)!
        return (
          <div key={a.id} className={row}>
            <dt className="text-sm text-base-200">{a.does}</dt>
            <dd className="flex shrink-0 items-center gap-1">
              {a.fixed?.map((k) => <Keycap key={k}>{keyLabel(k).join('+')}</Keycap>)}
              <button
                onClick={() => {
                  setCapturing(capturing === a.id ? null : a.id)
                  setSaid('')
                }}
                aria-label={`${a.does}: ${named(keyOf(a.id))}. Change`}
                aria-pressed={capturing === a.id}
                className={cx(
                  'tap relative flex min-h-8 items-center gap-1 rounded-md px-1 transition-colors hover:bg-white/[0.06] hover:ring-1 hover:ring-accent-400/60',
                  capturing === a.id && 'bg-accent-500/20 ring-1 ring-accent-400',
                )}
              >
                {capturing === a.id ? (
                  <span className="px-1 text-xs text-accent-200">Press a key</span>
                ) : (
                  keyLabel(keyOf(a.id)).map((k) => <Keycap key={k}>{k}</Keycap>)
                )}
              </button>
            </dd>
          </div>
        )
      })}
      {g.tenths && (
        <div className={row}>
          <dt className="text-sm text-base-200">Jump to that tenth of the episode</dt>
          <dd className="flex shrink-0 items-center gap-1">
            <Keycap>0</Keycap>
            <span className="text-xs text-base-500">to</span>
            <Keycap>9</Keycap>
          </dd>
        </div>
      )}
    </Group>
  )

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
      <div ref={backdrop} className="relative w-full max-w-3xl">
        <div
          ref={panel}
          role="dialog"
          aria-modal="true"
          aria-label="Keyboard shortcuts"
          className="flex max-h-[92vh] animate-fade-in flex-col overflow-hidden rounded-card bg-base-850 shadow-panel ring-1 ring-white/[0.06]"
        >
          <div className="flex shrink-0 items-start justify-between gap-4 px-5 pt-5 pb-3">
            <div>
              <h2 className="text-lg font-semibold text-white">Keyboard shortcuts</h2>
              <p className="mt-0.5 text-sm text-base-400">Choose a key to change it.</p>
            </div>
            <button
              onClick={close}
              aria-label="Close"
              className="press grid size-9 shrink-0 place-items-center rounded-full bg-base-800 text-base-300 transition-colors hover:bg-base-700 hover:text-white"
            >
              <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
                <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />
              </svg>
            </button>
          </div>
          <p role="status" className={cx('shrink-0 px-5 text-sm text-accent-300', message ? 'pb-3' : 'sr-only')}>
            {message}
          </p>

          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-3 scrollbar-thin sm:grid sm:grid-cols-2 sm:gap-x-6">
            <div>{LEFT.map(group)}</div>
            <div>
              {RIGHT.map(group)}
              {OTHER.ids.length > 0 && group(OTHER)}
              <Group title="Anywhere">
                {PAGE_SHORTCUTS.map((s) => (
                  <div key={s.does} className={row}>
                    <dt className="text-sm text-base-200">{s.does}</dt>
                    <dd className="flex shrink-0 gap-1">
                      {s.keys.map((k) => (
                        <Keycap key={k}>{k}</Keycap>
                      ))}
                    </dd>
                  </div>
                ))}
              </Group>
            </div>
          </div>

          <div className="shrink-0 border-t border-white/[0.06] px-5 py-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div role="radiogroup" aria-label="Single keys work" className="flex items-center gap-1 rounded-lg bg-base-900 p-0.5 text-xs">
                <span className="mr-1 pl-2 text-base-400">Keys work</span>
                {(
                  [
                    ['page', 'Anywhere on the page'],
                    ['player', 'In the player only'],
                  ] as const
                ).map(([value, label]) => (
                  <button
                    key={value}
                    role="radio"
                    aria-checked={keymap.scope === value}
                    tabIndex={keymap.scope === value ? 0 : -1}
                    onKeyDown={(e) => {
                      if (!e.key.startsWith('Arrow')) return
                      e.preventDefault()
                      const other = value === 'page' ? 'player' : 'page'
                      setKeyScope(other)
                      ;(e.currentTarget.parentElement?.querySelector(`[data-scope=${other}]`) as HTMLElement | null)?.focus()
                    }}
                    data-scope={value}
                    onClick={() => setKeyScope(value)}
                    className={cx(
                      'tap relative rounded-md px-2 py-1 transition-colors',
                      keymap.scope === value ? 'bg-base-750 text-white' : 'text-base-400 hover:text-base-100',
                    )}
                  >
                    {label}
                  </button>
                ))}
              </div>
              <button
                onClick={() => {
                  resetKeys()
                  setCapturing(null)
                  setSaid('Every key is back to how it came')
                }}
                disabled={!changed}
                className="tap relative rounded-md px-2 py-1 text-xs text-base-300 transition-colors hover:bg-base-800 hover:text-white disabled:text-base-600 disabled:hover:bg-transparent"
              >
                {changed ? 'Reset keys' : 'Keys are as they came'}
              </button>
            </div>
            {keymap.scope === 'player' && (
              <p className="mt-2 text-xs text-base-500">Click the picture or tab to it first; a screen reader keeps its own keys elsewhere.</p>
            )}
          </div>
        </div>
      </div>
    </div>,
    home,
  )
}
