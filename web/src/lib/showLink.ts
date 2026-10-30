import { parseTogether } from './together'

/** A show page's address on AniList or MyAnimeList, found in pasted or shared text: the site and its id. */
export function showLink(text: string): { site: 'anilist' | 'mal'; id: number } | null {
  const m = /(?:^|[\s/])(?:www\.)?(anilist\.co|myanimelist\.net)\/anime\/(\d+)/i.exec(text)
  if (!m) return null
  return { site: m[1].toLowerCase() === 'anilist.co' ? 'anilist' : 'mal', id: Number(m[2]) }
}

export type OpenableLink = { site: 'anilist' | 'mal' | 'room'; id: number }

export function openableLink(text: string): OpenableLink | null {
  return showLink(text) ?? (parseTogether(text) ? { site: 'room', id: 0 } : null)
}

export const openLinkPath = (text: string) => `/open?url=${encodeURIComponent(text.trim())}`

export const openLinkWords = (link: OpenableLink) =>
  link.site === 'room' ? 'Join this watch-together room' : `Open this ${link.site === 'anilist' ? 'AniList' : 'MyAnimeList'} link`
