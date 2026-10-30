export type SkipMode = 'off' | 'button' | 'auto'

export const SKIP_MODES: ReadonlyArray<{ value: SkipMode; label: string }> = [
  { value: 'off', label: 'Off' },
  { value: 'button', label: 'Button' },
  { value: 'auto', label: 'Auto' },
]

export function skipMode(value: string | undefined): SkipMode {
  if (value === 'auto' || value === 'true') return 'auto'
  if (value === 'off') return 'off'
  return 'button'
}
