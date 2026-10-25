// Skips: an opening found in two episodes' shared sound, per-show switches, "Watch it", marking your own,
// AniSkip's "Wrong skip" and kept-skip votes, the first episode's song kept, and Settings' Skipping section.
import { execFileSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { join } from 'node:path'
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const LIB = process.env.YORU_LIB
const SHOTS = process.env.SHOTS ?? '.'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const FFMPEG = [join(process.env.YORU_REPO ?? '..', 'bin', 'ffmpeg.exe'), join(process.env.YORU_REPO ?? '..', 'bin', 'ffmpeg')]
  .find(existsSync) ?? 'ffmpeg'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 10_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
  }
  return false
}
const api = async (path, body) =>
  (await fetch(`${BASE}${path}`, body ? { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) } : {})).json().catch(() => ({}))

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

for (let i = 0; i < 60; i++) {
  try {
    if ((await fetch(`${BASE}/api/setup`)).ok) break
  } catch {}
  await sleep(1000)
}

// Two 5-minute episodes (openings are looked for in the first 45%) share a 60 s song, at 20 s and 45 s.
const LENGTH = 300
const SONG = "aevalsrc='0.4*sin(2*PI*(220+55*floor(mod(t*2,12)))*t)+0.3*sin(2*PI*(330+40*floor(mod(t,7)))*t)':s=44100:d=60"
const tune = (d, s) => `aevalsrc='0.4*sin(2*PI*(${180 + s * 23}+31*floor(mod(t*${3 + (s % 5)},11)))*t)':s=44100:d=${d}`
const make = (name, before, seed) =>
  execFileSync(FFMPEG, ['-hide_banner', '-loglevel', 'error', '-y',
    '-f', 'lavfi', '-i', `testsrc2=size=320x180:rate=12:duration=${LENGTH}`,
    '-f', 'lavfi', '-i', tune(before, seed), '-f', 'lavfi', '-i', SONG, '-f', 'lavfi', '-i', tune(LENGTH - 60 - before, seed + 1),
    '-filter_complex', '[1:a][2:a][3:a]concat=n=3:v=0:a=1[a]', '-map', '0:v', '-map', '[a]',
    // A steady 1 Mbit/s: the library takes nothing under 20 MiB, which is how it tells samples from episodes.
    '-c:v', 'libx264', '-preset', 'ultrafast', '-g', '24', '-b:v', '1M', '-minrate', '1M', '-maxrate', '1M', '-bufsize', '2M',
    '-x264-params', 'nal-hrd=cbr', '-c:a', 'aac', '-shortest', join(LIB, name)])
make('skips-7.mkv', 20, 1)
make('skips-8.mkv', 45, 9)
await api('/api/local/paths', { paths: [LIB] })
await api('/api/local/scan', {})
const files = async () => (await api('/api/local/files')).items ?? []
await until(async () => (await files()).filter((f) => /skips-\d|Test Show - 01/.test(f.path ?? '')).length === 3, 40_000)
for (const f of await files()) {
  const n = /skips-(\d)/.exec(f.path ?? '')?.[1] ?? (/Test Show - 01/.test(f.path ?? '') ? '1' : null)
  if (n) await api('/api/local/assign', { id: f.id, animeId: ANIME, episode: Number(n) })
}
await api('/api/prefs', { key: 'playback.autoplay', value: 'true' })

// Found in the sound: the opening of 8, from what it shares with 7.
const skipsOf = (ep) => api(`/api/episode/skips?anime=${ANIME}&episode=${ep}&duration=${LENGTH}`)
let found
await until(async () => {
  found = ((await skipsOf(8)).ranges ?? []).find((r) => r.kind === 'op' && r.source === 'found')
  return !!found
}, 60_000)
check(!!found && Math.abs(found.start - 45) < 2.5 && Math.abs(found.end - 105) < 3.5, 'the opening is found in the shared sound',
  JSON.stringify(found))
// Saved just after the first, each lined up with the music on its way.
let seven
await until(async () => {
  seven = ((await skipsOf(7)).ranges ?? []).find((r) => r.kind === 'op')
  return !!seven
}, 15_000)
check(!!seven && Math.abs(seven.start - 20) < 2.5, 'and in the episode it was compared with', JSON.stringify(seven))

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 860 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const video = page.locator('video')
const playing = () => until(() => video.evaluate((v) => !v.paused && v.currentTime > 0), 60_000)

// Per show: the switch on the watch page is this show's.
await page.goto(`${BASE}/watch/${ANIME}/8`, { waitUntil: 'domcontentloaded' })
await playing()
const perShow = page.waitForRequest((r) => r.url().endsWith('/api/prefs') && r.postDataJSON()?.key === 'playback.autoskip_op')
await page.getByRole('switch', { name: 'Skip opening' }).click()
const sent = (await perShow).postDataJSON()
check(sent.value === 'true' && sent.animeId === ANIME, 'the watch page sets Skip opening for this show', JSON.stringify(sent))
await sleep(800)

// Auto-skip, then "Watch it" for a moment, which goes back and plays it through.
await page.goto(`${BASE}/watch/${ANIME}/8`, { waitUntil: 'domcontentloaded' })
await playing()
await video.evaluate((v, at) => (v.currentTime = at), found.start - 1.5)
const watchIt = page.getByRole('button', { name: 'Watch it' })
check(await until(() => watchIt.isVisible(), 15_000), 'after skipping the opening it offers to watch it',
  await video.evaluate((v, s) => `at ${v.currentTime.toFixed(1)}s paused=${v.paused} ready=${v.readyState}, opening from ${s}`, found.start))
await page.screenshot({ path: `${SHOTS}/skips-watch-it.png` })
// Its first half second ignores taps: the second of a double tap on Skip.
await sleep(600)
await watchIt.click()
check(await until(() => video.evaluate((v, s) => v.currentTime >= s - 1 && v.currentTime < s + 6, found.start)), 'Watch it goes back to the song')
await sleep(2500)
check(await video.evaluate((v, s) => v.currentTime > s + 1, found.start), 'and plays it rather than skipping again')

// The chip leaves by itself within a few seconds.
await page.goto(`${BASE}/watch/${ANIME}/8`, { waitUntil: 'domcontentloaded' })
await playing()
await video.evaluate((v, at) => (v.currentTime = at), found.start - 1.5)
await until(() => watchIt.isVisible(), 15_000)
check(await until(async () => !(await watchIt.isVisible()), 6000), 'Watch it goes after about three seconds')

// Marking your own ending from the player's skip times menu.
await page.mouse.move(640, 400)
const gear = page.getByRole('button', { name: /^Player settings/ })
await gear.click()
const menu = page.getByRole('menu')
const skipRow = menu.getByRole('menuitem', { name: /^Skip times/ })
check(/1 known/.test(await skipRow.innerText()), 'the menu counts the known skip times', await skipRow.innerText())
await skipRow.click()
check(await menu.getByText('Found by Yoru').isVisible(), 'and says where the opening came from')
await video.evaluate((v) => {
  v.pause()
  v.currentTime = 120
})
await menu.getByRole('menuitemradio', { name: 'Mark where the ending starts' }).click()
// The start is kept while the menu is closed to play on to the end.
await page.keyboard.press('Escape')
await video.evaluate((v) => (v.currentTime = 140))
await page.mouse.move(640, 400)
await gear.click()
check(/Marking/.test(await skipRow.innerText()), 'a started mark waits with the menu closed', await skipRow.innerText())
await skipRow.click()
const marked = page.waitForRequest((r) => r.url().endsWith('/api/episode/skips/mark'))
await menu.getByRole('menuitemradio', { name: 'Mark where it ends' }).click()
const mark = (await marked).postDataJSON()
check(mark.kind === 'ed' && Math.round(mark.start) === 120 && Math.round(mark.end) === 140, 'a marked ending is kept', JSON.stringify(mark))
check(await until(() => menu.getByText('Yours', { exact: true }).isVisible()), 'and listed as yours')
check(await until(() => menu.getByText(/^Ending saved · 2:\d\d–2:\d\d/).isVisible()), 'the menu says what was saved', await menu.innerText())
await page.screenshot({ path: `${SHOTS}/skips-menu.png` })
await menu.getByRole('menuitemradio', { name: 'Forget my marks' }).click()
check(await until(async () => !((await skipsOf(8)).ranges ?? []).some((r) => r.source === 'marked')), 'Forget my marks drops it')
await page.keyboard.press('Escape')

// The same with [ and ]: late in the episode it's the ending, and ] saves it.
const late = await video.evaluate((v) => Math.round(v.duration * 0.7))
await video.evaluate((v, at) => (v.currentTime = at), late)
await page.locator('body').click({ position: { x: 5, y: 400 } })
await page.keyboard.press('[')
// .first(): the notice on screen, then the same words for a screen reader.
check(await until(() => page.getByText(/^Ending starts at .* · \] where it ends/).first().isVisible()), '[ starts an ending mark and says how to finish it')
await video.evaluate((v, at) => (v.currentTime = at), late + 30)
const keyed = page.waitForRequest((r) => r.url().endsWith('/api/episode/skips/mark'))
await page.keyboard.press(']')
const keyMark = (await keyed).postDataJSON()
check(keyMark.kind === 'ed' && Math.abs(keyMark.start - late) < 1 && Math.abs(keyMark.end - late - 30) < 1, '] saves it', JSON.stringify(keyMark))
check(await until(() => page.getByText(/^Ending saved · /).first().isVisible()), 'and the player says it was saved')
await page.keyboard.press(']')
check(await until(() => page.getByText(/^\[ first, where the opening or ending starts/).first().isVisible()), '] with nothing begun says to press [ first')
await api('/api/episode/skips/forget', { animeId: ANIME, episode: 8 })

// AniSkip's own time: "Wrong skip" votes it down; a skip left standing votes it up.
await page.route(`**/api/episode/skips?anime=${ANIME}&episode=8*`, (r) =>
  r.fulfill({ json: { ranges: [{ kind: 'op', start: 30, end: 60, source: 'aniskip', id: 'abc-1' }], finding: false, share: true } }))
await page.goto(`${BASE}/watch/${ANIME}/8`, { waitUntil: 'domcontentloaded' })
await playing()
await video.evaluate((v) => (v.currentTime = 29))
const wrong = page.getByRole('button', { name: 'Wrong skip' })
check(await until(() => wrong.isVisible(), 15_000), 'an AniSkip skip can be called wrong')
const down = page.waitForRequest((r) => r.url().endsWith('/api/episode/skips/vote'))
await sleep(600)
await wrong.click()
check(JSON.parse((await down).postData() ?? '{}').up === false, 'which votes it down')
check(await until(() => video.evaluate((v) => v.currentTime < 35)), 'and goes back to it')
await page.goto(`${BASE}/watch/${ANIME}/8`, { waitUntil: 'domcontentloaded' })
await playing()
const up = page.waitForRequest((r) => r.url().endsWith('/api/episode/skips/vote'), { timeout: 15_000 })
await video.evaluate((v) => (v.currentTime = 29))
check(JSON.parse((await up).postData() ?? '{}').up === true, 'left standing, it votes it up')
await page.unroute(`**/api/episode/skips?anime=${ANIME}&episode=8*`)

// The first episode's opening plays even with skipping on.
await api('/api/prefs', { key: 'playback.autoskip_op', value: 'true' })
await page.route(`**/api/episode/skips?anime=${ANIME}&episode=1*`, (r) =>
  r.fulfill({ json: { ranges: [{ kind: 'op', start: 10, end: 60, source: 'chapters' }], finding: false, share: false } }))
await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
await playing()
await video.evaluate((v) => (v.currentTime = 9))
await sleep(3000)
check(await video.evaluate((v) => v.currentTime > 10 && v.currentTime < 20), "episode 1's opening plays through")

// A recap known only to AniSkip skips with Skip recap scenes on.
await api('/api/prefs', { key: 'playback.autoskip_recap', value: 'true' })
await page.route(`**/api/episode/skips?anime=${ANIME}&episode=7*`, (r) =>
  r.fulfill({ json: { ranges: [{ kind: 'recap', start: 5, end: 50, source: 'aniskip', id: 'rec-1' }], finding: false, share: false } }))
await page.goto(`${BASE}/watch/${ANIME}/7`, { waitUntil: 'domcontentloaded' })
await playing()
await video.evaluate((v) => (v.currentTime = 4))
check(await until(() => video.evaluate((v) => v.currentTime >= 49), 10_000), "AniSkip's recap is skipped")
await page.unroute(`**/api/episode/skips?anime=${ANIME}&episode=7*`)

// Settings: the Skipping section, and the finder available with this ffmpeg.
await page.goto(`${BASE}/settings?tab=Playback`, { waitUntil: 'domcontentloaded' })
check(await until(() => page.getByText('Keep the songs on a first and last episode').isVisible(), 15_000), 'Settings has the Skipping section')
check(await until(() => page.getByText('Available', { exact: true }).isVisible()), 'and says finding them is available')
check(await page.getByText('Share skip times with AniSkip').isVisible(), 'and offers sharing with AniSkip')
await page.getByText('Share skip times with AniSkip').scrollIntoViewIfNeeded()
await page.screenshot({ path: `${SHOTS}/skips-settings.png`, fullPage: false })

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures === 0 ? '\nall passed' : `\n${failures} FAILED`)
process.exit(failures === 0 ? 0 : 1)
