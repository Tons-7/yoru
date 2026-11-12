import { useMutation, useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import { toast } from '../lib/toast'
import { buttonClass } from '../lib/ui'

export function BrowseFolder({
  title,
  start,
  onPick,
  disabled,
}: {
  title: string
  start?: string
  onPick: (path: string) => void
  disabled?: boolean
}) {
  const can = useQuery({
    queryKey: ['pick'],
    queryFn: () => api.get<{ available: boolean }>('/api/setup/pick'),
    staleTime: Infinity,
  })
  const pick = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<{ path?: string; cancelled?: boolean }>('/api/setup/pick', { title, start: start ?? '' }),
    onSuccess: (r) => r.path && onPick(r.path),
    onError: (e) => toast((e as Error).message),
  })
  if (!can.data?.available) return null
  return (
    <button type="button" onClick={() => pick.mutate()} disabled={disabled || pick.isPending} className={buttonClass()}>
      {pick.isPending ? 'Choosing…' : 'Browse…'}
    </button>
  )
}
