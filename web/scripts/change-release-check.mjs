import { spawnSync } from 'node:child_process'
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const SHOTS = process.env.SHOTS ?? '.'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const until = async (fn, ms = 30_000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
    await new Promise((r) => setTimeout(r, 150))
  }
  return false
}

// Two finished downloads on disk (episodes 2 and 3), planted the way the features check does.
const seeded = spawnSync('go', ['run', './scripts/e2e-seed', '-db', process.env.YORU_DB, '-anime', String(ANIME)], {
  cwd: process.env.YORU_REPO, stdio: 'inherit',
}).status === 0
check(seeded, 'download rows seeded')

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

// The sites are not reachable here: the picker's list is answered for it.
const releases = [
  { Torrent: { title: '[Mock] Yoru Test Show - 03 [1080p]', infoHash: 'a'.repeat(40), size: 1 << 30, seeders: 40, seedersKnown: true, leechers: 1 }, Release: { group: 'Mock' } },
  { Torrent: { title: '[Other] Yoru Test Show - 03 [720p]', infoHash: 'b'.repeat(40), size: 1 << 29, seeders: 5, seedersKnown: true, leechers: 0 }, Release: { group: 'Other' } },
]
await page.route('**/api/episode/sources*', (route) => route.fulfill({ json: { results: releases, queries: ['Yoru Test Show'] } }))
const chosen = []
await page.route('**/api/download/release', (route) => {
  chosen.push(JSON.parse(route.request().postData() ?? '{}'))
  return route.fulfill({ status: 202, json: { queued: true } })
})
// Once chosen, the queue lists the episode with its release.
await page.route('**/api/download/queue', (route) =>
  route.fulfill({
    json: {
      items: chosen.length
        ? [{ animeId: ANIME, epKey: '3', episode: 3, state: 'pending', infoHash: chosen[0].infoHash, release: chosen[0].title }]
        : [],
      waiting: {}, paused: false,
    },
  }),
)

await page.goto(`${BASE}/downloads`, { waitUntil: 'domcontentloaded' })
const more = page.getByRole('button', { name: /^More for .*episode 3$/ })
const group = page.getByRole('button', { name: /^Show downloads of / }).first()
if (!(await until(() => more.first().isVisible(), 10_000)) && (await group.isVisible())) await group.click()
check(await until(() => more.first().isVisible()), 'a download on disk has a menu')
await more.first().click()
const change = page.getByRole('menuitem', { name: 'Change release' })
check(await until(() => change.isVisible()), 'which offers Change release')
await change.click()
const dialog = page.getByRole('dialog', { name: 'Choose a release' })
check(await until(() => dialog.isVisible()), 'the picker opens')
check(await until(() => dialog.getByText('downloads it instead').isVisible()), 'and says the pick downloads instead of the current release')
await page.screenshot({ path: `${SHOTS}/change-release-picker.png` })
await dialog.getByText('[Other] Yoru Test Show - 03 [720p]').click()
check(await until(() => chosen.length === 1), 'picking one sends the choice')
check(chosen[0]?.animeId === ANIME && chosen[0]?.episode === 3 && chosen[0]?.infoHash === 'b'.repeat(40) && chosen[0]?.title === '[Other] Yoru Test Show - 03 [720p]', 'with the show, the episode, the release and its name', JSON.stringify(chosen[0]))
check(await until(async () => (await dialog.count()) === 0), 'and the picker closes')
// The copy on disk stands for the queued swap, so it is the row that says what is coming.
check(await until(() => page.getByText('Changing to: [Other] Yoru Test Show - 03 [720p]').isVisible()), 'the row on disk shows the release it is changing to')
await more.first().click()
check(await until(() => change.isVisible()), 'and still offers to change it')
await page.keyboard.press('Escape')
await page.screenshot({ path: `${SHOTS}/change-release-queued.png` })

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
