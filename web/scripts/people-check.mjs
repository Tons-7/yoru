// People pages: a cast card's character and voice lead to their pages; a voice actor's roles come one card a
// character, the viewer's shows first; a character's page lists who voiced them and the shows they're in.
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 20_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(200)) if (await fn().catch(() => false)) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

for (let i = 0; i < 60; i++) {
  try {
    if ((await fetch(`${BASE}/api/setup`)).ok) break
  } catch {}
  await sleep(1000)
}

const show = (malId, extra = {}) => ({ malId, title: `Show ${malId}`, year: 2020, ...extra })
const person = {
  id: 86, name: 'Fukuyama, Jun', native: '福山 潤', birthday: '1978-11-26', favorites: 9000, about: 'Line one.\n'.repeat(12),
  characters: [
    { character: { id: 417, name: 'Lamperouge, Lelouch' }, role: 'Main',
      shows: [show(1575, { id: ANIME, title: 'Code Geass', status: 'COMPLETED' }), show(2904)] },
    { character: { id: 7, name: 'Someone' }, role: 'Supporting', shows: [show(999, { title: 'Newer Show' })] },
  ],
  work: [{ show: show(1575, { id: ANIME, title: 'Code Geass', status: 'COMPLETED' }), positions: ['Theme Song Performance'] }],
}
const character = {
  id: 417, name: 'Lamperouge, Lelouch', native: 'ルルーシュ', about: 'A prince.',
  appearances: [{ role: 'Main', show: show(1575, { id: ANIME, title: 'Code Geass', status: 'COMPLETED' }) }, { role: 'Main', show: show(2904) }],
  voices: [{ id: 86, name: 'Fukuyama, Jun', language: 'Japanese' }, { id: 5, name: 'Johnny Yong Bosch', language: 'English' }],
}

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
try {
  await page.route(`**/api/anime/${ANIME}/characters`, (route) => route.fulfill({ json: {
    items: [{ id: 417, name: 'Lamperouge, Lelouch', role: 'Main', favorites: 1, voices: [{ id: 86, name: 'Fukuyama, Jun', language: 'Japanese' }] }],
  } }))
  await page.route('**/api/person/86', (route) => route.fulfill({ json: person }))
  await page.route('**/api/character/417', (route) => route.fulfill({ json: character }))

  // The cast card: two ways in, each named as said, not as MyAnimeList files it.
  await page.goto(`${BASE}/anime/${ANIME}`, { waitUntil: 'domcontentloaded' })
  const lelouch = page.getByRole('link', { name: 'Lelouch Lamperouge', exact: true })
  check(await until(() => lelouch.isVisible(), 30_000), 'the cast card names the character, given name first')
  const voice = page.getByRole('link', { name: 'Jun Fukuyama, Japanese voice' })
  check(await voice.isVisible(), 'and its voice actor')
  await voice.click()
  check(await until(async () => new URL(page.url()).pathname === '/person/86'), 'the voice opens their page', page.url())

  // A voice actor's page.
  check(await until(() => page.getByRole('heading', { name: 'Jun Fukuyama', level: 1 }).isVisible()), 'their page leads with their name')
  check(await page.getByText('福山 潤').isVisible(), 'and their name in Japanese')
  const mine = page.locator('section', { has: page.getByRole('heading', { name: /^In your shows/ }) })
  check(await mine.getByRole('link', { name: 'Lelouch Lamperouge', exact: true }).isVisible(), 'a part in the viewer\'s show comes first, under its own heading')
  check((await mine.getByRole('link', { name: /^Code Geass/ }).getAttribute('href')) === `/anime/${ANIME}`, 'its show opens in Yoru')
  check(await mine.getByText('Completed').isVisible(), 'with the list status on the card')
  check(await mine.getByText('· +1').isVisible(), 'and how many more shows the part is in')
  const other = page.locator('section', { has: page.getByRole('heading', { name: /^Supporting roles/ }) })
  check((await other.getByRole('link', { name: 'Newer Show' }).getAttribute('href') ?? '').startsWith('/open?url='),
    'a show Yoru hasn\'t met opens through Open, by its MyAnimeList number')
  check(await page.getByText('Theme song · 2020').isVisible(), 'staff work says the position in plain words and the year')
  const more = page.getByRole('button', { name: 'Show more' })
  await more.click()
  check(await until(() => page.getByRole('button', { name: 'Show less' }).isVisible()), 'a long bio folds, Show more unfolds it')

  // A character's page, from their card.
  await mine.getByRole('link', { name: 'Lelouch Lamperouge', exact: true }).click()
  check(await until(async () => new URL(page.url()).pathname === '/character/417'), 'the character opens their page', page.url())
  check(await until(() => page.getByRole('heading', { name: 'Lelouch Lamperouge', level: 1 }).isVisible()), 'named as said')
  const voiced = page.locator('section', { has: page.getByRole('heading', { name: /^Voiced by/ }) })
  check((await voiced.getByRole('link', { name: /Jun Fukuyama/ }).getAttribute('href')) === '/person/86', 'their voices link to the actors')
  const appears = page.locator('section', { has: page.getByRole('heading', { name: /^Appears in/ }) })
  check((await appears.getByRole('link', { name: 'Code Geass', exact: true }).getAttribute('href')) === `/anime/${ANIME}`,
    'and the shows they\'re in, the viewer\'s first')

  // A phone: no page wider than the screen.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(`${BASE}/person/86`, { waitUntil: 'domcontentloaded' })
  await until(() => page.getByRole('heading', { name: 'Jun Fukuyama', level: 1 }).isVisible())
  check(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'on a phone nothing runs off the side')
  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
