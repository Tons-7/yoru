import { Select } from './ui'
import { waitLabel } from '../lib/format'

// How long auto-download holds a new episode for a preferred group or the top resolution; "0" takes the first.
const WAITS: ReadonlyArray<{ value: string; label: string }> = [
  { value: '0', label: "Don't wait" },
  { value: '1', label: 'Up to 1 hour' },
  { value: '3', label: 'Up to 3 hours' },
  { value: '6', label: 'Up to 6 hours' },
  { value: '12', label: 'Up to 12 hours' },
]

/** The wait, or with inherit set the choice to follow Settings (value ""), named with what Settings say. */
export function WaitSelect({
  value,
  onChange,
  inherit,
}: {
  value: string
  onChange: (value: string) => void
  inherit?: string
}) {
  return (
    <Select value={value} onChange={onChange} aria-label="Wait for a better release">
      {inherit !== undefined && (
        <option value="">As in Settings ({inherit === '0' ? "don't wait" : waitLabel(inherit)})</option>
      )}
      {WAITS.map((w) => (
        <option key={w.value} value={w.value}>
          {w.label}
        </option>
      ))}
    </Select>
  )
}
