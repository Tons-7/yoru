// Real episodes downloaded through nyaa as a viewer's Yoru would, each one's end played: the countdown must come.
//   YORU_EXTRA_CONFIG='[[indexer]] ...' ANILIST_LIVE=1 YORU_SHOW_ID=<id> EPISODES=1,2 [HEADED=1 CHANNEL=chrome ANIME4K=1] node scripts/run-player-check.mjs end-probe.mjs
import { readFileSync } from 'node:fs'
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ID = Number(process.env.YORU_SHOW_ID)
const EPISODES = (process.env.EPISODES ?? '1').split(',').map(Number)
const LOG = process.env.YORU_LOG
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const api = async (p, body) => {
  const res = await fetch(BASE + p, body ? { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) } : {})
  return res.json().catch(() => ({}))
}
const until = async (fn, ms) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(500)) if (await fn().catch(() => false)) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

for (const [key, value] of [['playback.autoplay', 'true'], ['playback.autonext', 'true'], ['playback.skip_filler', 'false']]) {
  await api('/api/prefs', { key, value })
}

// Release search matches titles against the corpus, which a fresh instance seeds in its first seconds.
if (LOG) check(await until(async () => /corpus seeded/.test(readFileSync(LOG, 'utf8')), 180_000), 'title corpus seeded')

// Downloaded whole first, as an episode waiting in the queue is by the time it is watched.
for (const ep of EPISODES) await api('/api/download', { animeId: ID, episode: ep })
const t0 = Date.now()
let last = 0
const done = await until(async () => {
  const items = ((await api('/api/downloads')).items ?? []).filter((d) => d.animeId === ID)
  if (Date.now() - last > 30_000) {
    last = Date.now()
    for (const d of items) console.log(`  ${Math.round((Date.now() - t0) / 1000)}s  ep ${d.episode}: ${d.percent.toFixed(1)}% of ${(d.totalBytes / 2 ** 30).toFixed(2)} GB, ${d.peers ?? 0} peers  ${d.name}`)
  }
  return EPISODES.every((ep) => items.some((d) => d.episodes?.includes(String(ep)) && d.percent >= 100))
}, Number(process.env.DOWNLOAD_MINUTES ?? 60) * 60_000)
check(done, 'every episode downloaded')
for (const d of ((await api('/api/downloads')).items ?? []).filter((d) => d.animeId === ID)) console.log(`  release: ep ${d.episode}  ${d.name}`)
if (!done) process.exit(1)

await api('/api/prefs', { key: 'playback.anime4k', value: process.env.ANIME4K === '1' ? 'true' : 'false' })
const browser = await chromium.launch({
  headless: process.env.HEADED !== '1',
  channel: process.env.CHANNEL || undefined,
  args: ['--autoplay-policy=no-user-gesture-required'],
})
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.split('\n')[0].slice(0, 160)))
const video = page.locator('video')
const card = page.getByRole('dialog', { name: 'Up next' })
const state = () =>
  video.evaluate((v) => `t=${v.currentTime.toFixed(1)}/${v.duration.toFixed(1)} paused=${v.paused} ended=${v.ended} ready=${v.readyState}`)
let opened
page.on('response', async (res) => {
  if (res.url().includes('/api/stream/open') && res.ok()) opened = await res.json().catch(() => null)
})
let lines = LOG ? readFileSync(LOG, 'utf8').split('\n').length : 0
const notes = () => {
  const all = readFileSync(LOG, 'utf8').split('\n')
  const mine = all.slice(lines).filter((l) => /msg=player |stream session opened|msg=segment|not produced/.test(l))
  lines = all.length - 1
  return mine
}

// Watching late in the episode first, as a viewer reaches the end, then the drag.
const cases = [['false', false], ['true', false], ['true', true]]
for (const ep of EPISODES) {
  for (const [headphones, late] of cases) {
    const label = `ep ${ep}${headphones === 'true' ? ', headphones' : ''}${late ? ', watched at 92% first' : ''}`
    await api('/api/prefs', { key: 'playback.headphones', value: headphones })
    opened = null
    await page.goto(`${BASE}/watch/${ID}/${ep}`, { waitUntil: 'domcontentloaded' })
    check(await until(() => video.evaluate((v) => v.readyState >= 2 && !v.paused), 120_000), `${label}: plays`)
    await sleep(3000)
    if (late) {
      await video.evaluate((v) => (v.currentTime = v.duration * 0.92))
      check(await until(() => video.evaluate((v) => v.currentTime > v.duration * 0.92 + 20), 90_000), `${label}: plays on there`, await state())
    }
    // The player's length against the server's: far apart means the timeline was stretched.
    const lengths = await video.evaluate((v) => v.duration)
    const planned = opened?.duration ?? opened?.info?.duration
    check(planned > 0 && Math.abs(lengths - planned) < 2, `${label}: the player's length is the file's`, `${lengths.toFixed(1)} vs ${planned}`)
    // Skipping the ending by hand: a click on the bar 10 s before the end, as the mouse does it (the late case).
    if (late) {
      await page.mouse.move(640, 300)
      const bar = page.locator('.group\\/scrub')
      await bar.waitFor({ state: 'visible', timeout: 10_000 })
      const box = await bar.boundingBox()
      const at = await video.evaluate((v) => (v.duration - 10) / v.duration)
      await page.mouse.click(box.x + box.width * at, box.y + box.height / 2)
    } else {
      await video.evaluate((v) => (v.currentTime = v.duration - 10))
    }
    const shown = await until(() => card.isVisible(), 60_000)
    check(shown, `${label}: the countdown comes after the drag`, await state())
    await page.screenshot({ path: `${SHOTS}/end-probe-${ep}-${headphones}.png` })
    if (shown) await card.getByRole('button', { name: 'Cancel' }).click()
    if (LOG) for (const l of notes()) console.log(`    ${l.replace(/^time=\S+ /, '').slice(0, 260)}`)
  }
}
await api('/api/prefs', { key: 'playback.headphones', value: 'false' })

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
