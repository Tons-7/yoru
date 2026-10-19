// Wi-Fi lost mid-play: downloads cut part way, then the network gone after a seek; playback carries on.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms) => {
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
const context = await browser.newContext({ viewport: { width: 1280, height: 800 } })
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message.split('\n')[0].slice(0, 160)))
const video = page.locator('video')
const now = () => video.evaluate((v) => v.currentTime)
const state = () => video.evaluate((v) => `t=${v.currentTime.toFixed(1)} paused=${v.paused} ready=${v.readyState}`)
const failed = () => page.getByText(/Playback stopped unexpectedly|could not be loaded|Playback failed/).isVisible()

// Each of the first three segment downloads is cut off, as a dropping connection cuts a transfer.
let cut = 0
await page.route(/\/api\/stream\/[^/]+\/\d+\.mp4$/, (route) => (cut++ < 3 ? route.abort('connectionreset') : route.continue()))
await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
check(await until(() => video.evaluate((v) => v.readyState >= 2 && !v.paused && v.currentTime > 2), 60_000), 'plays though its first downloads were cut', await state())
check(cut >= 3, 'the downloads really were cut', `${cut} asked`)
await page.unrouteAll({ behavior: 'ignoreErrors' })

// The network gone right after a seek, while what the seek needs is still to come, then back.
await video.evaluate((v) => (v.currentTime = 90))
await context.setOffline(true)
await sleep(8000)
const stalledAt = await now()
await context.setOffline(false)
check(await until(async () => (await now()) > Math.max(stalledAt, 90) + 4, 45_000), 'playback carries on once the network is back', await state())
check(!(await failed()), 'no failure shown to the viewer')
await page.screenshot({ path: `${SHOTS}/wifi-drop.png` })

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
