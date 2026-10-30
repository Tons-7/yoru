import type { ReactNode } from 'react'
import { BottomSheet } from './BottomSheet'

export function Sheet({
  open,
  onClose,
  title,
  subtitle,
  tall = false,
  side = true,
  footer,
  children,
}: {
  open: boolean
  onClose: () => void
  title: string
  subtitle?: ReactNode
  tall?: boolean
  side?: boolean
  footer?: ReactNode
  children: ReactNode
}) {
  if (!open) return null
  return (
    <BottomSheet panel title={title} detail={subtitle} onClose={onClose} tall={tall} side={side} footer={footer}>
      {children}
    </BottomSheet>
  )
}
