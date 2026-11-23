import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const seen = (locator, ms = 15000) => locator.first().waitFor({ timeout: ms }).then(() => true, () => false)
const until = async (fn, ms = 10000) => {
  for (const end = Date.now() + ms; Date.now() < end; await new Promise((r) => setTimeout(r, 250))) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
  }
  return false
}

const months = Array.from({ length: 12 }, (_, i) => {
  const d = new Date()
  d.setDate(1)
  d.setMonth(d.getMonth() - 11 + i)
  return { month: `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`, seconds: i === 11 ? 7200 : i * 1800 }
})
const STATS = {
  totalSeconds: 360000,
  episodes: 412,
  completed: 31,
  shows: 58,
  genres: [
    { name: 'Action', seconds: 120000, shows: 20 },
    { name: 'Drama', seconds: 60000, shows: 12 },
  ],
  studios: [{ name: 'Bones', seconds: 40000, shows: 4 }],
  seasons: [{ name: 'FALL 2025', seconds: 30000, shows: 6 }],
  months,
}

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.split('\n')[0]))

await page.route('**/api/stats', (route) => route.fulfill({ json: STATS }))
await page.goto(`${BASE}/stats`, { waitUntil: 'domcontentloaded' })
check(await seen(page.getByRole('heading', { name: 'Statistics' })), 'the Statistics page opens')
check(await seen(page.getByText('100 h', { exact: true })), 'it leads with the hours watched')
check(await seen(page.getByRole('heading', { name: 'Genres' })), 'with genres')
check(await seen(page.getByText('Fall 2025')), 'seasons read as words, not codes')
check((await page.getByRole('list', { name: 'Hours watched each month' }).locator('li').count()) === 12, 'twelve months of bars')
await page.screenshot({ path: `${SHOTS}/stats-wide.png`, fullPage: true })

await page.setViewportSize({ width: 390, height: 844 })
await page.reload({ waitUntil: 'domcontentloaded' })
await seen(page.getByRole('heading', { name: 'Genres' }))
check(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1), 'no sideways scroll on a phone')
await page.screenshot({ path: `${SHOTS}/stats-phone.png`, fullPage: true })

await page.unroute('**/api/stats')
await page.route('**/api/stats', (route) =>
  route.fulfill({ json: { ...STATS, totalSeconds: 0, shows: 0, genres: [], studios: [], seasons: [] } }),
)
await page.reload({ waitUntil: 'domcontentloaded' })
check(await seen(page.getByText('Nothing watched yet.')), 'an empty history says so')
await page.setViewportSize({ width: 1280, height: 900 })

const asked = []
page.on('request', (r) => {
  if (r.url().includes('/api/library?')) asked.push(r.url())
})
await page.goto(`${BASE}/library?layout=list`, { waitUntil: 'domcontentloaded' })
const format = page.getByRole('combobox', { name: 'Format' })
check(await seen(format), 'Library offers a format filter')
await format.selectOption('MOVIE')
check(await until(() => page.url().includes('format=MOVIE')), 'the choice goes into the address')
check(await until(() => asked.some((u) => u.includes('format=MOVIE'))), 'and into the request')
await page.getByRole('button', { name: 'Downloaded' }).click()
check(await until(() => asked.some((u) => u.includes('disk=1'))), 'Downloaded narrows to what is on disk')
const clear = page.getByRole('button', { name: 'Clear filters' })
check(await seen(clear), 'a Clear filters button appears')
await clear.click()
check(await until(() => !page.url().includes('format=') && !page.url().includes('disk=')), 'Clear filters empties them')

await page.goto(`${BASE}/settings?tab=Library`, { waitUntil: 'domcontentloaded' })
const full = page.getByRole('link', { name: 'Download', exact: true })
const fullRow = page.getByText('Full backup', { exact: true })
if (!(await seen(fullRow, 5000))) {
  await page.goto(`${BASE}/settings?q=backup`, { waitUntil: 'domcontentloaded' })
}
check(await seen(page.getByText('Full backup', { exact: true })), 'Settings offers a full backup')
check(((await full.first().getAttribute('href').catch(() => '')) ?? '').includes('/api/backup/full'), 'which downloads the whole database')
const backup = await fetch(`${BASE}/api/backup/full`)
const head = Buffer.from(await backup.arrayBuffer()).subarray(0, 15).toString()
check(backup.ok && head === 'SQLite format 3', 'the backup is a real database file', `${backup.status} ${head}`)
check((backup.headers.get('content-disposition') ?? '').includes('yoru-backup-'), 'named as a Yoru backup')
const junk = await fetch(`${BASE}/api/backup/full`, { method: 'POST', body: 'not a database' })
check(junk.status === 400, 'restoring a file that is not a backup is refused', String(junk.status))

const pick = { id: 99001, title: { romaji: 'Pick Show', english: 'Pick Show' }, coverImage: {}, format: 'TV', status: 'FINISHED' }
let hiddenPost = null
await page.route('**/api/home/for-you', (route) => route.fulfill({ json: { items: [pick] } }))
await page.route('**/api/bookmarks', (route) => {
  if (route.request().method() !== 'POST') return route.continue()
  hiddenPost = JSON.parse(route.request().postData() ?? '{}')
  return route.fulfill({ json: { ok: true } })
})
await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
const card = page.locator('div.group\\/card', { hasText: 'Pick Show' })
if (await seen(card, 15000)) {
  await card.getByRole('button', { name: 'Add to list' }).click()
  const not = page.getByRole('menuitem', { name: 'Not interested' })
  check(await seen(not, 5000), 'a For you pick offers Not interested')
  await not.click()
  check(await until(() => hiddenPost?.animeId === pick.id && hiddenPost?.hidden === true), 'it hides the show on the PC')
  check(await until(async () => (await card.count()) === 0), 'the pick leaves the row at once')
  check(await seen(page.getByRole('button', { name: 'Undo' }), 5000), 'with an Undo')
} else {
  console.log('SKIP Not interested: For you is folded or hidden on this Home')
}

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures === 0 ? '\nall checks passed' : `\n${failures} check(s) failed`)
process.exit(failures === 0 ? 0 : 1)
