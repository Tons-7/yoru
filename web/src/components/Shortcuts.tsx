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
    <kbd className="min-w-7 rounded-md bg-base-800 px-1.5 py-0.5 text-center font-sans text-xs text-base-100 ring-1 ring-white/10">
      {children}
    </kbd>
  )
}

const does = (id: KeyAction) => KEY_ACTIONS.find((a) => a.id === id)!.does
const named = (key: string) => keyLabel(key).join('+')

/** Every keyboard shortcut behind "?", the player's changeable: keys nobody is told about go unused. */
export function Shortcuts() {
  const [open, setOpen] = useState(false)
  // The action whose key is being changed: the next press is its new key.
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

  // Heard before anything else on the page, so the press is a new key and nothing else: not ?, not Esc's close.
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
      // A character, or a key named on its cap; not a dead key, an IME's or one the browser can't name.
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

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
      <div ref={backdrop} className="relative w-full max-w-md">
        <div
          ref={panel}
          role="dialog"
          aria-modal="true"
          aria-label="Keyboard shortcuts"
          className="animate-fade-in max-h-[85vh] overflow-y-auto rounded-xl border border-base-800 bg-base-900 p-4 shadow-2xl scrollbar-thin"
        >
          <div className="mb-1 flex items-center justify-between gap-4">
            <h2 className="text-base font-semibold text-base-100">Keyboard shortcuts</h2>
            <button
              onClick={close}
              aria-label="Close"
              className="tap relative grid size-7 place-items-center rounded-md text-base-400 transition-colors hover:bg-base-800 hover:text-white"
            >
              ✕
            </button>
          </div>
          <p role="status" className="sticky -top-4 z-10 -mx-4 min-h-6 bg-base-900 px-4 py-1 text-xs text-accent-300">
            {said || (capturing && `Press the new key for ${does(capturing).toLowerCase()}, or Esc to keep ${named(keyOf(capturing))}`)}
          </p>

          <section className="mt-2">
            <h3 className="mb-1 text-[11px] font-semibold tracking-wider text-base-400 uppercase">While watching</h3>
            <p className="mb-1 text-xs text-base-500">Choose a key to change it.</p>
            <dl className="divide-y divide-base-800">
              {KEY_ACTIONS.map((a) => (
                <div key={a.id} className="flex items-center justify-between gap-4 py-1">
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
                        'tap relative flex min-h-8 items-center gap-1 rounded-md px-1 transition-colors hover:bg-base-800',
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
              ))}
              <div className="flex items-center justify-between gap-4 py-1.5">
                <dt className="text-sm text-base-200">Jump to that tenth of the episode</dt>
                <dd className="flex shrink-0 gap-1">
                  <Keycap>0</Keycap>
                  <span className="text-xs text-base-500">to</span>
                  <Keycap>9</Keycap>
                </dd>
              </div>
            </dl>

            <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
              <div role="radiogroup" aria-label="Single keys work" className="flex items-center gap-1 text-xs">
                <span className="mr-1 text-base-400">Keys work</span>
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
                    // One stop in the tab order, arrows between the two, as a radio group is.
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
              <p className="mt-1 text-xs text-base-500">Click the picture or tab to it first; a screen reader keeps its own keys elsewhere.</p>
            )}
          </section>

          <section className="mt-4">
            <h3 className="mb-1 text-[11px] font-semibold tracking-wider text-base-400 uppercase">Anywhere</h3>
            <dl className="divide-y divide-base-800">
              {PAGE_SHORTCUTS.map((s) => (
                <div key={s.does} className="flex items-center justify-between gap-4 py-1.5">
                  <dt className="text-sm text-base-200">{s.does}</dt>
                  <dd className="flex shrink-0 gap-1">
                    {s.keys.map((k) => (
                      <Keycap key={k}>{k}</Keycap>
                    ))}
                  </dd>
                </div>
              ))}
            </dl>
          </section>
        </div>
      </div>
    </div>,
    home,
  )
}
