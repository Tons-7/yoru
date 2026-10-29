// Save an episode, open Yoru with the PC out of reach, play it with subtitles, and sync what was watched.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 15_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(250)) if (await fn().catch(() => false)) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')
const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const context = await browser.newContext({ viewport: { width: 1400, height: 1000 } })
// This browser is on Yoru's own machine; the page is told it's another device, which can save.
await context.route('**/api/access', async (route) => {
  const res = await route.fetch()
  await route.fulfill({ response: res, json: { ...(await res.json()), host: false } })
})
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const at = () => page.evaluate(() => document.querySelector('video')?.currentTime ?? -1)
const kept = () =>
  page.evaluate(async () => {
    const urls = (await (await caches.open('yoru-saved')).keys()).map((r) => new URL(r.url).pathname)
    return {
      segments: urls.filter((u) => /\/\d+\.mp4$/.test(u)).length,
      subtitles: urls.filter((u) => u.includes('/subtitle/')).length,
      info: urls.filter((u) => u.endsWith('/info.json')).length,
      apps: (await caches.keys()).filter((k) => k.startsWith('yoru-shell-')).length,
    }
  })
const position = async () => {
  const body = await (await fetch(`${BASE}/api/episodes?id=${ANIME}`)).json()
  return body.items?.find((e) => e.number === 1)?.position ?? 0
}

try {
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  await page.locator('video').first().waitFor({ timeout: 30_000 })
  await page.getByRole('button', { name: 'Save to this device' }).click()
  check(await until(() => page.getByRole('link', { name: 'On this device', exact: true }).isVisible(), 120_000),
    'the episode saves', await page.locator('a[href="/saved"]').first().textContent().catch(() => ''))
  check(await until(async () => (await kept()).apps === 1, 30_000), 'with a copy of the app to open it in')
  const pieces = await kept()
  check(pieces.segments >= 30 && pieces.subtitles >= 1 && pieces.info === 1, 'every piece kept, subtitles too', JSON.stringify(pieces))

  await page.goto(`${BASE}/saved`, { waitUntil: 'domcontentloaded' })
  check(await until(() => page.getByRole('link', { name: 'Play' }).isVisible()), 'it lists under On this device')
  const before = await position()

  const cut = (route) => route.abort('internetdisconnected')
  await context.route('**/*', cut)
  await context.setOffline(true)
  await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' }).catch(() => {})
  check(await until(() => page.getByText("Can't reach Yoru on your PC").isVisible(), 20_000),
    'with the PC out of reach, Yoru opens on what is saved', page.url())
  await page.getByRole('link', { name: 'Play' }).click()
  await page.locator('video').first().waitFor({ timeout: 20_000 })
  await page.evaluate(() => {
    const v = document.querySelector('video')
    v.muted = true
    v.currentTime = 100
    v.play().catch(() => {})
  })
  check(await until(async () => (await at()) > 104, 20_000), 'the saved copy plays offline', String(await at()))
  check(await page.evaluate(() => performance.getEntriesByType('resource').some((e) => e.name.includes('/subtitle/'))),
    'with its subtitles')
  await page.getByRole('link', { name: '‹ On this device' }).click()
  check(await until(() => page.evaluate(() =>
    Object.values(JSON.parse(localStorage.getItem('yoru.saved.watch') || '{}')).some((w) => w.dirty && w.position > 100)),
  ), 'what was watched is kept on the device')

  await context.unroute('**/*', cut)
  await context.setOffline(false)
  check(await until(() => page.evaluate(() => fetch('/api/access').then((r) => r.ok, () => false))), 'the PC answers again')
  await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
  check(await until(async () => new URL(page.url()).pathname === '/'), 'and Yoru opens as usual', page.url())
  check(await until(async () => (await position()) > 100, 30_000), 'back in reach, Yoru has where it was left',
    `${before} → ${await position()}`)

  await page.goto(`${BASE}/saved`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('button', { name: /^Delete .* from this device$/ }).click()
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  check(await until(() => page.getByText('Nothing saved on this device').isVisible()), 'Delete removes it')
  check(await until(async () => { const k = await kept(); return k.segments === 0 && k.apps === 0 }), 'and every piece, the app copy too')

  // A finished download saves from its row in Downloads; the PC's own page has no such button.
  const finished = {
    infoHash: 'cd'.repeat(20), animeId: ANIME, title: 'Yoru Test Show', name: 'Yoru Test Show - 01.mkv', episode: '1',
    episodes: ['1'], totalBytes: 1 << 27, bytesOnDisk: 1 << 27, percent: 100, pinned: false, kept: true, state: 'paused',
  }
  const listed = (route) => route.fulfill({ json: { items: [finished], count: 1 } })
  await context.route('**/api/downloads', listed)
  await page.goto(`${BASE}/downloads`, { waitUntil: 'domcontentloaded' })
  const row = page.getByRole('button', { name: 'Save to this device' })
  check(await until(() => row.isVisible()), 'a finished download offers to save it here')
  await row.click()
  check(await until(() => page.getByRole('button', { name: /^(Saving to|On) this device$/ }).isVisible()), 'and starts saving it')
  const pc = await browser.newPage()
  await pc.route('**/api/downloads', listed)
  await pc.goto(`${BASE}/downloads`, { waitUntil: 'domcontentloaded' })
  await pc.getByRole('button', { name: 'Delete Yoru Test Show' }).waitFor({ timeout: 15_000 })
  check((await pc.getByRole('button', { name: /this device/ }).count()) === 0, 'the PC running Yoru shows no Save button')
  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
