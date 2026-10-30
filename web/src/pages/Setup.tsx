import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { cx } from '../lib/format'
import { useSetup } from '../lib/queries'
import { ComponentState } from '../components/ComponentState'
import { SitesEditor } from '../components/SitesEditor'
import { WhereThingsGo } from '../components/WhereThingsGo'
import { Badge, Button, ErrorState, LinkButton, PageHeader, Skeleton } from '../components/ui'

/** What Yoru needs before it can play anything, and what each piece is for. */
export function SetupPage() {
  const qc = useQueryClient()
  const { data, isPending, isError, error, refetch } = useSetup({
    // While anything is downloading, often enough for the bar to move.
    refetchInterval: (q) => {
      const setup = q.state.data
      if (!setup) return 5000
      const busy = (setup.progress ?? []).some(
        (p) => p.stage !== 'done' && p.stage !== 'failed',
      )
      return busy ? 1000 : setup.ready ? false : 5000
    },
  })

  const install = useMutation({
    meta: { inline: true },
    mutationFn: (name: string) => api.post(`/api/setup/install/${name}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['setup'] }),
  })

  if (isError && !data) return <ErrorState error={error} retry={() => refetch()} />
  if (isPending) return <Skeleton className="h-96 w-full" />

  // Defended, not trusted: a first run lands here, and a null list blanked the app.
  const components = data.components ?? []
  const missing = components.filter((c) => !c.present)
  // Only downloadable ones drive the bulk button; a manual one shows its command on its row.
  const fetchable = missing.filter((c) => !c.manual)
  const progressFor = (name: string) => (data.progress ?? []).find((p) => p.component === name)

  return (
    <div className="mx-auto max-w-3xl space-y-6">
      <PageHeader
        title="Setting up"
        subtitle="Yoru shells out to a few external programs. None are bundled: together they are far larger than the app itself."
      />

      <section className="surface overflow-hidden">
        <ul className="divide-y divide-white/[0.05]">
          {components.map((c) => (
            <li key={c.name} className="flex items-start gap-3 p-4">
              <span
                className={cx(
                  'mt-0.5 grid size-5 shrink-0 place-items-center rounded-full text-xs',
                  c.present ? 'bg-emerald-400/15 text-emerald-300' : 'bg-base-800 text-base-500',
                )}
              >
                {c.present ? '✓' : '·'}
              </span>

              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium text-base-100">{c.label}</span>
                  <span className="text-xs text-base-500">{c.size}</span>
                  {c.required && !c.present && <Badge tone="recap">Required</Badge>}
                  {!c.required && <Badge>{c.role ?? 'Optional'}</Badge>}
                  {c.version && <Badge tone="accent">{c.version}</Badge>}
                  {c.system && <Badge>On this system</Badge>}
                </div>
                <p className="mt-1 text-sm text-base-400">{c.purpose}</p>
                {c.problem && <p className="mt-1 text-xs text-recap">{c.problem}</p>}
                {c.needs && !components.find((x) => x.name === c.needs)?.present && (
                  <p className="mt-1 text-xs text-base-500">Needs {c.needs} to be useful.</p>
                )}
                <ComponentState
                  progress={progressFor(c.name)}
                  present={c.present}
                  manual={c.manual}
                  version={c.version}
                  latest={c.latest}
                  onInstall={() => install.mutate(c.name)}
                  pending={install.isPending && install.variables === c.name}
                  requestError={
                    install.isError && install.variables === c.name
                      ? (install.error as Error).message
                      : undefined
                  }
                />
              </div>
            </li>
          ))}
        </ul>
      </section>

      {data.temporary && (
        <section className="rounded-xl border border-recap/40 bg-recap/10 p-4 text-sm">
          <p className="font-semibold text-base-100">Yoru is running from a temporary folder</p>
          <p className="mt-1 text-base-300">
            That happens when yoru.exe is opened from inside the zip. Windows deletes
            the folder when the zip window closes, so settings saved beside it are
            lost each time. Extract the zip to a folder of its own and run yoru.exe
            from there.
          </p>
          <p className="mt-1 truncate text-xs text-base-500" title={data.configPath}>
            {data.configPath}
          </p>
        </section>
      )}

      {data.strayConfig && (
        <section className="rounded-xl border border-recap/40 bg-recap/10 p-4 text-sm">
          <p className="font-semibold text-base-100">There is a config.toml.txt next to config.toml</p>
          <p className="mt-1 text-base-300">
            Notepad adds .txt when saving as a new file, and Yoru only reads{' '}
            <code className="text-base-300">config.toml</code>. Move your changes into that
            file, delete the .txt one, and restart.
          </p>
          <p className="mt-1 truncate text-xs text-base-500" title={data.strayConfig}>
            {data.strayConfig}
          </p>
        </section>
      )}

      <section className="surface p-4">
        <h2 className="text-sm font-semibold text-base-100">Release sites</h2>
        <p className="mt-1 text-sm text-base-400">Where Yoru looks for episodes. Saved at once, no restart.</p>
        <SitesEditor />
      </section>

      {missing.length > 0 ? (
        <section className="surface p-4">
          <h2 className="text-sm font-semibold text-base-100">Fetch what is missing</h2>
          <p className="mt-1 text-sm text-base-400">
            Downloaded from the same places a build uses, into{' '}
            <code className="text-base-300">{data.binDir}</code>.
          </p>
          <div className="mt-3 flex flex-wrap items-center gap-3">
            <Button
              variant="primary"
              onClick={() => fetchable.forEach((c) => install.mutate(c.name))}
              disabled={fetchable.length === 0}
            >
              Install {fetchable.length === 1 ? fetchable[0].label.toLowerCase() : 'everything missing'}
            </Button>
            {/* Always a way out: most are optional, and installs can fail. */}
            <Link to="/" className="text-sm text-base-400 transition-colors hover:text-base-100">
              Skip for now
            </Link>
          </div>
        </section>
      ) : (
        <section className="rounded-xl border border-accent-500/30 bg-accent-500/5 p-4">
          <p className="text-sm text-base-100">
            Everything is installed.
            {data.indexers === 0 && ' Add a release site above to be able to play anything.'}
          </p>
          <LinkButton to="/" variant="primary" className="mt-3">
            Start watching
          </LinkButton>
        </section>
      )}

      <section className="surface p-4 text-sm">
        <h2 className="mb-2 font-semibold text-base-100">Where things go</h2>
        <WhereThingsGo setup={data} />
        <p className="mt-3 text-xs text-base-500">
          Watched episodes stay cached so a rewatch is instant; the oldest are
          removed once the budget is reached. Both are adjustable in{' '}
          <Link to="/settings" className="text-accent-300 hover:underline">
            settings
          </Link>
          .
        </p>
      </section>
    </div>
  )
}

