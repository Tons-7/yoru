import { useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api } from '../lib/api'
import { showLink } from '../lib/showLink'
import { Spinner } from '../components/ui'

// Where a pasted or shared AniList or MyAnimeList link lands (?url=, or the share target's title and text):
// its show's page, or a search for the words when there's no show link.
export function Open() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const shared = ['url', 'text', 'title'].map((k) => params.get(k) ?? '').join(' ').trim()
  const [problem, setProblem] = useState('')

  useEffect(() => {
    const link = showLink(shared)
    if (!link) {
      navigate(shared ? `/browse?q=${encodeURIComponent(shared)}` : '/', { replace: true })
      return
    }
    if (link.site === 'anilist') {
      navigate(`/anime/${link.id}`, { replace: true })
      return
    }
    let cancelled = false
    api
      .get<{ id: number }>(`/api/anime/from-mal?id=${link.id}`)
      .then(({ id }) => !cancelled && navigate(`/anime/${id}`, { replace: true }))
      .catch((e: Error) => !cancelled && setProblem(e.message))
    return () => {
      cancelled = true
    }
  }, [shared, navigate])

  return (
    <div className="mx-auto max-w-md py-16 text-center">
      {problem ? (
        <>
          <p className="text-base-200">{problem}</p>
          <Link to={`/browse?q=${encodeURIComponent(shared)}`} className="mt-3 inline-block text-accent-300 underline">
            Search for it instead
          </Link>
        </>
      ) : (
        <>
          <Spinner className="mx-auto size-8" />
          <p className="mt-3 text-sm text-base-400">Opening the show…</p>
        </>
      )}
    </div>
  )
}
