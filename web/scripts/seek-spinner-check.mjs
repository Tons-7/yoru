import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const LIB = process.env.YORU_LIB
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const SHOTS = process.env.MANUAL_SHOTS ?? process.env.SHOTS ?? '.'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 20000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (await fn().catch(() => false)) return true
    await sleep(150)
  }
  return false
}

const prepared = await prepareLocalEpisode({ base: BASE, lib: LIB, anime: ANIME })
check(prepared, 'test file listed')
if (!prepared) process.exit(1)

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.split('\n')[0].slice(0, 160)))

const video = page.locator('video')
const spinner = page.locator('.group\\/player .animate-spin')
const spinnerDuring = async (ms) => {
  const end = Date.now() + ms
  let seen = false
  while (Date.now() < end) {
    if (await spinner.first().isVisible().catch(() => false)) seen = true
    await sleep(40)
  }
  return seen
}

await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
check(await until(() => video.evaluate((v) => v.readyState >= 1), 60_000), 'episode loads')
check(await until(() => video.evaluate((v) => !v.paused), 20_000), 'it starts playing')

await video.evaluate((v) => {
  v.currentTime = 40
})
check(await until(() => video.evaluate((v) => v.currentTime > 39 && v.readyState >= 3), 30_000),
  'buffered and playing well past the start')
check(!(await spinnerDuring(700)), 'no spinner sitting there before the seek')

await page.mouse.move(640, 400)
await page.keyboard.press('k')
check(await until(() => video.evaluate((v) => v.paused), 5000), 'k paused it')
const before = await video.evaluate((v) => v.currentTime)

await page.keyboard.press('ArrowLeft')
const flashed = await spinnerDuring(1200)
check(!flashed, 'a paused arrow-key seek shows no spinner')

const after = await video.evaluate((v) => v.currentTime)
check(after < before - 3, 'and it actually moved back', `${before.toFixed(1)} → ${after.toFixed(1)}`)
check(await video.evaluate((v) => v.paused), 'and it stayed paused')
await page.screenshot({ path: `${SHOTS}/paused-seek.png` })

await page.keyboard.press('k')
check(await until(() => video.evaluate((v) => !v.paused), 5000), 'k resumed it')
await page.keyboard.press('ArrowRight')
check(!(await spinnerDuring(1200)), 'a short seek while playing shows no spinner either')
check(await until(() => video.evaluate((v) => !v.paused), 5000), 'still playing after the seek')

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures === 0 ? '\nall checks passed' : `\n${failures} check(s) failed`)
process.exit(failures === 0 ? 0 : 1)
