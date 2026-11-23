import { toast } from '../lib/toast'
import type { ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { cannotSave, retrySave, saveEpisodes, savedKey, useSaved, type SaveStatus } from '../lib/offline'
import { useAccess } from '../lib/queries'
import type { ButtonVariant } from '../lib/ui'
import { Button, LinkButton } from './ui'

function saveWords(s?: SaveStatus): string {
  switch (s?.phase) {
    case 'saving':
      return `Saving ${s.total ? Math.floor((s.done / s.total) * 100) : 0}%`
    case 'preparing':
      return 'Getting it ready'
    case 'waiting':
      return `Saving once the PC has it${s.percent != null ? ` · ${Math.floor(s.percent)}%` : ''}`
    case 'stopped':
      return 'Save stopped'
    case 'paused':
      return 'Save paused'
    case 'failed':
      return "Couldn't save"
    default:
      return 'Saving…'
  }
}

function Label({ icon, long, short }: { icon?: ReactNode; long: string; short: string }) {
  return (
    <>
      {icon}
      <span className="max-sm:hidden">{long}</span>
      <span className="leading-tight sm:hidden">{short}</span>
    </>
  )
}

export function SaveToDevice({
  animeId,
  episode,
  variant,
  className,
  icon,
}: {
  animeId: number
  episode: number
  variant?: ButtonVariant
  className?: string
  icon?: ReactNode
}) {
  const access = useAccess()
  const navigate = useNavigate()
  const { saved, jobs, status } = useSaved()
  if (!access.data || access.data.host) return null
  const key = savedKey(animeId, episode)
  const save = () => {
    const why = cannotSave()
    if (why) {
      toast(why, 'info', { label: 'How to', run: () => navigate('/saved') })
      return
    }
    saveEpisodes([{ animeId, episode }])
  }

  if (className) {
    if (saved.some((e) => e.key === key)) {
      return (
        <Link to="/saved" className={`${className} text-emerald-300`}>
          <Label icon={icon} long="On this device" short="Saved" />
        </Link>
      )
    }
    if (status.get(key)?.phase === 'failed') {
      return (
        <button type="button" className={`${className} text-red-300`} onClick={() => retrySave(key)}>
          <Label icon={icon} long="Couldn't save · Try again" short="Try again" />
        </button>
      )
    }
    if (jobs.some((j) => savedKey(j.animeId, j.episode) === key)) {
      return (
        <Link to="/saved" className={className}>
          <Label icon={icon} long={saveWords(status.get(key))} short="Saving" />
        </Link>
      )
    }
    return (
      <button type="button" className={className} onClick={save} title="Watch it here with the PC off">
        <Label icon={icon} long="Save to this device" short="Save here" />
      </button>
    )
  }

  if (saved.some((e) => e.key === key)) {
    return (
      <LinkButton to="/saved" variant={variant}>
        On this device
      </LinkButton>
    )
  }
  if (status.get(key)?.phase === 'failed') {
    return (
      <Button variant={variant} onClick={() => retrySave(key)}>
        Couldn't save · Try again
      </Button>
    )
  }
  if (jobs.some((j) => savedKey(j.animeId, j.episode) === key)) {
    return (
      <LinkButton to="/saved" variant={variant}>
        {saveWords(status.get(key))}
      </LinkButton>
    )
  }
  return (
    <Button variant={variant} onClick={save} title="Watch it here with the PC off">
      Save to this device
    </Button>
  )
}
