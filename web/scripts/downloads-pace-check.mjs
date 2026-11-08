import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

const MB = 1 << 20
const part = (animeId, size, done, extra = {}) => ({
  animeId, epKey: '1', fileIndex: 0, size, done, complete: false, kept: true, pinned: false, paused: false, ...extra,
})
const items = [
  {
    infoHash: 'aaaa', animeId: 1, title: 'Crown Handler 1', name: '[SubsPlease] Princess Principal - Crown Handler - 01 (1080p).mkv',
    episodes: ['1'], episode: '1', totalBytes: 3000 * MB, bytesOnDisk: 300 * MB, percent: 10, pinned: false, kept: true,
    state: 'live', paused: false, mbps: 6, peers: 6, group: 'SubsPlease', resolution: '1080p',
    parts: [part(1, 3000 * MB, 300 * MB)],
  },
  {
    infoHash: 'old0', animeId: 2, title: 'Crown Handler 2', name: 'Princess Principal Crown Handler 02 [BDRip 1080p x264 FLAC] [Dual-Audio].mkv',
    episodes: ['1'], episode: '1', totalBytes: 5000 * MB, bytesOnDisk: 0, percent: 0, pinned: false, kept: true,
    state: 'live', paused: true, resolution: '1080p',
    parts: [part(2, 5000 * MB, 0, { paused: true })],
  },
  {
    infoHash: 'pdpd', animeId: 4, title: 'Picture Drama', name: '[Kametsu] Princess Principal - Picture Dramas (BD 1080p Hi10 FLAC)',
    episodes: ['1'], episode: '1', totalBytes: 180 * MB, bytesOnDisk: 12 * MB, percent: 7, pinned: false, kept: true,
    state: 'live', paused: false, mbps: 0.3, peers: 1, group: 'Kametsu', resolution: '1080p', files: 7,
    parts: [{ ...part(4, 180 * MB, 12 * MB), fileIndex: 3 }],
  },
]
const queued = [
  { animeId: 1, epKey: '1', episode: 1, state: 'active', title: 'Crown Handler 1' },
  {
    animeId: 2, epKey: '1', episode: 1, state: 'active', title: 'Crown Handler 2', infoHash: 'new0',
    release: '[SubsPlease] Princess Principal - Crown Handler - 02 (1080p).mkv',
  },
  { animeId: 3, epKey: '1', episode: 1, state: 'pending', title: 'Crown Handler 3' },
]

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
await page.route('**/api/download/queue', (route) => route.fulfill({ json: { items: queued, waiting: {}, paused: false } }))
await page.route('**/api/downloads', (route) => route.fulfill({ json: { items, count: items.length } }))
const calls = []
await page.route(/\/api\/downloads\/pdpd\//, (route) => {
  calls.push(new URL(route.request().url()).pathname)
  return route.fulfill({ json: { ok: true } })
})
const setPace = (value) =>
  page.request.post(`${BASE}/api/prefs`, { data: { key: 'downloads.parallel', value } })
await setPace('1')

await page.goto(`${BASE}/downloads`, { waitUntil: 'domcontentloaded' })
const main = page.locator('main')
await main.getByText('Downloading', { exact: true }).waitFor({ timeout: 10_000 })
await page.waitForTimeout(300)

const pace = main.getByRole('combobox', { name: 'Downloads at once' })
check(await pace.isVisible(), 'the Downloading header carries the downloads-at-once control')
check(/One at a time/.test(await pace.innerText()), 'and says the current pace', await pace.innerText())
check(await main.getByText('plus 1 alongside').isVisible(), 'queue downloads past the pace count as alongside, a download outside the queue does not')

const text = await main.innerText()
check(!/BDRip 1080p x264 FLAC/.test(text), 'a copy being replaced does not wait in line', text.slice(0, 300))
check(/Chosen: \[SubsPlease\] Princess Principal - Crown Handler - 02/.test(text), 'the episode shows the release it is fetching')

const drama = main.locator('li', { hasText: 'Picture Drama' }).first()
check(/Episode 1|episode 1/.test(await drama.innerText()), 'one episode of a many-file torrent gets an episode row', (await drama.innerText()).slice(0, 80))
await drama.getByRole('button', { name: /^Pause/ }).first().click()
check(await until(async () => calls.length > 0), 'pausing it asks for something')
check(calls.some((c) => c === '/api/downloads/pdpd/files/3/pause') && !calls.includes('/api/downloads/pdpd/pause'),
  'and pauses only that episode, not the torrent its later episodes will come from', calls.join(' | '))

await pace.click()
await page.getByRole('option', { name: 'Up to 3 at a time' }).click()
check(await until(() => pace.innerText().then((t) => /Up to 3 at a time/.test(t))), 'picking a pace saves it and shows it')
check(await until(() => main.getByText(/alongside/).count().then((n) => n === 0)), 'three downloads under a pace of three are no longer extra')
const saved = await (await page.request.get(`${BASE}/api/prefs`)).json()
check(saved.effective?.['downloads.parallel'] === '3', 'the setting itself changed', JSON.stringify(saved.effective?.['downloads.parallel']))

await setPace('1')
check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures === 0 ? 'all passed' : `${failures} failed`)
process.exit(failures === 0 ? 0 : 1)

async function until(fn, ms = 5000) {
  for (const end = Date.now() + ms; Date.now() < end; await new Promise((r) => setTimeout(r, 100))) {
    if (await fn().catch(() => false)) return true
  }
  return false
}
