import { Select } from './ui'
import { WAITS, waitLabel } from '../lib/format'

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
        <option value="">Default ({inherit === '0' ? "don't wait" : waitLabel(inherit)})</option>
      )}
      {WAITS.map((w) => (
        <option key={w.value} value={w.value}>
          {w.label}
        </option>
      ))}
    </Select>
  )
}
