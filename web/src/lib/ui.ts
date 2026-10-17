import { createContext } from 'react'
import { cx } from './format'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md' | 'lg'

// The brand colour leads the main action; white read as a generic button.
const VARIANTS: Record<ButtonVariant, string> = {
  primary:
    'bg-gradient-to-b from-accent-400 to-accent-500 text-white shadow-[0_6px_20px_-6px_rgb(111_92_255/0.6)] hover:brightness-110 active:scale-[0.98]',
  secondary:
    'bg-base-800 text-base-100 shadow-[inset_0_1px_0_rgb(255_255_255/0.05)] ring-1 ring-white/[0.07] hover:bg-base-750',
  ghost: 'text-base-300 hover:bg-base-850 hover:text-white',
  danger: 'bg-red-500/90 text-white hover:bg-red-500',
}

// The radius is the size's: a page's lead action (lg) is rounder than the rest.
const SIZES: Record<ButtonSize, string> = {
  sm: 'rounded-lg px-2.5 py-1 text-xs',
  md: 'rounded-lg px-4 py-2 text-sm',
  lg: 'rounded-xl px-5 py-2.5 text-sm',
}

/** Class list for something that should look like a button but is a link. */
export function buttonClass(variant: ButtonVariant = 'secondary', size: ButtonSize = 'md') {
  return cx(
    'inline-flex shrink-0 items-center justify-center gap-1.5 font-medium whitespace-nowrap transition-[background-color,color,transform,filter] focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-400 disabled:pointer-events-none disabled:opacity-50',
    VARIANTS[variant],
    SIZES[size],
  )
}

/** Names an unlabelled control after the settings row holding it. */
export const ControlLabel = createContext<string | undefined>(undefined)
