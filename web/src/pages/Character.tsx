import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { api } from '../lib/api'
import { personName } from '../lib/format'
import { useDocumentTitle } from '../lib/hooks'
import { ErrorState } from '../components/ui'
import {
  Face,
  ListBadge,
  PeopleHeader,
  PeopleSection,
  PeopleSkeleton,
  PortraitCard,
  PortraitGrid,
} from '../components/People'
import { showPath, type PeopleShow } from '../lib/people'

interface CharacterData {
  id: number
  name: string
  native?: string
  image?: string
  about?: string
  favorites?: number
  appearances: Array<{ role?: string; show: PeopleShow }>
  voices: Array<{ id?: number; name: string; image?: string; language: string }>
}

/** A character: who voiced them, and the shows they're in, the viewer's first. */
export function CharacterPage() {
  const id = Number(useParams().characterId)
  const character = useQuery({
    queryKey: ['character', id],
    queryFn: () => api.get<CharacterData>(`/api/character/${id}`),
    enabled: id > 0,
    staleTime: 12 * 60 * 60_000,
  })
  const c = character.data
  useDocumentTitle(c ? personName(c.name) : undefined)

  if (character.isPending) return <PeopleSkeleton />
  if (!c) return <ErrorState error={character.error} retry={() => void character.refetch()} />

  const facts = [
    c.appearances.length ? `In ${c.appearances.length} ${c.appearances.length === 1 ? 'show' : 'shows'}` : '',
    c.favorites ? `♥ ${c.favorites.toLocaleString()}` : '',
  ].filter(Boolean)

  return (
    <div className="space-y-10">
      <PeopleHeader image={c.image} name={c.name} native={c.native} facts={facts} about={c.about} />
      <PeopleSection title="Voiced by" items={c.voices} cap={8}>
        {(voices) => (
          <ul className="grid gap-2 sm:flex sm:flex-wrap">
            {voices.map((v) => {
              const body = (
                <>
                  <Face image={v.image} name={personName(v.name)} className="size-10" />
                  <span className="min-w-0">
                    <span className="block truncate text-sm font-medium text-base-100">{personName(v.name)}</span>
                    <span className="block text-xs text-base-500">{v.language}</span>
                  </span>
                </>
              )
              const row = 'flex min-w-0 items-center gap-3 rounded-xl bg-base-900/60 py-2 pr-4 pl-2 ring-1 ring-white/[0.06]'
              return (
                <li key={`${v.id ?? v.name}-${v.language}`}>
                  {v.id ? (
                    <Link to={`/person/${v.id}`} className={`${row} transition-colors hover:bg-base-850`}>
                      {body}
                    </Link>
                  ) : (
                    <div className={row}>{body}</div>
                  )}
                </li>
              )
            })}
          </ul>
        )}
      </PeopleSection>
      <PeopleSection title="Appears in" items={c.appearances}>
        {(list) => (
          <PortraitGrid>
            {list.map((a) => (
              <PortraitCard
                key={a.show.malId}
                to={showPath(a.show)}
                image={a.show.image}
                name={a.show.title}
                badge={<ListBadge status={a.show.status} />}
                lines={3}
                under={
                  a.show.year || a.role === 'Supporting' ? (
                    <p className="mt-0.5 text-xs text-base-500">
                      {[a.show.year, a.role === 'Supporting' ? 'Supporting' : ''].filter(Boolean).join(' · ')}
                    </p>
                  ) : null
                }
              />
            ))}
          </PortraitGrid>
        )}
      </PeopleSection>
    </div>
  )
}
