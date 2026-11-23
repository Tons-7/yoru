export interface PeopleShow {
  malId: number
  id?: number
  title: string
  image?: string
  year?: number
  status?: string
}

export interface PersonRef {
  id: number
  name: string
  image?: string
}

export function realImage(url?: string): string | undefined {
  return url && !/questionmark|na_series|na\.gif/i.test(url) ? url : undefined
}

export function initials(name: string): string {
  return name.split(/[\s,]+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join('')
}

export function showPath(show: PeopleShow): string {
  return show.id ? `/anime/${show.id}` : `/open?url=${encodeURIComponent(`https://myanimelist.net/anime/${show.malId}`)}`
}
