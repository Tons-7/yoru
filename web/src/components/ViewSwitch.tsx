import type { ReactNode } from 'react'
import { cx } from '../lib/format'
import { PageHeader, Segmented } from './ui'

export function ViewSwitch<T extends string>({
  options,
  value,
  onChange,
  className,
}: {
  options: ReadonlyArray<{ value: T; label: string }>
  value: T
  onChange: (value: T) => void
  className?: string
}) {
  return <Segmented options={options} value={value} onChange={onChange} full className={cx('min-w-0', className)} />
}

export function SwitchHeader({
  title,
  meta,
  switcher,
  actions,
  beside = false,
  actionsBelow = false,
}: {
  title: string
  meta?: ReactNode
  switcher: ReactNode
  actions?: ReactNode
  beside?: boolean
  actionsBelow?: boolean
}) {
  if (beside) {
    return (
      <div className="flex flex-wrap items-end gap-4">
        <h1 className="page-title">{title}</h1>
        <div className="max-sm:basis-full">{switcher}</div>
        {actions && <div className="flex flex-wrap items-center gap-2 max-sm:basis-full sm:ml-auto">{actions}</div>}
      </div>
    )
  }
  return (
    <div className="flex flex-wrap items-end justify-between gap-x-3 gap-y-4">
      <PageHeader title={title} meta={meta} />
      {actions && (
        <div className={cx('flex flex-wrap items-center gap-2', actionsBelow && 'max-sm:order-last max-sm:basis-full')}>
          {actions}
        </div>
      )}
      <div className="basis-full">{switcher}</div>
    </div>
  )
}
