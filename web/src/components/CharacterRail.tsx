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
    staleTime: 12 * 60 * 60_000,
  })
}

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
  const prefs = usePrefs(animeId)
  const [expanded, setExpanded] = useState(false)

  const dubbed = prefs.data?.effective['audio.prefer'] === 'dub'
  const voiceOf = (c: Character) => {
    const voices = c.voices ?? []
    if (dubbed) return voices.find((v) => v.language === 'English') ?? voices[0]
    return voices[0]
  }

  const cast = data?.items ?? []
  const shown = expanded ? cast.slice(0, 120) : cast.slice(0, limit)

  if (isPending) {
    return (
      <Rail title={title}>
        {Array.from({ length: 16 }, (_, i) => (
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
            type="button"
            onClick={() => setExpanded(!expanded)}
            className="tap relative px-1 py-1 text-sm font-medium text-base-400 transition-colors hover:text-white"
          >
            {expanded ? 'Show less' : cast.length > 120 ? 'Show more' : `Show all ${cast.length}`}
          </button>
        )
      }
    >
      {shown.map((c) => {
        const voice = voiceOf(c)
        const name = personName(c.name)
        const voiceName = voice && personName(voice.name)
        return (
          <div key={c.id} className="group/cast w-[116px] shrink-0 snap-start">
            <div className="relative overflow-hidden rounded-card bg-base-850">
              <Link to={`/character/${c.id}`} tabIndex={-1} aria-hidden className="block">
                {realImage(c.image) ? (
                  <img
                    src={c.image}
                    alt=""
                    loading="lazy"
                    className="h-[174px] w-[116px] object-cover transition-transform duration-300 group-hover/cast:scale-105"
                  />
                ) : (
                  <div className="h-[174px] w-[116px]" />
                )}
              </Link>
              {c.role?.toUpperCase() === 'MAIN' && (
                <span className="pointer-events-none absolute top-1.5 left-1.5 rounded-full bg-base-950/85 px-1.5 py-0.5 text-xs font-semibold text-base-100 backdrop-blur-sm">
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
                  className="line-clamp-2 text-[13px] leading-snug text-base-400 transition-colors hover:text-base-200"
                  title={`${voiceName} · ${voice.language}`}
                >
                  {voiceName}
                </Link>
              ) : (
                <p className="line-clamp-2 text-[13px] leading-snug text-base-400" title={`${voiceName} · ${voice.language}`}>
                  {voiceName}
                </p>
              ))}
          </div>
        )
      })}
    </Rail>
  )
}
