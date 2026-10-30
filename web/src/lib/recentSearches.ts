import { useSyncExternalStore } from 'react'
import { readKey, writeKey } from './storage'

const KEY = 'yoru.search.recent'
const KEEP = 5
const listeners = new Set<() => void>()

function read(): string[] {
  try {
    const saved = JSON.parse(readKey(KEY) ?? '[]') as unknown
    return Array.isArray(saved) ? saved.filter((s): s is string => typeof s === 'string').slice(0, KEEP) : []
  } catch {
    return []
  }
}

let recent = read()

function write(next: string[]) {
  recent = next
  listeners.forEach((l) => l())
  writeKey(KEY, JSON.stringify(next))
}

export function rememberSearch(text: string) {
  const q = text.trim()
  if (q.length < 2) return
  const next = [q, ...recent.filter((s) => s.toLowerCase() !== q.toLowerCase())].slice(0, KEEP)
  if (next.join('\n') !== recent.join('\n')) write(next)
}

export function forgetSearch(text: string) {
  write(recent.filter((s) => s !== text))
}

function subscribe(changed: () => void) {
  listeners.add(changed)
  const onStorage = (e: StorageEvent) => {
    if (e.key !== KEY) return
    recent = read()
    changed()
  }
  window.addEventListener('storage', onStorage)
  return () => {
    listeners.delete(changed)
    window.removeEventListener('storage', onStorage)
  }
}

export function useRecentSearches(): string[] {
  return useSyncExternalStore(subscribe, () => recent)
}
