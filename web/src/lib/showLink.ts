/** A show page's address on AniList or MyAnimeList, found in pasted or shared text: the site and its id. */
export function showLink(text: string): { site: 'anilist' | 'mal'; id: number } | null {
  const m = /(?:^|[\s/])(?:www\.)?(anilist\.co|myanimelist\.net)\/anime\/(\d+)/i.exec(text)
  if (!m) return null
  return { site: m[1].toLowerCase() === 'anilist.co' ? 'anilist' : 'mal', id: Number(m[2]) }
}
