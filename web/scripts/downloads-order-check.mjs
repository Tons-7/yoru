import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

const MB = 1 << 20
const items = [{
  infoHash: 'bbbb', animeId: 1, title: 'Princess Principal', name: '[AkihitoSubs] Princess Principal + Extras',
  episodes: ['6', '7'], episode: '6, 7', totalBytes: 800 * MB, bytesOnDisk: 120 * MB, percent: 15,
  pinned: false, kept: true, state: 'paused', paused: true, group: 'AkihitoSubs', resolution: '1080p', codec: 'HEVC',
  parts: [
    { epKey: '6', fileIndex: 11, size: 400 * MB, done: 100 * MB, complete: false, kept: true, pinned: false, paused: false },
    { epKey: '7', fileIndex: 12, size: 400 * MB, done: 20 * MB, complete: false, kept: true, pinned: false, paused: false },
  ],
}]
const q = (ep, state) => ({ animeId: 1, epKey: String(ep), episode: ep, state, title: 'Princess Principal' })
const queued = [q(9, 'pending'), q(6, 'paused'), q(10, 'paused'), q(7, 'paused'), q(11, 'pending')]

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const calls = []
await page.route('**/api/download/queue', (route) => route.fulfill({ json: { items: queued, waiting: {}, paused: false } }))
await page.route('**/api/downloads', (route) => route.fulfill({ json: { items, count: items.length } }))
await page.route(/\/api\/download\/queue\/(arrange|now)$/, async (route) => {
  const body = route.request().postData()
  calls.push(`${route.request().url().split('/').pop()} ${body}`)
  const v = JSON.parse(body)
  if (v.action === 'before' || v.action === 'after') {
    await new Promise((r) => setTimeout(r, 1500))
    const moving = queued.find((q) => q.epKey === v.epKey)
    queued.splice(queued.indexOf(moving), 1)
    const at = queued.findIndex((q) => q.epKey === v.target.epKey)
    queued.splice(v.action === 'after' ? at + 1 : at, 0, moving)
  }
  return route.fulfill({ json: { done: true, moved: true } })
})

await page.goto(`${BASE}/downloads`, { waitUntil: 'domcontentloaded' })
const main = page.locator('main')
await main.getByText('Waiting', { exact: true }).waitFor({ timeout: 10_000 })
await page.waitForTimeout(300)

const waitingText = await main.innerText()
const at = (label) => waitingText.indexOf(label)
check(at('episode 9') < at('episode 6') && at('episode 6') < at('episode 10') && at('episode 10') < at('episode 7') && at('episode 7') < at('episode 11'),
  'started and not-started episodes wait in one line, in queue order')
check(/Paused · 3rd in line/.test(waitingText), 'a paused episode that never started says where it waits')
check(/Paused · 25%/.test(waitingText), 'a started episode paused in line keeps its progress')

const rowOf = (ep) => main.locator('li', { hasText: `episode ${ep}` }).first()
await rowOf(9).getByRole('button', { name: /More for Princess Principal episode 9/ }).click()
check(await page.getByRole('menuitem', { name: 'Move up' }).isDisabled(), 'the first in line cannot move up')
await page.getByRole('menuitem', { name: 'Move down' }).click()
await page.waitForTimeout(200)
check(calls.some((c) => c.startsWith('arrange') && c.includes('"epKey":"9"') && c.includes('"down"')), 'move down asks the queue', calls.join(' | '))

await rowOf(11).getByRole('button', { name: /More for Princess Principal episode 11/ }).click()
check(await page.getByRole('menuitem', { name: 'Move down' }).isDisabled(), 'the last in line cannot move down')
await page.getByRole('menuitem', { name: 'Pause' }).click()
await page.waitForTimeout(200)
check(calls.some((c) => c.includes('"epKey":"11"') && c.includes('"pause"')), 'a waiting episode can be paused in place', calls.join(' | '))

await rowOf(6).getByRole('button', { name: /More for Princess Principal episode 6/ }).click()
await page.getByRole('menuitem', { name: 'Download now' }).click()
await page.waitForTimeout(200)
check(calls.some((c) => c.startsWith('now') && c.includes('"epKey":"6"')), 'a started, paused episode can be downloaded now', calls.join(' | '))

check((await main.getByRole('button', { name: /^Drag to reorder/ }).count()) === 5, 'every episode in line has a drag handle')
const grip = await rowOf(11).getByRole('button', { name: /^Drag to reorder/ }).boundingBox()
const target = await rowOf(9).getByRole('button', { name: /^Drag to reorder/ }).boundingBox()
await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2)
await page.mouse.down()
await page.mouse.move(grip.x + grip.width / 2, grip.y - 10, { steps: 5 })
await page.mouse.move(target.x + target.width / 2, target.y + 4, { steps: 20 })
await page.mouse.up()
await page.waitForTimeout(400)
check(calls.some((c) => c.includes('"epKey":"11"') && c.includes('"before"') && c.includes('"target":{"animeId":1,"epKey":"9"}')),
  'dragging episode 11 onto 9 puts it before 9', calls.filter((c) => c.includes('"11"')).join(' | '))
const after = await main.innerText()
check(after.indexOf('episode 11') < after.indexOf('episode 9'), 'the list moves at once, before the server answers')
await page.waitForTimeout(2500)
const settled = await main.innerText()
check(settled.indexOf('episode 11') < settled.indexOf('episode 9'), 'and stays there once the server has answered')

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures === 0 ? 'all passed' : `${failures} failed`)
process.exit(failures === 0 ? 0 : 1)
