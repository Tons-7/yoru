import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const until = async (fn, ms = 15000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (await fn().catch(() => false)) return true
    await sleep(250)
  }
  return false
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 160)))
const at = () => page.evaluate(() => document.querySelector('video')?.currentTime ?? -1)

let blocked = false
let held = 0
let replays = 0
const open = /^(loaded|health|fonts|thumbnails|thumbs\.jpg|jimaku|playlist\.m3u8)$/
await page.route('**/api/stream/*/*', async (route) => {
  const last = new URL(route.request().url()).pathname.split('/').pop() ?? ''
  if (blocked && route.request().method() === 'GET' && !open.test(last)) {
    held++
    return
  }
  await route.continue()
})
page.on('request', (r) => {
  if (r.method() === 'POST' && new URL(r.url()).pathname === '/api/play') replays++
})

await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
await page.locator('video').first().waitFor({ timeout: 30000 })
await page.evaluate(() => {
  const v = document.querySelector('video')
  v.muted = true
  v.play().catch(() => {})
})
check(await until(async () => (await at()) > 2, 30000), 'the episode plays')

const before = replays
blocked = true
await page.evaluate(() => document.activeElement instanceof HTMLElement && document.activeElement.blur())
await page.keyboard.press('7')
await sleep(6000)
const stuckAt = await at()
await sleep(3000)
check(held > 0 && Math.abs((await at()) - stuckAt) < 0.05, 'a jump whose video never arrives leaves the player stuck', `at ${stuckAt.toFixed(1)}, ${held} requests held`)
check(await until(async () => replays > before, 40000), 'stuck with the episode on disk, the player rebuilds its stream by itself', `${replays - before} rebuilds`)
blocked = false
check(await until(async () => (await at()) > stuckAt + 1.5, 40000), 'and plays on from where it was', `at ${(await at()).toFixed(1)}`)
const rebuilt = replays - before
await sleep(8000)
check(replays - before === rebuilt, 'once, not again while it plays', `${replays - before} rebuilds`)

check(errors.length === 0, 'no page errors', errors.join(' | '))
await page.unrouteAll({ behavior: 'ignoreErrors' })
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
