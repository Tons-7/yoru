import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import { copyText } from '../lib/downloads'
import { cx } from '../lib/format'
import { toast } from '../lib/toast'
import { useDismiss, useModalFocus } from '../lib/hooks'
import { buttonClass } from '../lib/ui'
import { appleTouchDevice } from '../lib/install'
import { CloseButton } from './ui'

interface ExternalLink {
  url: string
  name: string
  expires: string
}

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
  if (appleTouchDevice()) {
    const url = encodeURIComponent(link.url)
    return [
      { label: 'VLC', href: `vlc-x-callback://x-callback-url/stream?url=${url}` },
      { label: 'Infuse', href: `infuse://x-callback-url/play?url=${url}` },
    ]
  }
  return []
}

export function OpenInApp({ source, open, onClose }: { source: string; open: boolean; onClose: () => void }) {
  const panel = useDismiss<HTMLDivElement>(onClose, open)
  useModalFocus(panel, open)
  const link = useQuery({
    queryKey: ['external-link', source],
    queryFn: () => api.post<ExternalLink>('/api/stream/external', { source }),
    enabled: open,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: false,
  })

  if (!open) return null
  const apps = link.data ? appLinks(link.data) : []
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
        aria-label="Open or save"
        className="animate-fade-in relative w-full max-w-md rounded-t-xl border border-base-800 bg-base-900 p-4 shadow-2xl sm:rounded-xl"
      >
        <div className="mb-3 flex items-start justify-between gap-4">
          <div>
            <h2 className="text-base font-semibold text-white">Open or save</h2>
            <p className="mt-0.5 text-xs text-base-500">
              Plays the original file, subtitles and all, or keeps a copy to watch without Yoru. The link works for 12
              hours. Another app can't tell Yoru how far you got, so mark the episode watched when you're done.
            </p>
          </div>
          <CloseButton onClick={onClose} />
        </div>

        {link.isPending ? (
          <p className="py-3 text-sm text-base-400">Making a link…</p>
        ) : link.isError ? (
          <div className="flex items-center justify-between gap-3 py-2">
            <p className="text-sm text-red-300">{(link.error as Error).message}</p>
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
                className="min-h-11 min-w-0 flex-1 rounded-lg bg-base-800 px-3 font-mono text-xs text-base-300"
              />
              <button onClick={copy} className={cx(buttonClass(), 'min-h-11')}>
                Copy link
              </button>
            </div>
            {apps.length === 0 && (
              <p className="text-xs text-base-500">Paste it into VLC or mpv on any device that can reach this Yoru.</p>
            )}
            <div className="flex gap-2 pt-1">
              <a href={`${link.data.url}?download=1`} download={link.data.name} className={cx(buttonClass(), 'flex min-h-11 flex-1')}>
                Download the file
              </a>
              {appleTouchDevice() && (
                <a
                  href={`vlc-x-callback://x-callback-url/download?url=${encodeURIComponent(`${link.data.url}?download=1`)}&filename=${encodeURIComponent(link.data.name)}`}
                  className={cx(buttonClass(), 'flex min-h-11 flex-1')}
                >
                  Save to VLC
                </a>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
