import { createContext } from 'react'
import { cx } from './format'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md' | 'lg'

const VARIANTS: Record<ButtonVariant, string> = {
  primary: 'bg-accent-500 text-white hover:bg-accent-600',
  secondary: 'bg-base-800 text-base-100 hover:bg-base-750',
  ghost: 'text-base-300 hover:bg-base-800 hover:text-white',
  danger: 'bg-red-600 text-white hover:bg-red-700',
}

const SIZES: Record<ButtonSize, string> = {
  sm: 'h-8 rounded-lg px-3 text-xs',
  md: 'h-10 rounded-xl px-4 text-sm',
  lg: 'h-11 rounded-xl px-5 text-[15px] pointer-coarse:min-h-12',
}

export function buttonClass(variant: ButtonVariant = 'secondary', size: ButtonSize = 'md') {
  return cx(
    'press inline-flex shrink-0 items-center justify-center gap-1.5 font-medium whitespace-nowrap transition-[background-color,color,transform] focus:outline-hidden focus-visible:ring-2 focus-visible:ring-white focus-visible:ring-offset-2 focus-visible:ring-offset-base-950 disabled:pointer-events-none disabled:opacity-50 pointer-coarse:min-h-11',
    VARIANTS[variant],
    SIZES[size],
  )
}

/** Names an unlabelled control after the settings row holding it. */
export const ControlLabel = createContext<string | undefined>(undefined)
