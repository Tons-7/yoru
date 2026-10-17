// The upscale modes Settings and the player's dialog both offer.
export const ANIME4K_MODES = [
  { id: 'A', title: 'A · Restore', hint: 'Sharpens soft, blurry lines. Safe default for most 1080p web releases.' },
  { id: 'B', title: 'B · Soft restore', hint: 'Gentler. Better when the source is noisy rather than blurry, since it will not harden compression artefacts.' },
  { id: 'C', title: 'C · Denoise only', hint: 'Upscales and denoises without touching linework. Cheapest, and the right pick for clean Blu-ray sources.' },
  { id: 'A+A', title: 'A+A · Restore twice', hint: 'Mode A run again. For heavily degraded or low-resolution sources like 480p.' },
  { id: 'B+B', title: 'B+B · Soft restore twice', hint: 'Mode B run again, for low-resolution sources that are noisy rather than blurry.' },
  { id: 'C+A', title: 'C+A · Denoise then restore', hint: 'Cleans the noise first, then rebuilds lines. For sources that are both noisy and soft.' },
] as const

// mpv only; the browser build ships fixed networks. Each step doubles GPU cost.
export const ANIME4K_SIZES = [
  { id: 'S', hint: 'Integrated graphics' },
  { id: 'M', hint: 'Balanced default' },
  { id: 'L', hint: 'Dedicated GPU' },
  { id: 'VL', hint: 'Strong GPU' },
  { id: 'UL', hint: 'High end only; diminishing returns' },
] as const
