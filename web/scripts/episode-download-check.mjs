// Every episode not yet on disk has a download button on the series page; one click queues it.
import { spawnSync } from 'node:child_process'
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = process.env.YORU_ANIME ?? '127230'
const SHOTS = process.env.SHOTS ?? '.'


let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const until = async (fn, ms = 30_000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (await fn()) return true
    await new Promise((r) => setTimeout(r, 150))
  }
  return false
}

// A kept season pack holding episodes 4 and 5, planted the way the features check does.
const seeded = spawnSync('go', ['run', './scripts/e2e-seed', '-db', process.env.YORU_DB, '-anime', ANIME, '-pack'], {
  cwd: process.env.YORU_REPO, stdio: 'inherit',
}).status === 0
check(seeded, 'download rows seeded')

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

// Episode 7 is already waiting in the queue, so its row must not offer a second download.
await page.route('**/api/download/queue', (route) =>
  route.fulfill({ json: { items: [{ animeId: Number(ANIME), epKey: '7', episode: 7, state: 'pending' }], waiting: {}, paused: false } }),
)
const queued = []
await page.route('**/api/download', (route) => {
  queued.push(JSON.parse(route.request().postData() ?? '{}'))
  return route.fulfill({ json: { queued: 1 } })
})

await page.goto(`${BASE}/anime/${ANIME}`, { waitUntil: 'domcontentloaded' })
const moreFor = (n) => page.getByRole('button', { name: `More for episode ${n}`, exact: true })
const menuOf = (n) => page.getByRole('menu', { name: `Episode ${n}`, exact: true })
const download = page.getByRole('menuitem', { name: 'Download', exact: true })
const open = async (n) => {
  await moreFor(n).click()
  return until(() => menuOf(n).isVisible(), 5000)
}
check(await until(() => moreFor(1).isVisible()), 'episode rows have a More menu')

check((await open(4)) && (await download.count()) === 0, 'an episode on disk offers no download')
await page.keyboard.press('Escape')

const waiting = page.getByRole('menuitem', { name: 'Queued for download' })
check((await open(7)) && (await until(() => waiting.isVisible(), 10_000)) && (await waiting.isDisabled()),
  'an episode already in the queue shows as queued and cannot be queued again')
await page.keyboard.press('Escape')
const row = (n) => page.locator('li', { has: page.getByRole('link', { name: new RegExp(`^Episode ${n}\\b`) }) })
check(await row(7).getByText('Queued for download').isVisible(), 'and its row says so')

let n = 0
for (let i = 1; i <= 12 && !n; i++) {
  if (!(await moreFor(i).isVisible()) || !(await open(i))) continue
  if (await download.count()) {
    await download.click()
    n = i
  } else await page.keyboard.press('Escape')
}
check(n > 0, 'an episode not on disk offers Download')
check(await until(() => queued.length === 1), 'one click queues the episode', JSON.stringify(queued))
check(queued[0]?.animeId === Number(ANIME) && queued[0]?.episode === n, 'with the right show and episode', JSON.stringify(queued[0]))
check(await until(() => row(n).getByText('Queued for download').isVisible()), 'the row says it is queued')
await page.screenshot({ path: `${SHOTS}/episode-download.png` })

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
