import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

const MB = 1 << 20
const part = (epKey, fileIndex, done, more = {}) => {
  const size = 400 * MB
  return { epKey, fileIndex, size, done, complete: done >= size, kept: true, pinned: false, paused: false, ...more }
}
const pack = (infoHash, name, parts, more = {}) => {
  const total = parts.reduce((n, p) => n + p.size, 0)
  const done = parts.reduce((n, p) => n + p.done, 0)
  return {
    infoHash, animeId: 1, title: 'Princess Principal', name,
    episodes: parts.map((p) => p.epKey), episode: parts.map((p) => p.epKey).join(', '),
    totalBytes: total, bytesOnDisk: done, percent: (done / total) * 100,
    pinned: false, kept: true, state: 'live', mbps: 6.5, peers: 7,
    group: 'Sav1or', resolution: '1080p', codec: 'AV1', parts, ...more,
  }
}
const items = [
  pack('aaaa', '[Sav1or] Princess Principal + Movies + OVA [BD][1080p][AV1]', [
    part('3', 13, 400 * MB),
    part('5', 15, 400 * MB, { kept: false }),
    part('8', 18, 40 * MB),
  ]),
  pack('bbbb', '[AkihitoSubs] Princess Principal + Extras [BD 1080p x265 10Bit]', [
    part('6', 11, 100 * MB, { paused: true }),
    part('7', 12, 20 * MB),
  ], { group: 'AkihitoSubs', codec: 'HEVC', mbps: 0.4, peers: 1 }),
]

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const calls = []
await page.route('**/api/download/queue', (route) => route.fulfill({ json: { items: [], waiting: {}, paused: false } }))
await page.route('**/api/downloads', (route) => route.fulfill({ json: { items, count: items.length } }))
await page.route(/\/api\/downloads\/[a-z]+(\/files\/\d+)?(\/(keep|unkeep|pause|resume))?$/, (route) => {
  const url = new URL(route.request().url())
  calls.push(`${route.request().method()} ${url.pathname.replace('/api/downloads/', '')}`)
  return route.fulfill({ json: { ok: true } })
})

await page.goto(`${BASE}/downloads`, { waitUntil: 'domcontentloaded' })
const main = page.locator('main')
const row = (text) => main.locator('li', { hasText: text })
await row('episode 8').first().waitFor({ timeout: 10_000 })
await page.waitForTimeout(300)

check((await main.getByText(/episodes 3, 5, 8/).count()) === 0, 'a pack is never one row of several episodes')
const eight = row('episode 8').first()
check(/10%/.test(await eight.innerText()), 'the downloading episode shows its own progress')
check(/40\.0 MB of 400\.0 MB/.test(await eight.innerText()), 'with its own size')
check(/Paused · 25%/.test(await row('episode 6').first().innerText()), 'an episode paused inside a pack shows paused on its own')
check(/Downloading/.test(await row('episode 7').first().innerText()), 'while the other episode of that pack keeps downloading')
check((await main.getByText(/Same release as|pause together/).count()) === 0, 'no "pauses together" wording')

await row('episode 7').first().getByRole('button', { name: 'Pause download' }).click()
await page.waitForTimeout(300)
check(calls.includes('POST bbbb/files/12/pause'), 'pausing an episode pauses only its file', calls.join(' | '))
check(!calls.some((c) => c === 'POST bbbb/pause'), 'and never the whole pack')

await eight.getByRole('button', { name: /More for Princess Principal episode 8/ }).click()
check(await page.getByText('Change release', { exact: true }).isVisible(), 'a pack episode can change its release')
check(await page.getByRole('menuitem', { name: 'Stop and remove', exact: true }).isVisible().catch(() => false) ||
  (await page.getByText('Stop and remove', { exact: true }).isVisible()), 'removing names only this episode')
await page.keyboard.press('Escape')

const group = page.getByRole('button', { name: /downloads of Princess Principal$/ })
if (await group.count()) await group.first().click()
const three = row('Episode 3').last()
check((await three.count()) > 0 && (await row('Episode 5').count()) > 0, 'finished episodes of a pack still downloading are listed as downloaded')
await three.getByRole('button', { name: 'Keep' }).click()
await page.waitForTimeout(300)
check(calls.includes('POST aaaa/files/13/unkeep'), 'keep on one episode changes only its file', calls.join(' | '))

const notKept = page.locator('li', { hasText: /^Not kept/ }).filter({ has: page.getByRole('button', { name: 'Clear' }) }).first()
check(/1 episode · 400\.0 MB/.test(await notKept.innerText()), 'an episode not kept can be cleared on its own, whatever its pack holds', await notKept.innerText())

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures === 0 ? 'all passed' : `${failures} failed`)
process.exit(failures === 0 ? 0 : 1)
