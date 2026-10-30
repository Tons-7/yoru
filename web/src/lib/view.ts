import { useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { readKey, writeKey } from './storage'

export interface Sides<T extends string> {
  store: string
  base: T
  alt: T
  names: Partial<Record<string, T>>
  implied?: (params: URLSearchParams) => T | null
}

export function useSide<T extends string>({ store, base, alt, names, implied }: Sides<T>): [T, (next: T) => void] {
  const [params, setParams] = useSearchParams()
  const asked = names[params.get('view') ?? '']
  const side = asked ?? implied?.(params) ?? (readKey(store) === alt ? alt : base)
  const unwritten = side === alt && params.get('view') !== alt

  useEffect(() => {
    if (!unwritten) return
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set('view', alt)
        return next
      },
      { replace: true },
    )
  }, [unwritten, alt, setParams])

  const choose = (next: T) => {
    writeKey(store, next)
    const p = new URLSearchParams(params)
    if (next === alt) p.set('view', alt)
    else p.delete('view')
    p.delete('page')
    setParams(p, { replace: true })
  }
  return [side, choose]
}
