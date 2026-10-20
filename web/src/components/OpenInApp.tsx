import { useCallback } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import { copyText } from '../lib/downloads'
import { cx } from '../lib/format'
import { toast } from '../lib/toast'
import { useDismiss, useModalFocus } from '../lib/hooks'
import { buttonClass } from '../lib/ui'

interface ExternalLink {
  url: string
  name: string
  expires: string
}

/** How each app is asked to play a link: Android lists every video app; iOS has no chooser, so two by name. */
function appLinks(link: ExternalLink): { label: string; href: string }[] {
  const ua = navigator.userAgent
  if (/Android/i.test(ua)) {
    const u = new URL(link.url)
    const scheme = u.protocol.replace(':', '')
    return [
      {
        label: 'Choose an app',
        href: `intent://${u.host}${u.pathname}#Intent;scheme=${scheme};type=video/*;S.title=${encodeURIComponent(link.name)};end`,
      },
    ]
  }
  // iPads say Macintosh; touch gives them away.
  if (/iPhone|iPad|iPod/i.test(ua) || (/Macintosh/i.test(ua) && navigator.maxTouchPoints > 1)) {
    const url = encodeURIComponent(link.url)
    return [
      { label: 'VLC', href: `vlc-x-callback://x-callback-url/stream?url=${url}` },
      { label: 'Infuse', href: `infuse://x-callback-url/play?url=${url}` },
    ]
  }
  return []
}

/** Hands the episode's own file to a video app: VLC and mpv draw its subtitles as made, signs and all. */
export function OpenInApp({ source, open, onClose }: { source: string; open: boolean; onClose: () => void }) {
  const close = useCallback(() => onClose(), [onClose])
  const panel = useDismiss<HTMLDivElement>(close)
  useModalFocus(panel, open)
  // Made when the sheet opens, so each choice is a plain link the tap follows (a phone won't hand off else).
  // Fresh every time: a restart of Yoru ends every link made before it.
  const link = useQuery({
    queryKey: ['external-link', source],
    queryFn: () => api.post<ExternalLink>('/api/stream/external', { source }),
    enabled: open,
    staleTime: 0,
  })

  if (!open) return null
  const apps = link.data ? appLinks(link.data) : []
  // copyText: a phone on plain http has no clipboard API, which the old way still covers.
  const copy = async () => {
    if (await copyText(link.data!.url, panel.current ?? undefined)) toast('Link copied', 'info')
    else toast('Copy the link from the box instead; this page may not use the clipboard')
  }

  return (
    <div className="fixed inset-0 z-50 flex items-end justify-center sm:items-center sm:p-4">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label="Open in another app"
        className="animate-fade-in relative w-full max-w-md rounded-t-xl border border-base-800 bg-base-900 p-4 shadow-2xl sm:rounded-xl"
      >
        <div className="mb-3 flex items-start justify-between gap-4">
          <div>
            <h2 className="text-base font-semibold text-white">Open in another app</h2>
            <p className="mt-0.5 text-xs text-base-500">
              Plays the original file, subtitles and all. The link works for 12 hours. Another app can't tell Yoru how
              far you got, so mark the episode watched when you're done.
            </p>
          </div>
          <button
            onClick={close}
            aria-label="Close"
            className="min-h-11 min-w-11 rounded-md text-base-400 hover:bg-base-800 hover:text-base-100"
          >
            ✕
          </button>
        </div>

        {link.isPending || link.isFetching ? (
          <p className="py-3 text-sm text-base-400">Making a link…</p>
        ) : link.isError ? (
          <div className="flex items-center justify-between gap-3 py-2">
            <p className="text-sm text-recap">{(link.error as Error).message}</p>
            <button onClick={() => link.refetch()} className={buttonClass()}>
              Retry
            </button>
          </div>
        ) : (
          <div className="space-y-2">
            {apps.map((a) => (
              <a key={a.label} href={a.href} className={cx(buttonClass('primary'), 'flex min-h-11 w-full')}>
                {a.label}
              </a>
            ))}
            <div className="flex gap-2">
              <input
                readOnly
                aria-label="Link to the episode"
                value={link.data.url}
                onFocus={(e) => e.currentTarget.select()}
                className="min-h-11 min-w-0 flex-1 rounded-lg bg-base-950 px-3 font-mono text-xs text-base-300 ring-1 ring-white/[0.07]"
              />
              <button onClick={copy} className={cx(buttonClass(), 'min-h-11')}>
                Copy link
              </button>
            </div>
            {apps.length === 0 && (
              <p className="text-xs text-base-500">Paste it into VLC or mpv on any device that can reach this Yoru.</p>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
