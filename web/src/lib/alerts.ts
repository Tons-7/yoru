import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, type AlertShow } from './api'
import { DESKTOP_NOTIFY_KEY, desktopNotifyWanted } from './notifications'
import { disablePush, enablePush, pushState } from './push'
import { useSetNotify, type Notification } from './queries'
import { toast } from './toast'

export function useAlertSwitch(animeId: number, title: string, onChange?: (on: boolean) => void) {
  const qc = useQueryClient()
  const set = useSetNotify(animeId)
  const change = (on: boolean, undo = true) => {
    onChange?.(on)
    set.mutate(on, {
      onSuccess: (state) => {
        qc.setQueryData(['alert', animeId], state.on)
        void qc.invalidateQueries({ queryKey: ['alert-shows'], refetchType: 'none' })
        if (!undo) return
        toast(
          on ? `Alerts on for ${title}. You'll hear when each new episode is out.` : `Alerts off for ${title}.`,
          'info',
          { label: 'Undo', run: () => change(!on, false) },
        )
      },
      onError: (e) => {
        onChange?.(!on)
        toast(`Couldn't ${on ? 'turn on' : 'turn off'} the alert for ${title}: ${e.message}`, 'error', {
          label: 'Try again',
          run: () => change(on, undo),
        })
      },
    })
  }
  return { change, pending: set.isPending, asked: set.isPending ? set.variables : undefined }
}

export function useAlertShows() {
  return useQuery({
    queryKey: ['alert-shows'],
    queryFn: () => api.get<{ enabled: boolean; shows: AlertShow[] }>('/api/alerts'),
    staleTime: 60_000,
  })
}

export type DeviceAlerts = 'on' | 'open-only' | 'off' | 'blocked' | 'home-screen'

export function useDeviceAlerts() {
  const qc = useQueryClient()
  const push = useQuery({ queryKey: ['push-state'], queryFn: pushState, staleTime: Infinity })
  const [wanted, setWanted] = useState(desktopNotifyWanted)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const set = async (next: boolean) => {
    setBusy(true)
    setError('')
    try {
      if (next && typeof Notification !== 'undefined' && Notification.permission !== 'granted') {
        if ((await Notification.requestPermission()) !== 'granted') return
      }
      try {
        localStorage.setItem(DESKTOP_NOTIFY_KEY, next ? '1' : '0')
      } catch {
        setError('This browser would not keep the choice')
      }
      setWanted(next)
      const can = push.data === 'on' || push.data === 'off'
      if (next && can) await enablePush()
      if (!next && can) await disablePush()
    } catch (e) {
      setError(next ? `Alerts show while Yoru is open, not with it closed: ${(e as Error).message}` : (e as Error).message)
    } finally {
      setBusy(false)
      void qc.invalidateQueries({ queryKey: ['push-state'] })
    }
  }

  let state: DeviceAlerts | undefined
  switch (push.data) {
    case undefined:
      break
    case 'install':
      state = 'home-screen'
      break
    case 'denied':
      state = 'blocked'
      break
    case 'on':
      state = 'on'
      break
    default:
      state = wanted ? 'open-only' : 'off'
  }
  return { state, push: push.data, busy, error, set }
}

export type FeedItem = Notification & { show?: string; cover?: string }

export function feedTitle(n: FeedItem, episodes: number[] = [n.episode]): string {
  if (n.kind !== 'release' || !n.show) return n.title
  const sorted = [...new Set(episodes)].sort((a, b) => a - b)
  if (sorted.length === 1) return `${n.show} · Ep ${sorted[0]} is out`
  const last = sorted[sorted.length - 1]
  const words = sorted.length === 2 ? `Ep ${sorted[0]} and ${last}` : `Ep ${sorted[0]} to ${last}`
  return `${n.show} · ${words} are out`
}
