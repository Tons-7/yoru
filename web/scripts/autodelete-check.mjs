// Auto-delete "keep last 2" on a local swarm (scripts/e2e-swarm, the instance's only site): going back to an
// early episode, then on, keeps the one just left. Run by release-check, as prefetch-check is.
import { execFileSync, spawn } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { createInterface } from 'node:readline'
import { join } from 'node:path'
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const LIB = process.env.YORU_LIB
const REPO = process.env.YORU_REPO
// The same arithmetic as run-player-check.mjs: the engine's peer port, and the indexer port it wrote.
const PORT = Number(new URL(BASE).port)
const PEER_PORT = PORT === 4399 ? 4341 : PORT - 60
const SEED_PORT = PEER_PORT + 1000
const EPISODES = 4
const PROGRESS = 3

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 30_000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
    await sleep(250)
  }
  return false
}
const get = async (p) => (await fetch(BASE + p)).json()
const post = (p, b) =>
  fetch(BASE + p, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(b) })
// Which of the show's episodes the Downloads list still holds.
const held = async () =>
  ((await get('/api/downloads')).items ?? [])
    .filter((d) => d.animeId === ANIME)
    .flatMap((d) => [d.episode, ...(d.episodes ?? [])].filter(Boolean).map(String))
const serverLog = join(LIB, '..', 'server.log')
const logLines = () =>
  readFileSync(serverLog, 'utf8')
    .split('\n')
    .filter((l) => /removed|evict|auto-delete|superseded|orphan/i.test(l))
    .map((l) => l.replace(/^time=\S+ /, ''))

await until(async () => (await fetch(`${BASE}/api/setup`)).ok, 60_000)
const title = (await get(`/api/anime/${ANIME}`)).title
check(!!title, 'the show is known to the instance', title)

// The swarm: one torrent per episode, plus the site that lists them.
const swarm = spawn('go', ['run', './scripts/e2e-swarm',
  '-src', join(LIB, 'Yoru Test Show - 01.mkv'), '-dir', join(LIB, '..', 'swarm'), '-title', title,
  '-count', String(EPISODES), '-listen', String(SEED_PORT), '-peer', `127.0.0.1:${PEER_PORT}`,
  '-http', `127.0.0.1:${PORT + 2000}`,
], { cwd: REPO, stdio: ['ignore', 'pipe', 'inherit'] })
const killSwarm = () => {
  try {
    if (process.platform === 'win32') execFileSync('taskkill', ['/PID', String(swarm.pid), '/T', '/F'], { stdio: 'ignore' })
    else process.kill(-swarm.pid, 'SIGKILL')
  } catch {}
}
process.on('exit', killSwarm)
const ready = await new Promise((resolve) => {
  const lines = createInterface({ input: swarm.stdout })
  const timer = setTimeout(() => resolve(false), 180_000)
  lines.on('line', (line) => {
    if (line.startsWith('{') && JSON.parse(line).ready) {
      clearTimeout(timer)
      resolve(true)
    }
  })
})
check(ready, 'the swarm seeds one torrent per episode')
if (!ready) process.exit(1)

// The user's settings, and a list a few episodes in.
for (const [key, value] of [
  ['cache.autodelete', 'keep2'],
  ['cache.autodelete_downloads', 'true'],
  ['playback.autonext', 'true'],
  ['playback.autoplay', 'true'],
]) {
  await post('/api/prefs', { key, value })
}
check((await post('/api/watched', { animeId: ANIME, episode: PROGRESS })).ok, `progress set to ${PROGRESS}`)

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const playing = () => page.evaluate(() => {
  const v = document.querySelector('video')
  return !!v && v.currentTime > 1 && !v.paused
})

// 1. Episode 1 left mid-way for episode 2: the one just left stays.
await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
check(await until(playing, 120_000), 'episode 1 plays')
check(await until(async () => (await held()).includes('1'), 60_000), 'episode 1 is in Downloads while it plays', JSON.stringify(await held()))
await page.goto(`${BASE}/watch/${ANIME}/2`, { waitUntil: 'domcontentloaded' })
check(await until(playing, 120_000), 'episode 2 plays')
await sleep(10_000)
check((await held()).includes('1'), 'episode 1 SURVIVES leaving it mid-way', JSON.stringify(await held()))

// 2. Back to episode 1, played out, auto-next on to 2: still kept, as the last two watched stay.
await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
check(await until(playing, 120_000), 'episode 1 plays again')
await page.evaluate(() => {
  const v = document.querySelector('video')
  v.currentTime = Math.max(0, v.duration - 8)
  void v.play()
})
check(await until(() => page.url().endsWith(`/watch/${ANIME}/2`), 90_000), 'auto-next moves on to episode 2', page.url())
await until(playing, 120_000)
await sleep(10_000)
check((await held()).includes('1'), 'episode 1 SURVIVES being played to the end', JSON.stringify(await held()))

check(errors.length === 0, 'no page errors', errors.join(' | '))
console.log('\nserver log, deletion-related lines:')
for (const l of logLines()) console.log('  ' + l)
await browser.close()
killSwarm()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
