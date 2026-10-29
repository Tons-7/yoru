import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { personName } from '../lib/format'
import { realImage } from '../lib/people'
import { usePrefs } from '../lib/queries'
import { Rail } from './Rail'
import { Skeleton } from './ui'

export interface Voice {
  /** Their MyAnimeList id, which their page opens by. */
  id?: number
  name: string
  image?: string
  language: string
}

export interface Character {
  id: number
  name: string
  image?: string
  role?: string
  favorites: number
  voices?: Voice[]
}

function useCharacters(animeId: number | undefined) {
  return useQuery({
    enabled: !!animeId,
    queryKey: ['characters', animeId],
    queryFn: () => api.get<{ items: Character[] }>(`/api/anime/${animeId}/characters`),
    // A cast does not change once a show has aired.
    staleTime: 12 * 60 * 60_000,
  })
}

/** Who is in the show and who played them; MyAnimeList records one cast per series. */
export function CharacterRail({
  animeId,
  limit = 24,
  title = 'Cast',
}: {
  animeId: number
  limit?: number
  title?: string
}) {
  const { data, isPending } = useCharacters(animeId)
  // The show's own sub/dub choice, over the global one.
  const prefs = usePrefs(animeId)
  const [expanded, setExpanded] = useState(false)

  // Follow the performance being watched, else the original cast (most shows were never dubbed).
  const dubbed = prefs.data?.effective['audio.prefer'] === 'dub'
  const voiceOf = (c: Character) => {
    const voices = c.voices ?? []
    if (dubbed) return voices.find((v) => v.language === 'English') ?? voices[0]
    return voices[0]
  }

  // Capped (Conan lists two thousand), but generously, as the rail scrolls.
  const cast = data?.items ?? []
  const shown = expanded ? cast.slice(0, 120) : cast.slice(0, limit)

  if (isPending) {
    return (
      <Rail title={title}>
        {Array.from({ length: limit }, (_, i) => (
          <Skeleton key={i} className="h-[174px] w-[116px] shrink-0 rounded-card" />
        ))}
      </Rail>
    )
  }
  if (cast.length === 0) return null

  return (
    <Rail
      title={title}
      action={
        cast.length > limit && (
          <button
            onClick={() => setExpanded(!expanded)}
            className="rounded-md px-2 py-1 text-sm text-base-400 transition-colors hover:bg-base-850 hover:text-white"
          >
            {expanded ? 'Show less' : cast.length > 120 ? 'Show more' : `Show all ${cast.length}`}
          </button>
        )
      }
    >
      {/* One poster-shaped portrait each, the actor's face a small avatar on it. */}
      {/* Two ways in: the character (picture and name) and who voiced them (face and name). */}
      {shown.map((c) => {
        const voice = voiceOf(c)
        const name = personName(c.name)
        const voiceName = voice && personName(voice.name)
        return (
          <div key={c.id} className="group/cast w-[116px] shrink-0 snap-start">
            <div className="relative overflow-hidden rounded-card shadow-card">
              <Link to={`/character/${c.id}`} tabIndex={-1} aria-hidden className="block">
                {realImage(c.image) ? (
                  <img
                    src={c.image}
                    alt=""
                    loading="lazy"
                    className="h-[174px] w-[116px] object-cover transition-transform duration-300 group-hover/cast:scale-105"
                  />
                ) : (
                  <div className="h-[174px] w-[116px] bg-base-850" />
                )}
              </Link>
              {/* Only "Main" earns a badge: SUPPORTING on nine cards in ten says nothing. */}
              {c.role?.toUpperCase() === 'MAIN' && (
                <span className="pointer-events-none absolute top-1.5 left-1.5 rounded-full bg-accent-500/85 px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-white uppercase">
                  Main
                </span>
              )}
              {realImage(voice?.image) && voice?.id && (
                <Link
                  to={`/person/${voice.id}`}
                  tabIndex={-1}
                  aria-hidden
                  title={`${voiceName} · ${voice.language}`}
                  className="tap absolute right-1.5 bottom-1.5 rounded-full ring-2 ring-base-950/80 transition-transform hover:scale-110"
                >
                  <img src={voice.image} alt="" loading="lazy" className="size-9 rounded-full object-cover" />
                </Link>
              )}
            </div>
            <Link
              to={`/character/${c.id}`}
              className="mt-2 line-clamp-2 text-[13px] leading-snug font-medium text-base-100 transition-colors hover:text-white"
              title={name}
            >
              {name}
            </Link>
            {voice &&
              (voice.id ? (
                <Link
                  to={`/person/${voice.id}`}
                  aria-label={`${voiceName}, ${voice.language} voice`}
                  className="line-clamp-2 text-xs leading-snug text-base-500 transition-colors hover:text-base-200"
                  title={`${voiceName} · ${voice.language}`}
                >
                  {voiceName}
                </Link>
              ) : (
                <p className="line-clamp-2 text-xs leading-snug text-base-500" title={`${voiceName} · ${voice.language}`}>
                  {voiceName}
                </p>
              ))}
          </div>
        )
      })}
    </Rail>
  )
}
