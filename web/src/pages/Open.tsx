import { useEffect, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import { showLink } from '../lib/showLink'
import { parseTogether, roomInfo } from '../lib/together'
import { Button, LinkButton, Spinner } from '../components/ui'

export function Open() {
  const [params] = useSearchParams()
  const navigate = useNavigate()
  const shared = ['url', 'text', 'title'].map((k) => params.get(k) ?? '').join(' ').trim()
  const [problem, setProblem] = useState('')

  useEffect(() => {
    const together = parseTogether(shared)
    if (together) {
      let cancelled = false
      roomInfo(together)
        .then(({ state }) => {
          if (cancelled) return
          const q = new URLSearchParams({ together: together.href, ...(state.infoHash ? { hash: state.infoHash } : {}) })
          navigate(`/watch/${state.animeId}/${state.episode}?${q}`, { replace: true })
        })
        .catch((e: Error) => !cancelled && setProblem(e.message))
      return () => {
        cancelled = true
      }
    }
    const link = showLink(shared)
    if (!link) {
      const words = ['text', 'title']
        .map((k) => params.get(k) ?? '')
        .join(' ')
        .replace(/https?:\/\/\S+/g, '')
        .trim()
      navigate(words ? `/browse?q=${encodeURIComponent(words)}` : '/', { replace: true })
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
      .catch((e: Error) => {
        if (cancelled) return
        if (e instanceof ApiError && e.status === 404) navigate(`/anime/-${link.id}`, { replace: true })
        else setProblem(e.message)
      })
    return () => {
      cancelled = true
    }
  }, [shared, params, navigate])

  return (
    <div className="mx-auto max-w-md py-16 text-center">
      {problem ? (
        <>
          <p className="text-base-200">{problem}</p>
          {parseTogether(shared) ? (
            <div className="mt-4 flex justify-center gap-2">
              <Button onClick={() => window.location.reload()}>Try again</Button>
              <LinkButton to="/">Go to Home</LinkButton>
            </div>
          ) : (
            <Link to={`/browse?q=${encodeURIComponent(shared)}`} className="mt-3 inline-block text-accent-300 underline">
              Search for it instead
            </Link>
          )}
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
