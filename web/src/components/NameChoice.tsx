import { useRef, useState } from 'react'
import { cx } from '../lib/format'
import { useTrackers } from '../lib/queries'
import { useNameFrom, useProfile, type NameFrom } from '../lib/profile'

const card = 'flex min-h-16 items-center gap-3 rounded-xl border px-3.5 py-2.5 transition-colors'
const radio = 'size-[18px] shrink-0 accent-accent-400'

export function NameChoice({ host }: { host: boolean }) {
  const profile = useProfile().data
  const trackers = useTrackers().data?.trackers ?? []
  const choose = useNameFrom()
  const own = useRef<HTMLInputElement>(null)
  const saved = profile?.name ?? ''
  const [name, setName] = useState(saved)
  const [seen, setSeen] = useState(saved)
  const [ownPicked, setOwnPicked] = useState(false)
  if (seen !== saved) {
    setSeen(saved)
    setName(saved)
  }
  const user = (provider: string) => trackers.find((t) => t.provider === provider && t.connected)?.user
  const chosen = profile?.nameFrom ?? 'anilist'
  const from: NameFrom =
    chosen === 'own' ? 'own' : (([chosen, 'anilist', 'mal'] as const).find((source) => user(source)) ?? 'own')

  const site = (value: NameFrom, title: string, site: string) => {
    const name = user(value)
    const active = from === value
    return (
      <label
        className={cx(
          card,
          active ? 'border-accent-500 bg-accent-500/10' : 'border-base-800 bg-base-900',
          !name && 'border-dashed',
          name && host && 'cursor-pointer',
        )}
      >
        <input
          type="radio"
          name="name-from"
          checked={active}
          disabled={!name || !host}
          onChange={() => choose.mutate({ from: value })}
          className={radio}
        />
        <span className="min-w-0">
          <span className={cx('block text-sm font-semibold', name ? 'text-white' : 'text-base-400')}>{title}</span>
          <span className="block truncate text-[13px] text-base-400">{name ?? `Connect ${site} first`}</span>
        </span>
      </label>
    )
  }

  const typed = from === 'own' || ownPicked
  return (
    <div className="w-full">
      <div role="radiogroup" aria-label="Name to use" className="grid gap-2.5 md:grid-cols-3">
        {site('anilist', 'My AniList name', 'AniList')}
        {site('mal', 'My MyAnimeList name', 'MyAnimeList')}
        <div className={cx(card, typed ? 'border-accent-500 bg-accent-500/10' : 'border-base-800 bg-base-900')}>
          <input
            type="radio"
            name="name-from"
            aria-label="A name of my own"
            checked={typed}
            disabled={!host}
            onChange={() => {
              if (name.trim()) choose.mutate({ from: 'own' })
              else setOwnPicked(true)
              own.current?.focus()
            }}
            className={radio}
          />
          <span className="min-w-0 flex-1">
            <span className="block text-sm font-semibold text-white">A name of my own</span>
            <input
              ref={own}
              value={name}
              onChange={(e) => setName(e.target.value)}
              onFocus={() => setOwnPicked(true)}
              onBlur={() => {
                setOwnPicked(false)
                if (name.trim() !== saved) choose.mutate(name.trim() ? { from: 'own', name } : { name: '' })
              }}
              onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
              placeholder="Type it here"
              aria-label="My own name"
              maxLength={32}
              autoComplete="off"
              disabled={!host}
              className="mt-1 h-8 w-full rounded-lg bg-base-800 px-2.5 text-sm text-base-100 placeholder:text-base-500 focus:outline-hidden focus-visible:ring-2 focus-visible:ring-accent-400 disabled:opacity-50 pointer-coarse:h-11"
            />
          </span>
        </div>
      </div>
      {choose.isError && <p className="mt-2 text-xs text-recap">{(choose.error as Error).message}</p>}
    </div>
  )
}
