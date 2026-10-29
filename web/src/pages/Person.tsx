import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { api } from '../lib/api'
import { personName } from '../lib/format'
import { useDocumentTitle } from '../lib/hooks'
import { ErrorState } from '../components/ui'
import {
  ListBadge,
  PeopleHeader,
  PeopleSection,
  PeopleSkeleton,
  PortraitCard,
  PortraitGrid,
} from '../components/People'
import { showPath, type PeopleShow, type PersonRef } from '../lib/people'

interface Played {
  character: PersonRef
  role?: string
  shows: PeopleShow[]
}

interface PersonData {
  id: number
  name: string
  native?: string
  image?: string
  about?: string
  birthday?: string
  favorites?: number
  characters: Played[]
  work: Array<{ show: PeopleShow; positions: string[] }>
}

/** A voice actor or staff member: who they played, your shows first, and what else they worked on. */
export function Person() {
  const id = Number(useParams().personId)
  const person = useQuery({
    queryKey: ['person', id],
    queryFn: () => api.get<PersonData>(`/api/person/${id}`),
    enabled: id > 0,
    staleTime: 12 * 60 * 60_000,
  })
  const p = person.data
  useDocumentTitle(p ? personName(p.name) : undefined)

  if (person.isPending) return <PeopleSkeleton />
  if (!p) return <ErrorState error={person.error} retry={() => void person.refetch()} />

  // The server puts parts in the viewer's own shows first: those are the ones to recognise a voice by.
  const mine = p.characters.filter((c) => c.shows.some((s) => s.status))
  const others = p.characters.filter((c) => !c.shows.some((s) => s.status))
  // Headings rather than a badge on every card: main parts are most of what comes first.
  const leads = others.filter((c) => c.role === 'Main')
  const supporting = others.filter((c) => c.role !== 'Main')
  const born = p.birthday && new Date(`${p.birthday}T00:00`).toLocaleDateString(undefined, { dateStyle: 'medium' })
  const facts = [
    born ? `Born ${born}` : '',
    p.characters.length ? `${p.characters.length} ${p.characters.length === 1 ? 'role' : 'roles'}` : '',
    p.favorites ? `♥ ${p.favorites.toLocaleString()}` : '',
  ].filter(Boolean)

  const roles = (list: Played[]) => (
    <PortraitGrid>
      {list.map((c) => {
        const show = c.shows[0]
        return (
          <PortraitCard
            key={c.character.id}
            to={`/character/${c.character.id}`}
            image={c.character.image}
            name={personName(c.character.name)}
            badge={<ListBadge status={show?.status} />}
            under={
              show && (
                <Link to={showPath(show)} className="mt-0.5 line-clamp-2 text-xs leading-snug text-base-500 transition-colors hover:text-base-200">
                  {show.title}
                  {c.shows.length > 1 && <span className="text-base-600"> · +{c.shows.length - 1}</span>}
                </Link>
              )
            }
          />
        )
      })}
    </PortraitGrid>
  )

  return (
    <div className="space-y-10">
      <PeopleHeader image={p.image} name={p.name} native={p.native} facts={facts} about={p.about} />
      <PeopleSection title="In your shows" items={mine}>
        {roles}
      </PeopleSection>
      <PeopleSection title="Main roles" items={leads}>
        {roles}
      </PeopleSection>
      <PeopleSection title="Supporting roles" items={supporting}>
        {roles}
      </PeopleSection>
      <PeopleSection title="Staff work" items={p.work} cap={12}>
        {(work) => (
          <ul className="surface divide-y divide-white/[0.05] overflow-hidden">
            {work.map((w) => (
              <li key={w.show.malId}>
                <Link to={showPath(w.show)} className="flex items-center gap-3 px-3 py-2.5 transition-colors hover:bg-base-850/70">
                  {w.show.image ? (
                    <img src={w.show.image} alt="" loading="lazy" className="h-14 w-10 shrink-0 rounded object-cover" />
                  ) : (
                    <div className="h-14 w-10 shrink-0 rounded bg-base-850" />
                  )}
                  <div className="min-w-0 flex-1">
                    <p className="line-clamp-1 text-sm font-medium text-base-100">{w.show.title}</p>
                    <p className="mt-0.5 line-clamp-1 text-xs text-base-500">
                      {[w.positions.join(', '), w.show.year].filter(Boolean).join(' · ')}
                    </p>
                  </div>
                  <ListBadge status={w.show.status} />
                </Link>
              </li>
            ))}
          </ul>
        )}
      </PeopleSection>
    </div>
  )
}
