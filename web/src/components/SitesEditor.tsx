import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import { useHeldData } from '../lib/hooks'
import { useAccess } from '../lib/queries'
import { toast } from '../lib/toast'
import { Badge, Button, ErrorState, FilterToggle, Select, Skeleton, TextInput } from './ui'
import { iconButton as railButton } from './DownloadsBits'

export interface Site {
  type: string
  url: string
  adult: boolean
}

interface SitesState {
  sites: Array<Site & { problem?: string }>
  saved: boolean
  configPath: string
  kinds: string[]
}

const KIND_LABEL: Record<string, string> = { nyaa: 'Nyaa-style', tokyotosho: 'TokyoTosho' }

const iconButton = `${railButton} disabled:opacity-40`

export function SitesEditor() {
  const qc = useQueryClient()
  const sites = useQuery({ queryKey: ['sites'], queryFn: () => api.get<SitesState>('/api/sites') })
  const access = useAccess()
  const host = !!access.data?.host

  const save = useMutation({
    meta: { inline: true },
    mutationFn: (list: Site[]) => api.put<SitesState>('/api/sites', { sites: list }),
    onSuccess: (data) => {
      qc.setQueryData(['sites'], data)
      void qc.invalidateQueries({ queryKey: ['setup'] })
    },
  })

  if (sites.isPending) return <Skeleton className="h-24 w-full" />
  if (sites.isError && !sites.data) return <ErrorState error={sites.error} retry={() => void sites.refetch()} />

  const rows = sites.data.sites
  const usable = (from: typeof rows): Site[] => from.filter((s) => !s.problem).map(({ type, url, adult }) => ({ type, url, adult }))
  const list = usable(rows)
  const remove = (i: number) => {
    const gone = rows[i]
    save.mutate(usable(rows.filter((_, j) => j !== i)), {
      onSuccess: () =>
        toast(`Removed ${gone.url}`, 'info', {
          label: 'Undo',
          run: () => {
            const now = usable(qc.getQueryData<SitesState>(['sites'])?.sites ?? [])
            if (now.some((s) => s.type === gone.type && s.url === gone.url)) return
            const { type, url, adult } = gone
            save.mutate([...now.slice(0, i), { type, url, adult }, ...now.slice(i)])
          },
        }),
    })
  }
  const raise = (i: number) => save.mutate(usable([...rows.slice(0, i - 1), rows[i], rows[i - 1], ...rows.slice(i + 1)]))

  return (
    <div className="space-y-3 py-3">
      {rows.length === 0 ? (
        <p className="text-sm text-base-400">
          Yoru ships with no torrent sites. Add one below: its feed type and address.
        </p>
      ) : (
        <ul className="space-y-2">
          {rows.map((site, i) => (
            <li key={`${i} ${site.type} ${site.url} ${site.adult}`} className="flex flex-wrap items-center gap-2 rounded-lg bg-base-850/60 px-3 py-2">
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm text-base-100" title={site.url}>
                  {site.url}
                </p>
                <p className="text-xs text-base-500">
                  {KIND_LABEL[site.type] ?? site.type}
                  {i === 0 && rows.length > 1 ? ' · first, so its record wins a release several sites carry' : ''}
                </p>
                {site.problem && (
                  <p className="mt-0.5 text-xs text-red-300">{site.problem}. Left out when the list is next saved.</p>
                )}
              </div>
              {site.adult && <Badge tone="recap">Adult titles only</Badge>}
              {host && (
                <div className="flex shrink-0 items-center gap-1.5">
                  <button
                    onClick={() => raise(i)}
                    disabled={i === 0 || save.isPending}
                    aria-label={`Move ${site.url} up`}
                    title="Move up"
                    className={iconButton}
                  >
                    ↑
                  </button>
                  <button
                    onClick={() => remove(i)}
                    disabled={save.isPending}
                    aria-label={`Remove ${site.url}`}
                    title="Remove"
                    className={iconButton}
                  >
                    ✕
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      {!sites.data.saved && rows.length > 0 && (
        <p className="text-xs text-base-500">
          From {sites.data.configPath}. A change here moves the list into Yoru, and config.toml's blocks are then ignored.
        </p>
      )}
      {save.isError && <p className="text-xs text-red-300">{(save.error as Error).message}</p>}

      {host ? (
        <AddSite
          kinds={sites.data.kinds}
          adding={save.isPending}
          onAdd={(site, added) => save.mutate([...list, site], { onSuccess: added })}
        />
      ) : (
        access.data && <p className="text-xs text-base-500">Change sites from the computer Yoru runs on.</p>
      )}
    </div>
  )
}

function AddSite({
  kinds,
  adding,
  onAdd,
}: {
  kinds: string[]
  adding: boolean
  onAdd: (site: Site, added: () => void) => void
}) {
  const [type, setType] = useState(kinds[0] ?? 'nyaa')
  const [url, setUrl] = useState('')
  const [adult, setAdult] = useState(false)
  const site: Site = { type, url: url.trim(), adult: adult && type === 'nyaa' }

  const test = useMutation({
    meta: { inline: true },
    mutationFn: (s: Site) => api.post<{ count: number; ms: number }>('/api/sites/test', s),
  })
  const tested = test.variables && test.variables.url === site.url && test.variables.type === site.type
  const result = useHeldData(test)

  return (
    <form
      className="flex flex-wrap items-center gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        if (!site.url) return
        onAdd(site, () => {
          setUrl('')
          test.reset()
        })
      }}
    >
      <Select value={type} onChange={setType} aria-label="Feed type">
        {kinds.map((k) => (
          <option key={k} value={k}>
            {KIND_LABEL[k] ?? k}
          </option>
        ))}
      </Select>
      <TextInput
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        placeholder="https://…"
        aria-label="Site address"
        inputMode="url"
        className="min-w-48 flex-1"
      />
      {type === 'nyaa' && (
        <FilterToggle on={adult} onChange={setAdult}>
          Adult titles only
        </FilterToggle>
      )}
      <Button onClick={() => test.mutate(site)} disabled={!site.url || test.isPending}>
        {test.isPending ? 'Testing…' : 'Test'}
      </Button>
      <Button type="submit" variant="primary" disabled={!site.url || adding}>
        {adding ? 'Adding…' : 'Add'}
      </Button>
      {tested && result && (
        <p className="basis-full text-xs text-accent-300">
          {result.count} result{result.count === 1 ? '' : 's'} · {(result.ms / 1000).toFixed(1)} s
        </p>
      )}
      {tested && test.isError && (
        <p className="flex basis-full flex-wrap items-center gap-2 text-xs text-red-300">
          <span>{(test.error as Error).message}</span>
          <Button type="button" size="sm" onClick={() => test.mutate(site)}>
            Try again
          </Button>
        </p>
      )}
    </form>
  )
}
