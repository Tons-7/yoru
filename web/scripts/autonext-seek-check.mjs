// Skipping the ending by hand (a drag to 10 s from the end) still brings the countdown, sound options or not.
//   REAL_EPS="a.mkv;b.mkv" plays two real episodes instead of the generated clips.
import { execFileSync } from 'node:child_process'
import { copyFileSync, existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const LIB = process.env.YORU_LIB
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const SHOTS = process.env.SHOTS ?? '.'

const api = async (p, init) => {
  const res = await fetch(BASE + p, { ...init, headers: { 'content-type': 'application/json' } })
  let body
  try {
    body = await res.json()
  } catch {}
  return { ok: res.ok, body }
}
const post = (p, b) => api(p, { method: 'POST', body: JSON.stringify(b) })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 10000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) if (await fn()) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

for (let i = 0; i < 60; i++) {
  try {
    if ((await api('/api/setup')).ok) break
  } catch {}
  await sleep(1000)
}
const real = process.env.REAL_EPS?.split(';')
if (real) {
  copyFileSync(real[0], join(LIB, 'real-first.mkv'))
  copyFileSync(real[1], join(LIB, 'second.mkv'))
} else {
  // Keyframes every 3.5 s, none past the last 6 s mark: evenly spaced copy segments replayed this end, stuck.
  const exe = process.platform === 'win32' ? 'ffmpeg.exe' : 'ffmpeg'
  const bundled = process.env.YORU_REPO && join(process.env.YORU_REPO, 'bin', exe)
  const ffmpeg = bundled && existsSync(bundled) ? bundled : 'ffmpeg'
  for (const [name, tone] of [['real-first.mkv', 440], ['second.mkv', 880]]) {
    execFileSync(ffmpeg, ['-hide_banner', '-loglevel', 'error', '-y',
      // 720p, or the library scan passes it over as too small to be an episode.
      '-f', 'lavfi', '-i', 'testsrc2=size=1280x720:rate=24000/1001:duration=181',
      '-f', 'lavfi', '-i', `sine=frequency=${tone}:sample_rate=48000:duration=181.5`,
      '-c:v', 'libx264', '-preset', 'veryfast', '-bf', '2', '-pix_fmt', 'yuv420p',
      '-g', '100000', '-sc_threshold', '0', '-force_key_frames', 'expr:gte(t,n_forced*3.5)',
      '-c:a', 'eac3', '-ac', '2', join(LIB, name)])
  }
}
await post('/api/local/paths', { paths: [LIB] })
await post('/api/local/scan', {})
const listed = async (name) => {
  for (let i = 0; i < 60; i++) {
    const f = (await api('/api/local/files')).body?.items?.find((f) => String(f.path ?? '').includes(name))
    if (f) return f
    await sleep(1000)
  }
}
const first = await listed('real-first.mkv')
check(!!first, 'test file listed')
if (!first) process.exit(1)
await post('/api/local/assign', { id: first.id, animeId: ANIME, episode: 1 })
await post('/api/local/scan', {})
const second = await listed('second.mkv')
check(!!second, 'second episode scanned')
if (!second) process.exit(1)
await post('/api/local/assign', { id: second.id, animeId: ANIME, episode: 2 })

await post('/api/prefs', { key: 'playback.autoplay', value: 'true' })
await post('/api/prefs', { key: 'playback.autonext', value: 'true' })
await post('/api/prefs', { key: 'playback.skip_filler', value: 'false' })

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.split('\n')[0].slice(0, 160)))
const video = page.locator('video')
const card = page.getByRole('dialog', { name: 'Up next' })
const state = () =>
  video.evaluate((v) => `t=${v.currentTime.toFixed(1)}/${v.duration.toFixed(1)} paused=${v.paused} ended=${v.ended} ready=${v.readyState}`)

const LOG = process.env.YORU_LOG
let lines = 0
const cases = [
  ['plain', 'false', false],
  ['with headphones on', 'true', false],
  // Watched through first, so the end's report comes back watched, as a viewer's does.
  ['after watching it through', 'true', true],
]
for (const [label, headphones, through] of cases) {
  await post('/api/prefs', { key: 'playback.headphones', value: headphones })
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  check(await until(() => video.evaluate((v) => v.readyState >= 2 && !v.paused), 60_000), `${label}: episode 1 plays`)
  await sleep(3000)
  if (through) {
    // A long episode from 4 minutes before its end: the pass from there still reads to the end before the drag.
    await video.evaluate((v) => v.duration > 300 && (v.currentTime = v.duration - 240))
    // 6x keeps each step under the 2 s that counts as played time.
    await video.evaluate((v) => (v.playbackRate = 6))
    const watched = await until(() => video.evaluate((v) => v.currentTime > v.duration * 0.92), 20 * 60_000)
    check(watched, `${label}: played to 92%`, await state())
    await video.evaluate((v) => (v.playbackRate = 1))
  }
  // A click on the bar near the end, as the viewer did to skip the ending.
  await video.evaluate((v) => (v.currentTime = v.duration - 10))
  const shown = await until(() => card.isVisible(), 40_000)
  check(shown, `${label}: the Up next countdown appears after the drag`, await state())
  if (shown) {
    await page.screenshot({ path: `${SHOTS}/autonext-seek-${headphones}.png` })
    await card.getByRole('button', { name: 'Cancel' }).click()
  }
  // What the page saw, in the server's log: all a log sent from another machine can show.
  if (LOG) {
    const logged = (re) => until(() => re.test(readFileSync(LOG, 'utf8').split('\n').slice(lines).join('\n')), 5000)
    check(await logged(/msg=player anime=\d+ episode=1 event=ended autoNext=true card="up next" episodes=\d+ list=none next=2/),
      `${label}: the end is logged with the card it brought`)
    check(await logged(/event=countdown-cleared afterMs=\d+ by=cancel/), `${label}: and Cancel is logged as what cleared it`)
    lines = readFileSync(LOG, 'utf8').split('\n').length - 1
  }
}
await post('/api/prefs', { key: 'playback.headphones', value: 'false' })

// The last segment never comes: playback stalls short of the end, no countdown, and the log says so.
if (LOG) {
  let last
  await page.route('**/api/stream/*/playlist.m3u8', async (route) => {
    const res = await route.fetch()
    const body = await res.text()
    last = body.trim().split('\n').filter((l) => l.endsWith('.mp4')).pop()
    await route.fulfill({ response: res, body })
  })
  await page.route(/\/api\/stream\/[^/]+\/\d+\.mp4$/, (route) =>
    route.request().url().endsWith(`/${last}`) ? undefined : route.continue())
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  check(await until(() => video.evaluate((v) => v.readyState >= 2 && !v.paused), 60_000), 'held back: episode 1 plays')
  await video.evaluate((v) => (v.currentTime = v.duration - 10))
  const tail = () => readFileSync(LOG, 'utf8').split('\n').slice(lines).join('\n')
  const stuck = await until(() => /event=end-stuck .*ready=\d/.test(tail()), 40_000)
  check(stuck, 'a playback stalled short of the end is logged as stuck', await state())
  // Under 2 s from the end with nothing more to play, the player ends it; further off, it waits.
  const assumed = /event=end-stuck .*assumed=true/.test(tail())
  check(await until(async () => (await card.isVisible()) === assumed, 5000),
    assumed ? 'stuck at the very end, the countdown comes anyway' : 'stuck further off, no countdown', await state())
  await page.unrouteAll({ behavior: 'ignoreErrors' })
}

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
