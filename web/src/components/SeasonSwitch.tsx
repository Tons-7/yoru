import { useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { statusLabel } from '../lib/api'
import { cx } from '../lib/format'
import type { Season } from '../lib/queries'
import { buttonClass } from '../lib/ui'
import { PopSheet } from './PopSheet'

const nameOf = (s: Season) => s.english || s.romaji || `Anime ${s.id}`

function progressWord(s: Season): { text: string; done: boolean } | null {
  const finished = !!s.episodes && s.progress >= s.episodes
  if (s.listStatus === 'COMPLETED' || finished) return { text: 'Finished', done: true }
  if (s.progress > 0) return { text: `${s.progress}${s.episodes ? ` of ${s.episodes}` : ''} watched`, done: false }
  const tag = statusLabel(s.listStatus)
  return tag ? { text: tag, done: false } : null
}

export function SeasonSwitch({ seasons, current }: { seasons: Season[]; current: number }) {
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const here = seasons.find((s) => s.id === current)
  if (seasons.length < 2 || !here) return null

  return (
    <>
      <button
        ref={trigger}
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={`Season: ${nameOf(here)}. Change season`}
        className={cx(buttonClass('secondary', 'sm'), 'max-w-full min-w-0')}
      >
        <span className="truncate">{nameOf(here)}</span>
        <svg viewBox="0 0 24 24" className="size-3.5 shrink-0 text-base-400" aria-hidden>
          <path d="m6 9 6 6 6-6" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      <PopSheet open={open} onClose={() => setOpen(false)} title="Seasons" anchor={trigger} width={340}>
        <ul className="space-y-1">
          {seasons.map((s) => {
            const word = progressWord(s)
            const isHere = s.id === current
            return (
              <li key={s.id}>
                <Link
                  to={`/anime/${s.id}`}
                  onClick={() => setOpen(false)}
                  aria-current={isHere ? 'page' : undefined}
                  className={cx(
                    'flex min-h-14 items-center gap-3 rounded-lg p-1.5 transition-colors',
                    isHere ? 'bg-accent-500/15' : 'hover:bg-base-800',
                  )}
                >
                  {s.cover ? (
                    <img src={s.cover} alt="" loading="lazy" className="h-12 w-8 shrink-0 rounded object-cover" />
                  ) : (
                    <span className="h-12 w-8 shrink-0 rounded bg-base-800" />
                  )}
                  <span className="min-w-0 flex-1">
                    <span className="line-clamp-2 text-sm leading-snug font-medium text-base-100">{nameOf(s)}</span>
                    <span className="block text-[13px] text-base-400 tabular-nums">
                      {[s.year ?? 'TBA', s.episodes ? `${s.episodes} eps` : null].filter(Boolean).join(' · ')}
                      {word && (
                        <span className={word.done ? 'text-emerald-300' : 'text-accent-300'}> · {word.text}</span>
                      )}
                    </span>
                  </span>
                  {isHere && (
                    <svg viewBox="0 0 24 24" className="size-4 shrink-0 text-accent-300" aria-hidden>
                      <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                  )}
                </Link>
              </li>
            )
          })}
        </ul>
      </PopSheet>
    </>
  )
}
