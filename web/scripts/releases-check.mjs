// Choosing releases: Settings' wait, avoids and preview; SeaDex notes; held episodes and better copies.
// Sites aren't reachable here, so what they would answer is fed to the page.
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 10_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
  }
  return false
}
const prefs = async (anime) => (await (await fetch(`${BASE}/api/prefs${anime ? `?anime=${anime}` : ''}`)).json())

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

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const row = (label) => page.locator('[data-row]', { has: page.getByText(label, { exact: true }) })
const GB = 1 << 30

// The picker's list, as a site search would rank it, with SeaDex's word on two of them.
const notes = 'Mehul is CsS+Restyled and retimed GUDish.\nThe JPN BD is heavily lowpassed; CsS cleans up a lot of the ringing to make it bearable.'
const sources = [
  { Torrent: { title: '[Mehul] Show - 05 (BD 1080p)', infoHash: 'a'.repeat(40), size: GB, seeders: 40, seedersKnown: true, leechers: 1 },
    Release: { Group: 'Mehul', Resolution: '1080p', Source: 'BD' }, SeaDexBest: true, SeaDexListed: true, SeaDexNotes: notes,
    SeaDexCompare: ['https://slow.pics/c/0iBPQRr7', 'https://www.diffchecker.com/9nrmWTwB/'], SeaDexIncomplete: true,
    score: 900, playable: true, autoPick: true, reasons: ['curated best release on SeaDex'] },
  { Torrent: { title: '[Drag] Show - 05 (BD 1080p)', infoHash: 'b'.repeat(40), size: GB, seeders: 20, seedersKnown: true, leechers: 0 },
    Release: { Group: 'Drag', Resolution: '1080p', Source: 'BD' }, SeaDexListed: true, SeaDexNotes: notes, score: 700, playable: true, autoPick: true },
  { Torrent: { title: '[BadGroup] Show - 05 (1080p)', infoHash: 'c'.repeat(40), size: GB, seeders: 900, seedersKnown: true, leechers: 0 },
    Release: { Group: 'BadGroup', Resolution: '1080p' }, score: 600, playable: true, autoPick: false, blocked: 'avoided group BadGroup' },
]
await page.route('**/api/episode/sources*', (route) => route.fulfill({ json: { results: sources, queries: ['Show'] } }))
const posted = []
await page.route('**/api/download/release', (route) => {
  posted.push(JSON.parse(route.request().postData() ?? '{}'))
  return route.fulfill({ status: 202, json: { queued: true } })
})

// Settings: the wait, avoided groups and words, and the low-quality switch are saved.
await page.goto(`${BASE}/settings?tab=Downloads`, { waitUntil: 'domcontentloaded' })
const wait = row('Wait for a better release').getByLabel('Wait for a better release')
check(await until(() => wait.isVisible()), 'Downloads offers a wait for a better release')
await wait.selectOption('3')
check(await until(async () => (await prefs()).effective['autodownload.wait_hours'] === '3'), 'a 3-hour wait is saved')
check(await until(() => row('Wait for a better release').getByText(/up to 3 hours for a preferred group/).isVisible()), 'and says what it does')
await wait.selectOption('0')

for (const [label, key, typed, stored] of [
  ['Avoided release groups', 'release.avoid_groups', 'BadGroup', '["BadGroup"]'],
  ['Excluded words', 'release.exclude_terms', 'x265 mini, CamRip', '["x265 mini","CamRip"]'],
]) {
  const field = page.getByLabel(label, { exact: true })
  await field.fill(typed)
  await field.blur()
  check(await until(async () => (await prefs()).effective[key] === stored), `${label} are saved`, stored)
  await field.fill('')
  await field.blur()
}
const lq = row('Avoid low-quality groups').getByRole('switch')
await lq.click()
check(await until(async () => (await prefs()).effective['release.avoid_lq'] === 'true'), 'avoiding low-quality groups is saved')
await lq.click()
await until(async () => (await prefs()).effective['release.avoid_lq'] === 'false')

// The preview lists per show what would happen and why; Open picker opens that episode's releases.
await page.route('**/api/releases/preview', (route) => route.fulfill({ json: { items: [
  { animeId: ANIME, title: 'Frieren', episode: 6, state: 'download', release: '[SubsPlease] Frieren - 06 (1080p)', size: 1.4 * GB },
  { animeId: 2, title: 'Waiting Show', episode: 3, state: 'wait', release: '[Other] Waiting Show - 03 (1080p)', size: GB, reason: 'waits up to 3 hours for SubsPlease at 1080p' },
  { animeId: 3, title: 'Big Show', episode: 9, state: 'blocked', release: '[Remux] Big Show - 09', reason: 'larger than the automatic size limit' },
  { animeId: 4, title: 'Done Show', state: 'current' },
] } }))
await row('Preview auto-download').getByRole('button', { name: 'Preview' }).click()
check(await until(() => page.getByText('Would download [SubsPlease] Frieren - 06 (1080p) · 1.4 GB').isVisible()), 'the preview says what it would download')
check(await page.getByText(/Would hold it: waits up to 3 hours for SubsPlease at 1080p/).isVisible(), 'what it would wait for')
check(await page.getByText('Left for you to choose: larger than the automatic size limit').isVisible(), 'and what it leaves, and why')
check(await page.getByText('Up to date: nothing new has aired').isVisible(), 'and which shows are up to date')
await page.screenshot({ path: `${SHOTS}/releases-preview.png` })

await page.getByRole('button', { name: 'Open picker' }).first().click()
const dialog = page.getByRole('dialog', { name: 'Choose a release' })
check(await until(() => dialog.isVisible()), 'Open picker opens the episode\'s releases')
const best = dialog.locator('li', { hasText: '[Mehul] Show - 05' })
check(await until(() => best.getByText('SeaDex', { exact: true }).isVisible()), 'the SeaDex pick is marked')
check(await best.getByText('SeaDex incomplete').isVisible(), 'with its entry still open')
check(await best.getByText(/Mehul is CsS\+Restyled/).isVisible(), "and the curator's notes")
check(await best.getByRole('link', { name: 'Comparison 1' }).getAttribute('href') === 'https://slow.pics/c/0iBPQRr7', 'and its comparisons')
await best.getByRole('button', { name: 'All notes' }).click()
check(await until(() => best.getByRole('button', { name: 'Fewer notes' }).isVisible()), 'the notes open in full')
check(await dialog.locator('li', { hasText: '[Drag] Show - 05' }).getByText('SeaDex alt').isVisible(), "SeaDex's other picks say so")
check(await dialog.locator('li', { hasText: '[BadGroup]' }).getByText('avoided group BadGroup').isVisible(), 'an avoided group says why it is not picked')
await page.screenshot({ path: `${SHOTS}/releases-picker.png` })
await dialog.getByText('[Drag] Show - 05 (BD 1080p)').click()
check(await until(() => posted.length === 1 && posted[0].infoHash === 'b'.repeat(40) && posted[0].episode === 6), 'picking one queues it', JSON.stringify(posted[0]))

// Downloads: an episode held for a better release, and a better copy over kept ones.
const calls = []
await page.route('**/api/releases/waiting', (route) => route.fulfill({ json: { items: [
  { animeId: 2, episode: 3, title: 'Waiting Show', until: Math.floor(Date.now() / 1000) + 7200, wants: 'SubsPlease at 1080p' },
] } }))
await page.route(/\/api\/releases\/waiting\/\d+\/\d+$/, (route) => {
  calls.push(`now ${new URL(route.request().url()).pathname}`)
  return route.fulfill({ json: { queued: true } })
})
const offer = { animeId: ANIME, title: 'Kept Show', kept: '[SubsPlease] Kept Show - 01 (1080p).mkv', episodes: ['1', '2'], size: 4 * GB,
  infoHash: 'd'.repeat(40), group: 'Mehul', resolution: '1080p', source: 'BD', release: '[Mehul] 1080p BD' }
await page.route('**/api/better-copies*', (route) => route.fulfill({ json: { items: [offer] } }))
await page.route(/\/api\/better-copies\/\d+\/(replace|dismiss)$/, (route) => {
  calls.push(route.request().url().split('/').pop())
  return route.fulfill({ json: { queued: 2 } })
})
await page.route('**/api/downloads', (route) => route.fulfill({ json: { count: 1, items: [
  { infoHash: 'e'.repeat(40), animeId: ANIME, title: 'Kept Show', name: offer.kept, episode: '1', episodes: ['1'], totalBytes: GB, bytesOnDisk: GB,
    percent: 100, pinned: false, kept: true, state: 'paused' },
] } }))
await page.goto(`${BASE}/downloads`, { waitUntil: 'domcontentloaded' })
check(await until(() => page.getByText('Held for a better release').isVisible()), 'Downloads lists episodes held for a better release')
check(await page.getByText(/Waiting for SubsPlease at 1080p · takes the best there is in (1h 59m|2h)/).isVisible(), 'saying what for and until when')
await page.getByRole('button', { name: 'Download episode 3 now' }).click()
check(await until(() => calls.includes('now /api/releases/waiting/2/3')), 'Download now takes it')
check(await page.getByText("Better copy · [Mehul] 1080p BD · SeaDex's best · 2 episodes · 4.0 GB").isVisible(), 'a better copy is offered over kept episodes')
await page.screenshot({ path: `${SHOTS}/releases-downloads.png` })
await page.getByRole('button', { name: 'Replace', exact: true }).first().click()
await page.getByRole('button', { name: 'Replace 2 episodes' }).click()
check(await until(() => calls.includes('replace')), 'Replace asks first, then queues the copy')
await page.getByRole('button', { name: "Don't offer again" }).first().click()
check(await until(() => calls.includes('dismiss')), "Don't offer again turns the offer down")

// The show page: with auto-download on, a wait of the show's own, over Settings'; and the better copy too.
await page.goto(`${BASE}/anime/${ANIME}`, { waitUntil: 'domcontentloaded' })
const auto = page.getByRole('switch', { name: 'Auto-download new episodes' })
if (await until(() => auto.isVisible(), 30_000)) {
  check(await until(() => page.getByText(/^Better copy ·/).isVisible()), 'the show page offers the better copy')
  await auto.click()
  const own = page.getByLabel('Wait for a better release')
  check(await until(() => own.isVisible()), 'auto-download on, the show page offers its own wait')
  check(await own.locator('option[value=""]').innerText() === "Default (don't wait)", 'following Settings until changed')
  await own.selectOption('6')
  check(await until(async () => (await prefs(ANIME)).overrides?.['autodownload.wait_hours'] === '6'), 'the show keeps its own wait')
  await own.selectOption('')
  check(await until(async () => !(await prefs(ANIME)).overrides?.['autodownload.wait_hours']), 'and goes back to Settings\'')
  await page.screenshot({ path: `${SHOTS}/releases-show.png` })
  await auto.click()
} else {
  check(false, 'the show page loads', 'AniList unreachable?')
}

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
