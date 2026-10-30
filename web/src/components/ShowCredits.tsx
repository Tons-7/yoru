import { useState } from 'react'
import { cx } from '../lib/format'
import { useAnimeExtra, type Credit, type Theme } from '../lib/queries'
import { PersonRow } from './People'

const ROW = 'sm:grid sm:grid-cols-[8rem_minmax(0,1fr)] sm:gap-x-4'
const LABEL = 'text-[13px] font-semibold text-base-400'
const MORE = 'tap relative mt-3 text-sm font-medium text-accent-300 hover:underline'

export function ShowCredits({ animeId }: { animeId: number }) {
  const { data } = useAnimeExtra(animeId)
  const openings = data?.openings ?? []
  const endings = data?.endings ?? []
  const staff = data?.staff ?? []
  const music = openings.length > 0 || endings.length > 0
  if (!music && staff.length === 0) return null

  return (
    <section className="space-y-3">
      <h2 className="section-title">Music & staff</h2>
      <div className={cx('surface grid gap-6 p-4 sm:p-5', music && staff.length > 0 && 'lg:grid-cols-2 lg:gap-8')}>
        {music && (
          <div className="space-y-4">
            <ThemeList label="Opening" themes={openings} />
            <ThemeList label="Ending" themes={endings} />
          </div>
        )}
        {staff.length > 0 && <StaffList staff={staff} />}
      </div>
    </section>
  )
}

function themeEpisodes(raw: string): string {
  const text = raw.trim()
  const from = /^(\d+)\s*-$/.exec(text)
  if (from) return `from Ep ${from[1]}`
  if (/^\d+$/.test(text)) return `Ep ${text}`
  const span = /^(\d+)\s*-\s*(\d+)$/.exec(text)
  if (span) return `Eps ${span[1]} to ${span[2]}`
  return `Eps ${text}`
}

function ThemeList({ label, themes }: { label: string; themes: Theme[] }) {
  const [all, setAll] = useState(false)
  if (themes.length === 0) return null

  const cap = 4
  const hidden = Math.max(0, themes.length - cap)
  const shown = all ? themes : themes.slice(-cap)
  const offset = all ? 0 : hidden

  return (
    <div>
      <ul className="space-y-3">
        {shown.map((t, i) => (
          <li key={`${t.title}-${i}`} className={ROW}>
            <p className={LABEL}>
              {label}
              {themes.length > 1 ? ` ${offset + i + 1}` : ''}
            </p>
            <p className="mt-0.5 text-base text-base-100 sm:mt-0 sm:text-sm">
              {t.title}
              {t.artist && <span className="text-base-400"> by {t.artist}</span>}
              {t.episodes && <span className="text-base-500 tabular-nums"> · {themeEpisodes(t.episodes)}</span>}
            </p>
          </li>
        ))}
      </ul>
      {hidden > 0 && (
        <button type="button" onClick={() => setAll((v) => !v)} className={MORE}>
          {all ? 'Show fewer' : `Show all ${themes.length}`}
        </button>
      )}
    </div>
  )
}

function roleLabel(role: string, count: number): string {
  const said = role.charAt(0).toUpperCase() + role.slice(1).toLowerCase()
  return count > 1 && /(or|er)$/.test(said) ? `${said}s` : said
}

function StaffList({ staff }: { staff: Credit[] }) {
  const [all, setAll] = useState(false)
  const roles = new Map<string, Credit[]>()
  for (const c of staff) roles.set(c.role, [...(roles.get(c.role) ?? []), c])
  const groups = [...roles.entries()]
  const cap = 5
  const shown = all ? groups : groups.slice(0, cap)

  return (
    <div>
      <ul className="space-y-3">
        {shown.map(([role, people]) => (
          <li key={role} className={ROW}>
            <p className={cx(LABEL, 'sm:pt-3')}>{roleLabel(role, people.length)}</p>
            <div className="-mx-2">
              {people.map((c) => (
                <PersonRow key={`${c.id ?? 0}-${c.name}`} id={c.id} name={c.name} image={c.image} />
              ))}
            </div>
          </li>
        ))}
      </ul>
      {groups.length > cap && (
        <button type="button" onClick={() => setAll((v) => !v)} className={MORE}>
          {all ? 'Show fewer' : 'Show all staff'}
        </button>
      )}
    </div>
  )
}
