const { splitSeason, seasonOf, libraryProgress, timeLeft, historyUpNext, outDay, lastWatched } = await import(
  new URL('../src/lib/libraryText.ts', import.meta.url)
)

let failures = 0
const check = (got, want, label) => {
  const [g, w] = [JSON.stringify(got), JSON.stringify(want)]
  if (g === w) return console.log(`ok   ${label}`)
  failures++
  console.log(`FAIL ${label}: got ${g}, want ${w}`)
}
const split = (title, name, season) => check(splitSeason(title), { name, season }, `${title}`)

split('Spy x Family Season 2', 'Spy x Family', 'Season 2')
split('Oshi no Ko 2nd Season', 'Oshi no Ko', 'Season 2')
split('OSHI NO KO 3RD SEASON', 'OSHI NO KO', 'Season 3')
split('Haikyu!! Second Season', 'Haikyu!!', 'Season 2')
split('Re:Zero kara Hajimeru Isekai Seikatsu 2nd Season Part 2', 'Re:Zero kara Hajimeru Isekai Seikatsu', 'Season 2 Part 2')
split('Mushoku Tensei: Jobless Reincarnation Season 2 Part 2', 'Mushoku Tensei: Jobless Reincarnation', 'Season 2 Part 2')
split('Re:ZERO -Starting Life in Another World- Season 3', 'Re:ZERO -Starting Life in Another World-', 'Season 3')
split('Attack on Titan: The Final Season', 'Attack on Titan', 'Final Season')
split('Shingeki no Kyojin: The Final Season Part 2', 'Shingeki no Kyojin', 'Final Season Part 2')
split('Dr. STONE NEW WORLD Part 2', 'Dr. STONE NEW WORLD', 'Part 2')
split("JoJo's Bizarre Adventure: Stone Ocean Part 3", "JoJo's Bizarre Adventure: Stone Ocean", 'Part 3')
split('Haikyuu!! To the Top Part II', 'Haikyuu!! To the Top', 'Part 2')
split('Season 3 - Part 2 test', 'Season 3 - Part 2 test', null)
split('Kingdom 5th Season', 'Kingdom', 'Season 5')
split('Initial D Fourth Stage', 'Initial D', 'Stage 4')
split('Overlord IV', 'Overlord', 'Season 4')
split('Mob Psycho 100 II', 'Mob Psycho 100', 'Season 2')
split('Violet Evergarden: The Movie', 'Violet Evergarden', 'Movie')
split('K-On! Movie', 'K-On!', 'Movie')
split('Tensei shitara Slime Datta Ken 2nd Season Part 2', 'Tensei shitara Slime Datta Ken', 'Season 2 Part 2')
split('Gintama Season 4 - Cour 2', 'Gintama', 'Season 4 Cour 2')
split('Kaguya-sama: Love Is War Season 2', 'Kaguya-sama: Love Is War', 'Season 2')

split('Mushoku Tensei II: Isekai Ittara Honki Dasu', 'Mushoku Tensei II: Isekai Ittara Honki Dasu', null)
split('JoJo no Kimyou na Bouken Part 5: Ougon no Kaze', 'JoJo no Kimyou na Bouken Part 5: Ougon no Kaze', null)
split('Cyberpunk: Edgerunners 2', 'Cyberpunk: Edgerunners 2', null)
split('Steins;Gate 0', 'Steins;Gate 0', null)
split('Mobile Suit Gundam X', 'Mobile Suit Gundam X', null)
split('Final Fantasy VII', 'Final Fantasy VII', null)
split('Season 2', 'Season 2', null)
split('The Movie', 'The Movie', null)
split('Hunting Season', 'Hunting Season', null)
split('Sword Art Online the Movie: Progressive', 'Sword Art Online the Movie: Progressive', null)
split('ONE PIECE', 'ONE PIECE', null)
split('Part 2', 'Part 2', null)
split('Bleach: Part Time', 'Bleach: Part Time', null)
split('Kimi ni Todoke 2nd Season Part', 'Kimi ni Todoke 2nd Season Part', null)
split('Gekijouban Kimetsu no Yaiba Movie: Mugen Ressha-hen', 'Gekijouban Kimetsu no Yaiba Movie: Mugen Ressha-hen', null)

check(seasonOf('Haikyu!! The Dumpster Battle', 'MOVIE'), { name: 'Haikyu!! The Dumpster Battle', season: 'Movie' }, 'a film says Movie from its format')
check(seasonOf('Violet Evergarden: The Movie', 'MOVIE'), { name: 'Violet Evergarden', season: 'Movie' }, 'and once, when its title says so too')
check(seasonOf('Frieren', 'TV'), { name: 'Frieren', season: null }, 'a series without a season keeps its title')

const now = Math.floor(new Date(2026, 9, 30, 12).getTime() / 1000)
const card = (facts) => {
  const p = libraryProgress({ episodes: null, progress: 0, status: 'CURRENT', nextEpisode: null, nextAiringAt: null, airing: 'FINISHED', ...facts }, now)
  return [p.lead, p.tail, p.fresh, p.done, Math.round(p.percent), p.next, p.waiting]
}
check(card({ episodes: 26, progress: 18 }), ['Ep 19 next', '8 left', false, false, 69, 19, null], 'watching a finished show')
check(card({ episodes: 26, progress: 0, status: 'PLANNING' }), ['Not started · 26 eps', null, false, false, 0, 1, null], 'planned, not started')
check(
  card({ progress: 0, status: 'PLANNING', airing: 'RELEASING', nextEpisode: 1151 }),
  ['Not started · 1,150 eps', null, false, false, 0, 1, null],
  'a long runner counts what has aired',
)
check(card({ episodes: 26, progress: 26, status: 'COMPLETED' }), ['Done · 26 eps', null, false, true, 100, null, null], 'done')
check(card({ episodes: 28, progress: 3, status: 'COMPLETED' }), ['Done · 28 eps', null, false, true, 100, null, null], 'completed is done whatever the count')
check(card({ episodes: 1, progress: 1, status: 'COMPLETED' }), ['Done · 1 ep', null, false, true, 100, null, null], 'one episode')
check(
  card({ episodes: 11, progress: 6, airing: 'RELEASING', nextEpisode: 9 }),
  ['Ep 7 next', '2 new', true, false, 75, 7, null],
  'behind on an airing show: new episodes',
)
check(
  card({ episodes: 12, progress: 4, airing: 'RELEASING', nextEpisode: 5, nextAiringAt: now + 3600 }),
  ['Ep 5 next', 'out today', false, false, 100, null, 5],
  'caught up: when the next one is out',
)
check(
  card({ progress: 4, airing: 'RELEASING', nextEpisode: 5, nextAiringAt: now + 86400 }),
  ['Ep 5 next', 'out tomorrow', false, false, 100, null, 5],
  'out tomorrow',
)
check(
  card({ progress: 0, status: 'PLANNING', airing: 'NOT_YET_RELEASED', nextEpisode: 1, nextAiringAt: null }),
  ['Ep 1 next', 'not out yet', false, false, 0, null, 1],
  'not out yet',
)
check(card({ progress: 0, status: 'CURRENT', episodes: 12 }), ['Ep 1 next', '12 left', false, false, 0, 1, null], 'watching, nothing seen yet')
check(card({ progress: 5, status: 'PAUSED' }), ['Ep 6 next', null, false, false, 0, 6, null], 'no counts known')

check(outDay(now + 3 * 86400, now), 'Mon', 'within a week, the weekday')
check(outDay(now + 9 * 86400, now), 'Nov 8', 'later, the date')

check(lastWatched(now - 3600, now), 'Today', 'played today')
check(lastWatched(now - 86400, now), 'Yesterday', 'yesterday')
check(lastWatched(now - 3 * 86400, now), '3 days ago', 'this week')
check(lastWatched(now - 20 * 86400, now), 'Oct 10', 'earlier this year')
check(lastWatched(now - 400 * 86400, now), 'Sep 2025', 'another year')
check(lastWatched(undefined, now), null, 'never played here')

check(timeLeft(600, 1440), '14 min left', 'minutes left')
check(timeLeft(1410, 1440), 'Under a minute left', 'under a minute')
check(timeLeft(0, 7200), '2 h left', 'a film')
check(timeLeft(300, 0), 'Partly watched', 'no duration known')

const ep = (episode, finished, extra = {}) => ({
  episode, position: finished ? 1400 : 600, duration: 1440, finished, progress: 0, episodes: 12,
  airing: 'FINISHED', nextEpisode: null, nextAiringAt: null, ...extra,
})
check(historyUpNext([ep(3, false), ep(2, true)]), { kind: 'next', episode: 3, resume: ep(3, false) }, 'up next resumes a part-watched one')
check(historyUpNext([ep(2, true), ep(1, true)]), { kind: 'next', episode: 3, resume: null }, 'up next follows the last finished')
check(historyUpNext([ep(2, true, { progress: 7 })]), { kind: 'next', episode: 8, resume: null }, 'list progress ahead of history wins')
check(historyUpNext([ep(12, true)]), { kind: 'done' }, 'the last episode finished: done')
check(
  historyUpNext([ep(4, true, { airing: 'RELEASING', nextEpisode: 5, nextAiringAt: 99 })]),
  { kind: 'waiting', episode: 5, at: 99 },
  'caught up with an airing show',
)

console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
