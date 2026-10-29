/** A show on a person or character page, as Yoru knows it: id 0 when only MyAnimeList does. */
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

/** A picture worth showing: MyAnimeList's grey question mark stands in for none. */
export function realImage(url?: string): string | undefined {
  return url && !/questionmark|na_series|na\.gif/i.test(url) ? url : undefined
}

/** Initials for a face with no picture: "Jun Fukuyama" is "JF". */
export function initials(name: string): string {
  return name.split(/[\s,]+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join('')
}

/** Yoru's page for the show; one it hasn't met goes through Open, which finds it by its MyAnimeList id. */
export function showPath(show: PeopleShow): string {
  return show.id ? `/anime/${show.id}` : `/open?url=${encodeURIComponent(`https://myanimelist.net/anime/${show.malId}`)}`
}
