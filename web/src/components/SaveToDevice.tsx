import type { ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { cannotSave, saveEpisodes, savedKey, useSaved, type SaveStatus } from '../lib/offline'
import { useAccess } from '../lib/queries'
import type { ButtonVariant } from '../lib/ui'
import { Button, LinkButton } from './ui'

function saveWords(s?: SaveStatus): string {
  switch (s?.phase) {
    case 'saving':
      return `Saving ${s.total ? Math.floor((s.done / s.total) * 100) : 0}%`
    case 'waiting':
      return 'Saving once the PC has it'
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
    if (cannotSave()) navigate('/saved')
    else saveEpisodes([{ animeId, episode }])
  }

  if (className) {
    if (saved.some((e) => e.key === key)) {
      return (
        <Link to="/saved" className={`${className} text-emerald-300`}>
          <Label icon={icon} long="On this device" short="Saved" />
        </Link>
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
