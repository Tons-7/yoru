import { chromium } from 'playwright'
import { spawnSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const REAL = process.env.YORU_REAL
const SECONDS = Number(process.env.PROBE_SECONDS ?? 60)
const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const get = (path) => fetch(BASE + path).then((r) => r.json())
const post = (path, body) =>
  fetch(BASE + path, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body ?? {}) })

if (!(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }))) throw new Error('no test episode')
let episode = 1
if (REAL) {
  const dir = join(tmpdir(), 'yoru-e2e', 'real')
  mkdirSync(dir, { recursive: true })
  const out = join(dir, 'Yoru Real Show - 02.mkv')
  const ffmpeg = join(repo, 'bin', process.platform === 'win32' ? 'ffmpeg.exe' : 'ffmpeg')
  const cut = spawnSync(ffmpeg, ['-y', '-hide_banner', '-loglevel', 'error', '-ss', process.env.YORU_REAL_AT ?? '420', '-i', REAL, '-t', '240', '-map', '0', '-c', 'copy', out])
  if (cut.status !== 0) throw new Error(`could not cut the real episode: ${cut.stderr}`)
  await post('/api/local/paths', { paths: [process.env.YORU_LIB, dir] })
  await post('/api/local/scan', {})
  let file
  for (let i = 0; i < 40 && !file; i++) {
    await sleep(1000)
    file = ((await get('/api/local/files')).items ?? []).find((f) => String(f.path ?? '').includes('Yoru Real Show'))
  }
  if (!file || !(await post('/api/local/assign', { id: file.id, animeId: ANIME, episode: 2 })).ok) throw new Error('the real clip did not become episode 2')
  episode = 2
}

let status = await get('/api/smooth')
if (!status.ready) {
  await post('/api/setup/install/flyhigh')
  for (let i = 0; i < 120 && !status.ready; i++) {
    await sleep(1000)
    status = await get('/api/smooth')
  }
}
if (!status.ready) throw new Error('FlyHigh did not install')

const browser = await chromium.launch({
  headless: process.env.HEADLESS === '1',
  args: ['--autoplay-policy=no-user-gesture-required', '--enable-unsafe-webgpu', '--enable-features=Vulkan', '--force-high-performance-gpu'],
})

const record = () => {
  window.__shown = []
  const take = GPUCanvasContext.prototype.getCurrentTexture
  GPUCanvasContext.prototype.getCurrentTexture = function (...a) {
    window.__shown.push(performance.now())
    return take.apply(this, a)
  }
}

const quantile = (sorted, q) => sorted[Math.min(sorted.length - 1, Math.floor(q * sorted.length))] ?? 0

async function measure(label, prefs, rateMode) {
  for (const [key, value] of Object.entries(prefs)) await post('/api/prefs', { key, value })
  await post('/api/prefs', { key: 'playback.smooth_rate', value: rateMode })
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } })
  await context.addInitScript(() => {
    localStorage.setItem('yoru.smooth', 'on')
    localStorage.setItem('yoru.anime4k', 'off')
  })
  await context.addInitScript(record)
  const page = await context.newPage()
  const said = []
  page.on('console', (m) => {
    const t = m.text()
    if (t.includes('smooth motion') || t.includes('FlyHigh')) said.push(`${m.type()}: ${t.slice(0, 160)}`)
  })
  page.on('pageerror', (e) => said.push(`error: ${e.message.slice(0, 160)}`))
  await page.goto(`${BASE}/watch/${ANIME}/${episode}`, { waitUntil: 'domcontentloaded' })
  const video = page.locator('video')
  await video.waitFor({ timeout: 60000 })
  for (let i = 0; i < 40; i++) {
    if (await video.evaluate((v) => !v.paused && v.currentTime > 1).catch(() => false)) break
    await video.evaluate((v) => v.play().catch(() => {})).catch(() => {})
    await sleep(1000)
  }
  await video.evaluate((v) => (v.currentTime = 2))
  await sleep(8000)
  const target = Number(await page.evaluate(() => document.querySelector('video')?.previousElementSibling?.dataset?.target ?? 0))
  const from = await video.evaluate((v) => v.currentTime)
  await page.evaluate(() => (window.__shown = []))
  await sleep(SECONDS * 1000)
  const times = await page.evaluate(() => window.__shown)
  const to = await video.evaluate((v) => v.currentTime)
  const gaveUp = await page.getByText('Smooth motion turned itself off').first().isVisible().catch(() => false)
  const stillShown = await page.evaluate(() => {
    const canvas = document.querySelector('video')?.previousElementSibling
    return canvas instanceof HTMLCanvasElement && getComputedStyle(canvas).opacity === '1'
  })
  await context.close()

  const gaps = times.slice(1).map((t, i) => t - times[i])
  const sorted = [...gaps].sort((a, b) => a - b)
  const span = (times.at(-1) - times[0]) / 1000 || 1
  const long = gaps.filter((g) => g > 50).reduce((a, b) => a + b, 0) / 1000
  const perSecond = []
  for (let s = 0; s < Math.floor(span); s++) {
    const lo = times[0] + s * 1000
    perSecond.push(times.filter((t) => t >= lo && t < lo + 1000).length)
  }
  const plain = perSecond.filter((n) => n <= 26).length
  const full = perSecond.filter((n) => target > 0 && n >= target * 0.88).length
  const uneven = []
  for (let s = 0; s < Math.floor(span); s++) {
    const lo = times[0] + s * 1000
    const g = gaps.filter((_, i) => times[i] >= lo && times[i] < lo + 1000).sort((a, b) => a - b)
    if (g.length >= 4) uneven.push(quantile(g, 0.95) / Math.max(0.5, quantile(g, 0.05)))
  }
  uneven.sort((a, b) => a - b)
  console.log(
    JSON.stringify({
      run: label,
      engine: status.version,
      rate: rateMode,
      target,
      clip: `${from.toFixed(0)}-${to.toFixed(0)}s`,
      picturesPerSecond: +(times.length / span).toFixed(1),
      gapMs: { p50: +quantile(sorted, 0.5).toFixed(1), p95: +quantile(sorted, 0.95).toFixed(1), p99: +quantile(sorted, 0.99).toFixed(1), max: +(sorted.at(-1) ?? 0).toFixed(1) },
      shareOfTimeInGapsOver50ms: +(long / span).toFixed(3),
      secondsAtPlain24: `${plain}/${perSecond.length}`,
      secondsAtFullRate: `${full}/${perSecond.length}`,
      longOverShortGapWithinASecond: { median: +quantile(uneven, 0.5).toFixed(1), worstTenth: +quantile(uneven, 0.9).toFixed(1) },
      gaveUp,
      stillShown,
    }),
  )
  if (process.env.PROBE_DETAIL) {
    console.log('per second: ' + perSecond.join(' '))
    console.log('said: ' + (said.length ? said.slice(0, 12).join(' | ') : 'nothing'))
  }
}

for (let n = 1; n <= Number(process.env.PROBE_REPEAT ?? 0); n++) await measure(`default, time ${n}`, { 'playback.smooth_held': 'false' }, 'auto')
if (process.env.PROBE_ONLY !== 'held') await measure('default', { 'playback.smooth_held': 'false' }, 'auto')
if (process.env.PROBE_ONLY !== 'default') await measure('held drawings smoothed', { 'playback.smooth_held': 'true' }, 'auto')
await post('/api/prefs', { key: 'playback.smooth_held', value: 'false' })
await browser.close()
