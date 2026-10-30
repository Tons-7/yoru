import { useNavigate } from 'react-router-dom'
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
    case 'failed':
      return "Couldn't save"
    default:
      return 'Saving…'
  }
}

export function SaveToDevice({ animeId, episode, variant }: { animeId: number; episode: number; variant?: ButtonVariant }) {
  const access = useAccess()
  const navigate = useNavigate()
  const { saved, jobs, status } = useSaved()
  if (!access.data || access.data.host) return null
  const key = savedKey(animeId, episode)
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
    <Button
      variant={variant}
      onClick={() => {
        if (cannotSave()) navigate('/saved')
        else saveEpisodes([{ animeId, episode }])
      }}
      title="Watch it here with the PC off"
    >
      Save to this device
    </Button>
  )
}
