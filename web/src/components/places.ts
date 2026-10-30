export type Place = 'home' | 'search' | 'schedule' | 'library' | 'you'

export const historyIndex = () => (window.history.state as { idx?: number } | null)?.idx ?? 0

const ROOTS: Record<Exclude<Place, 'home'>, string[]> = {
  search: ['/browse', '/search'],
  schedule: ['/schedule', '/recent'],
  library: ['/library', '/history'],
  you: ['/you', '/downloads', '/saved', '/notifications', '/settings', '/local'],
}

const under = (pathname: string, root: string) => pathname === root || pathname.startsWith(`${root}/`)

export function placeOf(pathname: string): Place | null {
  if (pathname === '/') return 'home'
  for (const [place, roots] of Object.entries(ROOTS)) {
    if (roots.some((root) => under(pathname, root))) return place as Place
  }
  return null
}

const TAB_PAGES = new Set(['/', '/browse', '/search', '/schedule', '/recent', '/library', '/history', '/you'])

export function studioName(pathname: string, search: string): string | undefined {
  if (pathname !== '/browse') return undefined
  const params = new URLSearchParams(search)
  return params.get('studio') ? params.get('studioName') || 'Studio' : undefined
}

export function roomyPage(pathname: string, search: string): boolean {
  const view = new URLSearchParams(search).get('view')
  if (pathname === '/' || pathname === '/browse') return true
  if (pathname === '/library') return view !== 'history'
  if (pathname === '/schedule') return view === 'out'
  return false
}

export const isTabPage = (pathname: string, search = '') =>
  TAB_PAGES.has(pathname.replace(/\/+$/, '') || '/') && !studioName(pathname, search)

export interface Parent {
  label: string
  to?: string
}

export const SETTINGS_CATEGORIES: Record<string, string> = {
  New: 'New since your update',
  Playback: 'Playback',
  Language: 'Language and display',
  Sound: 'Sound and picture',
  Downloads: 'Downloads',
  Quality: 'Downloads',
  Storage: 'Storage',
  Accounts: 'Accounts',
  Trackers: 'Accounts',
  Library: 'Library',
  Notifications: 'Notifications',
  Devices: 'Devices',
  Access: 'Devices',
  Computer: 'This computer',
  About: 'About',
  Jobs: 'About',
}

export function settingsCategory(pathname: string, search: string): string | undefined {
  if (pathname !== '/settings') return undefined
  const tab = new URLSearchParams(search).get('tab')
  return tab ? SETTINGS_CATEGORIES[tab] : undefined
}

export function parentOf(pathname: string, search: string): Parent {
  if (settingsCategory(pathname, search)) return { label: 'Settings', to: '/settings' }
  if (pathname.startsWith('/saved/')) return { label: 'Downloads', to: '/saved' }
  if (placeOf(pathname) === 'you') return { label: 'You', to: '/you' }
  return { label: 'Back' }
}

const NAMES: Record<string, string> = {
  '/settings': 'Settings',
  '/notifications': 'Notifications',
  '/downloads': 'Downloads',
  '/saved': 'Downloads',
  '/local': 'Local files',
  '/setup': 'Setting up',
  '/open': 'Opening',
}

export const pageName = (pathname: string, search: string) => settingsCategory(pathname, search) ?? NAMES[pathname]
