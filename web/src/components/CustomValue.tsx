import { useState, type ReactNode } from 'react'
import type { ValueRange } from '../lib/queries'
import { buttonClass } from '../lib/ui'
import { TextInput } from './ui'
import type { ValueOption } from './ValueMenu'

const CUSTOM = 'custom'

const shown = (n: number) => String(+n.toFixed(2))

export function useCustomValue({
  value,
  presets,
  label,
  unit,
  scale,
  range,
  onChange,
}: {
  value: string
  presets: readonly ValueOption[]
  label: (value: string) => string
  unit: string
  scale: number
  range?: ValueRange
  onChange: (value: string) => void
}): { options: ValueOption[]; pick: (value: string) => void; below: ReactNode } {
  const [text, setText] = useState<string | null>(null)
  const fitting = presets.filter(
    (o) => !range || (range.off && o.value === '0') || (Number(o.value) >= range.min && Number(o.value) <= range.max),
  )
  const known = !value || fitting.some((o) => o.value === value)
  const options = [
    ...(known ? fitting : [...fitting, { value, label: label(value) }].toSorted((a, b) => Number(a.value) - Number(b.value))),
    ...(range ? [{ value: CUSTOM, label: 'Custom…' }] : []),
  ]
  const pick = (next: string) => {
    if (next !== CUSTOM) {
      setText(null)
      onChange(next)
      return
    }
    setText(Number(value) > 0 ? shown(Number(value) / scale) : '')
  }

  if (text === null || !range) return { options, pick, below: undefined }
  const lo = Math.ceil((range.min / scale) * 100) / 100
  const hi = Math.floor((range.max / scale) * 100) / 100
  const n = Number(text.trim().replace(',', '.'))
  const filled = text.trim() !== ''
  const ok = filled && Number.isFinite(n) && n >= lo && n <= hi
  const save = () => {
    if (!ok) return
    setText(null)
    onChange(String(Math.round(n * scale)))
  }

  const below = (
    <form
      onClick={(e) => e.stopPropagation()}
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
      onKeyDown={(e) => {
        if (e.key === 'Escape') setText(null)
      }}
      className="flex flex-wrap items-center justify-end gap-2"
    >
      <TextInput
        autoFocus
        inputMode="decimal"
        enterKeyHint="done"
        aria-label={`Custom value in ${unit}`}
        aria-invalid={filled && !ok}
        value={text}
        onChange={(e) => setText(e.target.value)}
        className="w-24 text-right"
      />
      <span className="text-sm text-base-400">{unit}</span>
      <button type="submit" disabled={!ok} className={buttonClass('primary', 'sm')}>
        Save
      </button>
      <button type="button" onClick={() => setText(null)} className={buttonClass('ghost', 'sm')}>
        Cancel
      </button>
      <p className={filled && !ok ? 'basis-full text-right text-xs text-amber-300' : 'basis-full text-right text-xs text-base-500'}>
        {shown(lo)} to {shown(hi)} {unit}
      </p>
    </form>
  )
  return { options, pick, below }
}
