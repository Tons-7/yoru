import { useState } from 'react'
import { createPortal } from 'react-dom'
import { usePortalHome } from './keys'
import { useAccess, useAccessDevices, useDecideDevice } from '../lib/queries'
import { buttonClass } from '../lib/ui'
import { ConfirmInline } from './ui'

export function AccessRequests() {
  const access = useAccess()
  const devices = useAccessDevices(!!access.data?.host)
  const decide = useDecideDevice()
  const home = usePortalHome()
  const [declining, setDeclining] = useState<string | null>(null)

  const waiting = (devices.data?.devices ?? []).filter((d) => d.status === 'pending')
  if (!waiting.length) return null

  return createPortal(
    <div
      className="fixed right-4 bottom-4 z-90 flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2 max-sm:top-[calc(0.75rem+env(safe-area-inset-top))] max-sm:bottom-auto"
      role="alert"
    >
      {waiting.slice(0, 3).map((d) => (
        <div key={d.id} className="rounded-xl border border-white/10 bg-base-900 p-4 shadow-2xl">
          <p className="text-sm font-semibold text-white">A device wants to use Yoru</p>
          <p className="mt-1 text-xs text-base-300">
            {d.name} <span className="text-base-500">· {d.addr || 'through a proxy'}</span>
          </p>
          <div className="mt-3 flex flex-wrap gap-2">
            {declining === d.id ? (
              <ConfirmInline
                label="Decline"
                onConfirm={() => decide.mutate({ id: d.id, status: 'denied' }, { onSettled: () => setDeclining(null) })}
                onCancel={() => setDeclining(null)}
                pending={decide.isPending}
              />
            ) : (
              <>
                <button
                  onClick={() => decide.mutate({ id: d.id, status: 'approved' })}
                  disabled={decide.isPending}
                  className={buttonClass('primary')}
                >
                  Accept
                </button>
                <button onClick={() => setDeclining(d.id)} disabled={decide.isPending} className={buttonClass()}>
                  Decline
                </button>
              </>
            )}
          </div>
          {decide.isError && decide.variables?.id === d.id && (
            <p className="mt-2 text-xs text-red-300">{(decide.error as Error).message}</p>
          )}
        </div>
      ))}
      {waiting.length > 3 && (
        <p className="text-right text-xs text-base-400">+{waiting.length - 3} more in Settings</p>
      )}
    </div>,
    home,
  )
}
