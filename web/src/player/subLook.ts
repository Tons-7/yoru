export type SubLook = '' | 'outline' | 'yellow' | 'box' | 'yellow-box'

export const SUB_LOOKS: { value: SubLook; label: string }[] = [
  { value: '', label: 'As made' },
  { value: 'outline', label: 'White, outlined' },
  { value: 'yellow', label: 'Yellow, outlined' },
  { value: 'box', label: 'White on a box' },
  { value: 'yellow-box', label: 'Yellow on a box' },
]

export const subLookLabel = (look: string) => SUB_LOOKS.find((l) => l.value === look)?.label ?? 'As made'

export const asSubLook = (value: string | undefined): SubLook => SUB_LOOKS.find((l) => l.value === value)?.value ?? ''

const WHITE = 0xffffff00
const YELLOW = 0xffff0000
const BLACK = 0x00000000
const SHADE = 0x00000060

interface Styled {
  FontSize: number
  PrimaryColour: number
  OutlineColour: number
  BackColour: number
  BorderStyle: number
  Outline: number
  Shadow: number
  MarginV: number
  Alignment: number
}

const yellow = (look: SubLook) => look === 'yellow' || look === 'yellow-box'

export function restyle<T extends Styled>(s: T, look: SubLook, raised = false): T {
  const em = s.FontSize
  const placed = raised && s.Alignment <= 3 ? { ...s, MarginV: s.MarginV + em * 2 } : s
  switch (look) {
    case 'outline':
    case 'yellow':
      return {
        ...placed,
        PrimaryColour: yellow(look) ? YELLOW : WHITE,
        OutlineColour: BLACK,
        BackColour: SHADE,
        BorderStyle: 1,
        Outline: em * 0.07,
        Shadow: em * 0.03,
      }
    case 'box':
    case 'yellow-box':
      return { ...placed, PrimaryColour: yellow(look) ? YELLOW : WHITE, OutlineColour: SHADE, BackColour: SHADE, BorderStyle: 3, Outline: em * 0.12, Shadow: 0 }
  }
  return placed
}

export function castStyle(look: SubLook, scale: number) {
  const base = { fontScale: scale }
  const foregroundColor = yellow(look) ? '#FFFF00FF' : '#FFFFFFFF'
  switch (look) {
    case 'outline':
    case 'yellow':
      return { ...base, foregroundColor, edgeType: 'OUTLINE', edgeColor: '#000000FF', backgroundColor: '#00000000' }
    case 'box':
    case 'yellow-box':
      return { ...base, foregroundColor, edgeType: 'NONE', backgroundColor: '#0000009F' }
  }
  return base
}

export function previewStyle(look: SubLook): React.CSSProperties {
  const color = yellow(look) ? '#ffff00' : '#ffffff'
  switch (look) {
    case 'outline':
    case 'yellow':
      return { color, textShadow: '0 0 2px #000, 0 0 2px #000, 0 0 2px #000, 1px 1px 2px #000' }
    case 'box':
    case 'yellow-box':
      return { color, background: 'rgb(0 0 0 / 0.62)', padding: '0.1em 0.4em', borderRadius: '2px' }
  }
  return { color: '#ffffff', textShadow: '0 0 3px #000, 1px 1px 2px #000' }
}
