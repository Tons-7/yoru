// How dialogue looks, over what the file says: signs and songs keep their typesetting whatever is chosen.

export type SubLook = '' | 'outline' | 'yellow' | 'box'

export const SUB_LOOKS: { value: SubLook; label: string }[] = [
  { value: '', label: 'As made' },
  { value: 'outline', label: 'White, outlined' },
  { value: 'yellow', label: 'Yellow, outlined' },
  { value: 'box', label: 'White on a box' },
]

export const subLookLabel = (look: string) => SUB_LOOKS.find((l) => l.value === look)?.label ?? 'As made'

// libass colours: 0xRRGGBBAA, where AA is how see-through (00 solid).
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
}

/** A dialogue style made over in a look; widths follow the font's size, as scripts are drawn at many sizes. */
export function restyle<T extends Styled>(s: T, look: SubLook): T {
  const em = s.FontSize
  switch (look) {
    case 'outline':
    case 'yellow':
      return {
        ...s,
        PrimaryColour: look === 'yellow' ? YELLOW : WHITE,
        OutlineColour: BLACK,
        BackColour: SHADE,
        BorderStyle: 1,
        Outline: em * 0.07,
        Shadow: em * 0.03,
      }
    case 'box':
      // An opaque box takes the outline colour, and its padding from the outline's width.
      return { ...s, PrimaryColour: WHITE, OutlineColour: SHADE, BackColour: SHADE, BorderStyle: 3, Outline: em * 0.12, Shadow: 0 }
  }
  return s
}

/** The same look for a TV's text track (the Cast SDK's TextTrackStyle fields). */
export function castStyle(look: SubLook, scale: number) {
  const base = { fontScale: scale }
  switch (look) {
    case 'outline':
    case 'yellow':
      return { ...base, foregroundColor: look === 'yellow' ? '#FFFF00FF' : '#FFFFFFFF', edgeType: 'OUTLINE', edgeColor: '#000000FF', backgroundColor: '#00000000' }
    case 'box':
      return { ...base, foregroundColor: '#FFFFFFFF', edgeType: 'NONE', backgroundColor: '#0000009F' }
  }
  return base
}
