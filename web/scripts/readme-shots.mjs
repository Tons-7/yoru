// The README's screenshots from a scratch instance into SHOTS; SHOW, EPISODE and AT (seconds) pick the frame.
//   ANILIST_LIVE=1 YORU_EXTRA_CONFIG=<a real indexer> CHECK=readme-shots.mjs node scripts/run-player-check.mjs
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const shots = process.env.SHOTS ?? '.'
const SHOW = Number(process.env.SHOW ?? 154587)
const EPISODE = Number(process.env.EPISODE ?? 2)
const AT = Number(process.env.AT ?? 305)

const api = async (path, init) => {
  const res = await fetch(BASE + path, { ...init, headers: { 'content-type': 'application/json' } })
  return res.json().catch(() => undefined)
}
const post = (path, body) => api(path, { method: 'POST', body: JSON.stringify(body) })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 15_000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (await fn().catch(() => false)) return true
    await sleep(250)
  }
  return false
}

// The player shot streams a real episode; nothing is shared back while it does.
await post('/api/prefs', { key: 'downloads.upload', value: 'never' })

// A few of what's trending on the Up next list, each at an episode already out; seen once so covers are stored.
const trending = (await api('/api/discover?sort=trending&perPage=12'))?.items ?? []
let listed = 0
for (const show of trending) {
  if (listed === 3) break
  const episode = show.nextEpisode ? show.nextEpisode - 1 : Math.min(show.episodes ?? 1, 3)
  if (episode < 1 || show.isAdult) continue
  await api(`/api/anime/${show.id}`)
  await post('/api/upnext', { animeId: show.id, episode })
  listed++
}
console.log(`${listed} shows on Up next`)

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 }, deviceScaleFactor: 1 })
// Every image on screen drawn, so no shot catches a grey box.
const settled = () =>
  until(() =>
    page.evaluate(() =>
      [...document.images]
        .filter((img) => {
          const box = img.getBoundingClientRect()
          return box.top < innerHeight && box.bottom > 0 && box.width > 0
        })
        .every((img) => img.complete && img.naturalWidth > 0),
    ),
  )

await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
await until(() => page.getByRole('heading', { name: 'Up next', exact: true }).isVisible())
// The banner, from AniList, is the slowest part of the page.
await until(() => page.locator('h1').first().isVisible(), 60_000)
await settled()
await sleep(1500)
await page.screenshot({ path: `${shots}/home.jpg`, type: 'jpeg', quality: 85 })

await page.setViewportSize({ width: 1280, height: 1048 })
await page.goto(`${BASE}/anime/${SHOW}`, { waitUntil: 'domcontentloaded' })
await until(() => page.getByRole('heading', { name: 'Episodes' }).isVisible(), 30_000)
await until(() => page.locator('h1').first().isVisible(), 60_000)
// Episode stills come from another source a moment after the list.
await until(() => page.locator('li img').count().then((n) => n >= 3), 60_000)
await settled()
await sleep(1500)
await page.screenshot({ path: `${shots}/series.jpg`, type: 'jpeg', quality: 85 })

// A real episode from a real swarm: the frame at AT, paused with the controls up.
await page.setViewportSize({ width: 1280, height: 800 })
await page.goto(`${BASE}/watch/${SHOW}/${EPISODE}`, { waitUntil: 'domcontentloaded' })
const video = page.locator('video')
if (!(await until(() => video.evaluate((v) => v.readyState >= 1), 300_000))) {
  console.error('the episode never started')
  process.exit(1)
}
await video.evaluate((v, at) => {
  v.currentTime = at
  return v.play().catch(() => {})
}, AT)
await until(() => video.evaluate((v, at) => v.currentTime > at + 2 && v.readyState >= 3, AT), 180_000)
await video.evaluate((v) => v.pause())
await page.mouse.move(640, 300)
await sleep(1200)
await page.screenshot({ path: `${shots}/player.jpg`, type: 'jpeg', quality: 85 })

await browser.close()
console.log(`shots in ${shots}`)
