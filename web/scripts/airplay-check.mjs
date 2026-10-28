// AirPlay, Safari's API stood in for: an Apple TV about shows the button, the signed playlist sits beside
// the page's own source, the button opens Safari's list, and unreachable it says why.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 8000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) if (await fn().catch(() => false)) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
// Safari's two pieces: the event that says an Apple TV is about, and the picker.
await page.addInitScript(() => {
  window.WebKitPlaybackTargetAvailabilityEvent = class extends Event {}
  HTMLMediaElement.prototype.webkitShowPlaybackTargetPicker = function () {
    window.__airplayPicked = (window.__airplayPicked ?? 0) + 1
  }
})
const appleTV = () =>
  page.evaluate(() => {
    const e = new Event('webkitplaybacktargetavailabilitychanged')
    e.availability = 'available'
    document.querySelector('video').dispatchEvent(e)
  })

try {
  // Reachable: the opened stream names its AirPlay playlist, as with access from other devices on.
  const airplay = `${BASE}/c/signed/master.m3u8`
  await page.route('**/api/stream/open*', async (route) => {
    const res = await route.fetch()
    route.fulfill({ response: res, json: { ...(await res.json()), airplay } })
  })
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  const video = page.locator('video').first()
  await video.waitFor({ timeout: 30_000 })
  await page.waitForFunction(() => (document.querySelector('video')?.readyState ?? 0) >= 2, null, { timeout: 30_000 }).catch(() => {})

  const button = page.getByRole('button', { name: 'AirPlay', exact: true })
  check(!(await button.isVisible()), 'no AirPlay button before an Apple TV is about')
  const source = await video.evaluate((v) => {
    const s = v.querySelector('source[type="application/x-mpegURL"]')
    return s ? { src: s.src, last: v.lastElementChild === s, remote: !v.disableRemotePlayback } : null
  })
  check(source?.src.startsWith(`${airplay}?sub=`) && source.last && source.remote,
    "the TV's playlist sits after the page's own source, remote playback on, with the subtitles shown here", JSON.stringify(source))
  await appleTV()
  await video.hover()
  check(await until(() => button.isVisible()), 'an Apple TV about shows the AirPlay button')
  await button.click()
  check(await page.evaluate(() => window.__airplayPicked === 1), "it opens Safari's list of them")

  // Unreachable: no playlist to give, so the button says what's missing rather than doing nothing.
  await page.unroute('**/api/stream/open*')
  await page.reload({ waitUntil: 'domcontentloaded' })
  await video.waitFor({ timeout: 30_000 })
  await page.waitForFunction(() => (document.querySelector('video')?.readyState ?? 0) >= 2, null, { timeout: 30_000 }).catch(() => {})
  check(await video.evaluate((v) => !v.querySelector('source[type="application/x-mpegURL"]')), 'without a playlist, no source beside')
  await appleTV()
  await video.hover()
  await until(() => button.isVisible())
  await button.click()
  check(await until(() => page.getByText(/AirPlay needs access from other devices/).first().isVisible()),
    'with Yoru out of the TV’s reach, the button says so')

  // Safari's managed media source stood in: hls.js's own source is a <source> too, emptied on every attach. A
  // recovery (the stall watchdog's: a clock stopped with data ahead) must leave the TV's beside it again.
  const safari = await browser.newPage({ viewport: { width: 1280, height: 800 } })
  safari.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
  await safari.addInitScript(() => {
    window.WebKitPlaybackTargetAvailabilityEvent = class extends Event {}
    window.ManagedMediaSource = window.MediaSource
  })
  await safari.route('**/api/stream/open*', async (route) => {
    const res = await route.fetch()
    route.fulfill({ response: res, json: { ...(await res.json()), airplay } })
  })
  await safari.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  const sources = () => safari.evaluate(() => {
    const v = document.querySelector('video')
    return { sources: [...v.querySelectorAll('source')].map((s) => s.type || s.src.slice(0, 5)), remote: !v.disableRemotePlayback }
  })
  await safari.waitForFunction(() => (document.querySelector('video')?.readyState ?? 0) >= 3, null, { timeout: 30_000 }).catch(() => {})
  let seen = await sources()
  check(seen.sources.length === 2 && seen.sources[1] === 'application/x-mpegURL' && seen.remote,
    "hls.js's source first, the TV's second", JSON.stringify(seen))
  await safari.evaluate(() => {
    const v = document.querySelector('video')
    window.__attaches = 0
    // Every attach sets the blob source again: counted to know a recovery ran.
    new MutationObserver((list) => {
      for (const m of list) for (const n of m.addedNodes) if (n.nodeName === 'SOURCE' && n.type !== 'application/x-mpegURL') window.__attaches++
    }).observe(v, { childList: true })
    v.muted = true
    void v.play()
    const at = v.currentTime + 1
    Object.defineProperty(v, 'currentTime', { configurable: true, get: () => at, set() {} })
  })
  const reattached = await until(() => safari.evaluate(() => window.__attaches > 0), 20_000)
  await safari.evaluate(() => delete document.querySelector('video').currentTime)
  await sleep(500)
  seen = await sources()
  check(reattached && seen.sources.length === 2 && seen.sources[1] === 'application/x-mpegURL' && seen.remote,
    "after a recovery re-attaches, the TV's source is back beside it", `${reattached} ${JSON.stringify(seen)}`)
  await safari.close()

  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
