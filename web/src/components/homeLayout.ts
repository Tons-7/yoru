import { useCallback, useState } from 'react'
import { HOME_ROWS, hiddenRows, type HomeRow } from '../lib/homeRows'
import { usePrefs, useSetPref } from '../lib/queries'
import { readKey, writeKey } from '../lib/storage'

export const HOME_LAYOUT = HOME_ROWS

export type HomeRowId = HomeRow

const DEFAULT_ORDER: HomeRowId[] = HOME_LAYOUT.map((r) => r.id)
const FOLDED_KEY = 'yoru.home.folded'
const ORDER_KEY = 'yoru.home.order'

const readList = (key: string) => [...hiddenRows(readKey(key) ?? undefined)]

function arrange(saved: string[]): HomeRowId[] {
  const known = new Set<string>(DEFAULT_ORDER)
  const out = saved.filter((id): id is HomeRowId => known.has(id))
  DEFAULT_ORDER.forEach((id, i) => {
    if (out.includes(id)) return
    const before = DEFAULT_ORDER.slice(0, i).filter((b) => out.includes(b)).at(-1)
    out.splice(before ? out.indexOf(before) + 1 : 0, 0, id)
  })
  return out
}

export function homeRowLabel(id: HomeRowId): string {
  return HOME_LAYOUT.find((r) => r.id === id)?.label ?? id
}

export function useHomeLayout() {
  const prefs = usePrefs()
  const setPref = useSetPref()
  const settling =
    setPref.variables?.key === 'home.hidden' && (setPref.isPending || (setPref.isSuccess && prefs.isFetching))
  const hidden = hiddenRows(settling ? setPref.variables?.value : prefs.data?.effective['home.hidden'])
  const [folded, setFolded] = useState(() => new Set(readList(FOLDED_KEY)))
  const [order, setOrder] = useState(() => arrange(readList(ORDER_KEY)))

  const setShown = (id: HomeRowId, on: boolean) => {
    const next = new Set(hidden)
    if (on) next.delete(id)
    else next.add(id)
    setPref.mutate({ key: 'home.hidden', value: JSON.stringify([...next]) })
  }

  const setFold = useCallback((id: HomeRowId, fold: boolean) => {
    setFolded((was) => {
      const next = new Set(was)
      if (fold) next.add(id)
      else next.delete(id)
      writeKey(FOLDED_KEY, JSON.stringify([...next]))
      return next
    })
  }, [])

  const reorder = (next: HomeRowId[]) => {
    setOrder(next)
    writeKey(ORDER_KEY, JSON.stringify(next))
  }

  const move = (id: HomeRowId, to: number) => {
    const from = order.indexOf(id)
    if (from < 0 || to < 0 || to >= order.length || to === from) return
    const next = order.filter((r) => r !== id)
    next.splice(to, 0, id)
    reorder(next)
  }

  const resetOrder = () => {
    setOrder(DEFAULT_ORDER)
    writeKey(ORDER_KEY, null)
  }

  return {
    order,
    shown: (id: HomeRowId) => !hidden.has(id),
    wanted: (id: HomeRowId) => prefs.isSuccess && !hidden.has(id),
    folded: (id: HomeRowId) => folded.has(id),
    unfold: (id: HomeRowId) => () => setFold(id, false),
    setShown,
    setFold,
    move,
    reorder,
    resetOrder,
    custom: order.join() !== DEFAULT_ORDER.join(),
    saveFailed: setPref.isError,
    retrySave: () => {
      if (setPref.variables) setPref.mutate(setPref.variables)
    },
  }
}

export type HomeLayout = ReturnType<typeof useHomeLayout>
