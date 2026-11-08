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
const SHOTS = process.env.SMOOTH_SHOTS ?? process.env.SHOTS ?? '.'
const repo = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
mkdirSync(SHOTS, { recursive: true })

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const get = (path) => fetch(BASE + path).then((r) => r.json())
const post = (path, body) =>
  fetch(BASE + path, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body ?? {}) })

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')

let EP = 1
if (REAL) {
  const dir = join(tmpdir(), 'yoru-e2e', 'real')
  mkdirSync(dir, { recursive: true })
  const out = join(dir, 'Yoru Real Show - 02.mkv')
  const ffmpeg = join(repo, 'bin', process.platform === 'win32' ? 'ffmpeg.exe' : 'ffmpeg')
  const cut = spawnSync(ffmpeg, ['-y', '-hide_banner', '-loglevel', 'error', '-ss', process.env.YORU_REAL_AT ?? '420', '-i', REAL, '-t', '240', '-map', '0', '-c', 'copy', out])
  check(cut.status === 0, 'a 4-minute stream copy of a real episode', cut.stderr?.toString().slice(0, 200))
  await post('/api/local/paths', { paths: [process.env.YORU_LIB, dir] })
  await post('/api/local/scan', {})
  let file
  for (let i = 0; i < 40 && !file; i++) {
    await sleep(1000)
    const files = await get('/api/local/files')
    file = (files.items ?? []).find((f) => String(f.path ?? '').includes('Yoru Real Show'))
  }
  const assigned = file && (await post('/api/local/assign', { id: file.id, animeId: ANIME, episode: 2 })).ok
  check(!!assigned, 'the real clip is episode 2 of the test show')
  if (assigned) EP = 2
}

const browser = await chromium.launch({
  headless: process.env.HEADLESS === '1',
  args: ['--autoplay-policy=no-user-gesture-required', '--enable-unsafe-webgpu', '--enable-features=Vulkan', '--force-high-performance-gpu'],
})

const tap = () => {
  window.__delays = []
  const makeDelay = AudioContext.prototype.createDelay
  AudioContext.prototype.createDelay = function (...a) {
    const d = makeDelay.apply(this, a)
    window.__delays.push(d)
    return d
  }
  const connect = AudioNode.prototype.connect
  AudioNode.prototype.connect = function (dest, ...rest) {
    const r = connect.call(this, dest, ...rest)
    if (dest instanceof AudioDestinationNode && !this.__tapped) {
      this.__tapped = true
      const a = this.context.createAnalyser()
      a.fftSize = 2048
      connect.call(this, a)
      window.__analyser = a
    }
    return r
  }
}

async function watch(store, options = {}) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, ...options })
  await context.addInitScript((s) => {
    for (const [k, v] of Object.entries(s)) localStorage.setItem(k, v)
  }, store)
  await context.addInitScript(tap)
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
  page.on('console', (m) => {
    const t = m.text()
    if (m.type() === 'warning' && (t.includes('smooth motion') || t.includes('anime4k'))) errors.push(t.slice(0, 300))
  })
  await page.goto(`${BASE}/watch/${ANIME}/${EP}`, { waitUntil: 'domcontentloaded' })
  const video = page.locator('video')
  await video.waitFor({ timeout: 60000 })
  for (let i = 0; i < 40; i++) {
    if (await video.evaluate((v) => !v.paused && v.currentTime > 1).catch(() => false)) break
    await video.evaluate((v) => v.play().catch(() => {})).catch(() => {})
    await sleep(1000)
  }
  return { context, page, errors, size: await shown(page) }
}

async function shown(page, tries = 30) {
  for (let i = 0; i < tries; i++) {
    const size = await page.evaluate(() => {
      const canvas = document.querySelector('video')?.previousElementSibling
      return canvas instanceof HTMLCanvasElement && getComputedStyle(canvas).opacity === '1' ? `${canvas.width}x${canvas.height}` : ''
    })
    if (size) return size
    await sleep(500)
  }
  return ''
}

const frames = (page, ms = 3000) =>
  page.evaluate(async (ms) => {
    const video = document.querySelector('video')
    const canvas = video?.previousElementSibling
    const shown = []
    let videoFrames = 0
    let ticks = 0
    let handle = 0
    const onFrame = () => {
      videoFrames++
      handle = video.requestVideoFrameCallback(onFrame)
    }
    handle = video.requestVideoFrameCallback(onFrame)
    const t0 = performance.now()
    let nextSample = t0 + 1000
    await new Promise((done) => {
      const step = (now) => {
        ticks++
        if (now >= nextSample) {
          nextSample += 1000
          shown.push(canvas instanceof HTMLCanvasElement ? Number(canvas.dataset.shown ?? 0) : 0)
        }
        if (now - t0 < ms) requestAnimationFrame(step)
        else done()
      }
      requestAnimationFrame(step)
    })
    video.cancelVideoFrameCallback(handle)
    const s = (performance.now() - t0) / 1000
    const canvasRate = shown.length ? shown.reduce((a, b) => a + b, 0) / shown.length : 0
    return { canvas: canvasRate, video: videoFrames / s, display: ticks / s }
  }, ms)
const rate = (f) => `${f.canvas.toFixed(1)}/s on screen, ${f.video.toFixed(1)}/s new pictures in the video, ${f.display.toFixed(0)} Hz`

const brightness = async (page) => {
  const png = (await page.screenshot()).toString('base64')
  return page.evaluate(async (b64) => {
    const bmp = await createImageBitmap(await (await fetch(`data:image/png;base64,${b64}`)).blob())
    const c = new OffscreenCanvas(64, 36)
    const x = c.getContext('2d')
    x.drawImage(bmp, 0, 0, 64, 36)
    const d = x.getImageData(0, 0, 64, 36).data
    let sum = 0
    for (let i = 0; i < d.length; i += 4) sum += (d[i] + d[i + 1] + d[i + 2]) / 3
    return Math.round(sum / (d.length / 4))
  }, png)
}
const lit = { smooth: -1, both: -1, plain: -1 }

const delayMs = (page) => page.evaluate(() => Math.round((window.__delays?.at(-1)?.delayTime.value ?? 0) * 1000))
const level = (page) =>
  page.evaluate(async () => {
    const a = window.__analyser
    if (!a) return -1
    const buf = new Float32Array(a.fftSize)
    let peak = 0
    for (let i = 0; i < 12; i++) {
      a.getFloatTimeDomainData(buf)
      let sum = 0
      for (const x of buf) sum += x * x
      peak = Math.max(peak, Math.sqrt(sum / buf.length))
      await new Promise((r) => setTimeout(r, 60))
    }
    return peak
  })

const gpu = await (async () => {
  const context = await browser.newContext()
  const page = await context.newPage()
  await page.goto(BASE, { waitUntil: 'domcontentloaded' })
  const adapter = await page.evaluate(async () => {
    const a = await navigator.gpu?.requestAdapter()
    return a ? `${a.info.vendor} ${a.info.architecture}`.trim() : ''
  })
  await context.close()
  return adapter
})()
check(!!gpu, 'this browser has WebGPU', gpu)

const setup = await get('/api/setup')
const row = setup.components?.find((c) => c.name === 'flyhigh')
check(!!row && !row.required, 'Setup lists Smooth motion as optional', JSON.stringify(row ?? {}).slice(0, 160))

let status = await get('/api/smooth')
check(status.ready === false && status.canInstall === true, 'a fresh install has no FlyHigh files, and the PC may fetch them', JSON.stringify(status))

if (gpu && !status.ready) {
  const missing = await watch({ 'yoru.smooth': 'on', 'yoru.anime4k': 'off' })
  const banner = await missing.page.getByText("Smooth motion isn't ready on the PC yet").first().isVisible().catch(() => false)
  check(banner, 'turned on without the files: the player says so and plays plain')
  check(!missing.size, 'turned on without the files: no canvas covers the video')
  check(await missing.page.locator('video').evaluate((v) => !v.paused && v.currentTime > 1), 'turned on without the files: playback goes on')

  await missing.page.locator('video').hover()
  await missing.page.getByRole('button', { name: /^Player settings/ }).click()
  const item = missing.page.getByRole('menuitem', { name: /Smooth motion/ })
  check(await item.isVisible().catch(() => false), 'the player menu has Smooth motion')
  await item.click()
  const dialog = missing.page.getByRole('dialog', { name: 'Smooth motion' })
  check(await dialog.isVisible().catch(() => false), 'its dialog opens')
  check(await dialog.getByText('Frame rate').isVisible().catch(() => false), 'the dialog offers a frame rate')
  check(
    !(await dialog.getByText(/Anime4K/).isVisible().catch(() => false)),
    'with Anime4K off, the dialog does not mention it',
  )
  await missing.page.screenshot({ path: join(SHOTS, 'smooth-dialog-missing.png') })
  await dialog.getByRole('button', { name: 'Download' }).click()
  const installed = await dialog
    .getByText(/Installed on the PC: FlyHigh/)
    .waitFor({ timeout: 120000 })
    .then(() => true)
    .catch(() => false)
  check(installed, "the dialog's Download fetches FlyHigh from the fork")
  await missing.page.screenshot({ path: join(SHOTS, 'smooth-dialog-ready.png') })
  await dialog.getByRole('button', { name: 'Close' }).click()
  const after = await shown(missing.page, 40)
  check(!!after, 'once the files land, the playing episode switches to smooth motion by itself', after)
  check(missing.errors.length === 0, 'the missing-files path: no errors', missing.errors.join(' | '))
  await missing.context.close()
}

status = await get('/api/smooth')
if (!status.ready) {
  await post('/api/setup/install/flyhigh')
  for (let i = 0; i < 120 && !status.ready; i++) {
    await sleep(1000)
    status = await get('/api/smooth')
  }
}
check(status.ready === true, 'FlyHigh is installed at the pinned version', JSON.stringify(status))

{
  const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const page = await context.newPage()
  await page.goto(`${BASE}/setup`, { waitUntil: 'domcontentloaded' })
  const listed = await page.getByText('Smooth motion').first().waitFor({ timeout: 15000 }).then(() => true).catch(() => false)
  check(listed, 'the Setup page shows the Smooth motion row')
  await page.screenshot({ path: join(SHOTS, 'smooth-setup.png'), fullPage: true })
  await context.close()
}

if (gpu && status.ready) {
  const s = await watch({ 'yoru.smooth': 'on', 'yoru.anime4k': 'off' })
  check(!!s.size, 'smooth motion alone: the generated picture is shown', s.size)
  await sleep(5000)
  const f = await frames(s.page, 4000)
  check(f.canvas > f.video * 1.4, 'smooth motion alone: more pictures than the video has', rate(f))
  check(f.canvas <= 126, 'Auto stays at or under 120 frames a second', rate(f))
  {
    const box = await s.page.locator('video').boundingBox()
    if (box) await s.page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await s.page.getByRole('button', { name: /^Player settings/ }).click()
    await s.page.getByRole('menuitem', { name: /Smooth motion/ }).click()
    const dialog = s.page.getByRole('dialog', { name: 'Smooth motion' })
    const before = await delayMs(s.page)
    await dialog.getByRole('radiogroup', { name: 'Frame rate' }).getByRole('radio').nth(1).click()
    await sleep(800)
    const kept = await shown(s.page, 2)
    const soundKept = await delayMs(s.page)
    await dialog.getByRole('button', { name: 'Close' }).click()
    await sleep(2500)
    const target = Number(await s.page.evaluate(() => document.querySelector('video')?.previousElementSibling?.dataset?.target ?? 0))
    const after = await frames(s.page, 3000)
    check(
      !!kept && soundKept > 20 && target >= 48 && Math.abs(after.canvas - target) <= target * 0.12,
      'a frame rate picked while playing applies at once, without a restart',
      `kept ${kept}, sound ${before}→${soundKept} ms, ${target} wanted, ${rate(after)}`,
    )
    await s.page.evaluate(() => document.querySelector('video')?.play())
  }
  const info = await (async () => {
    await s.page.keyboard.press('i')
    await sleep(1200)
    const text = await s.page.locator('text=Frame rate').locator('..').innerText().catch(() => '')
    const smoothRow = await s.page.locator('text=Smooth motion').last().locator('..').innerText().catch(() => '')
    await s.page.keyboard.press('i')
    return `${text.replace(/\s+/g, ' ')} | ${smoothRow.replace(/\s+/g, ' ')}`
  })()
  check(/23\.976|24 fps/.test(info) && /frames a second on screen/.test(info), "the info panel shows the episode's rate and what is on screen", info.slice(0, 200))
  const canvas = s.page.locator('video').locator('xpath=preceding-sibling::canvas[1]')
  for (const n of [1, 2, 3]) {
    await canvas.screenshot({ path: join(SHOTS, `smooth-frame-${n}.png`) }).catch(() => {})
    await sleep(70)
  }
  await s.page.screenshot({ path: join(SHOTS, 'smooth-on.png') })

  await sleep(3000)
  const held = await delayMs(s.page)
  check(held >= 20 && held <= 250, 'the sound is held back to match the picture', `${held} ms`)

  const loud = await level(s.page)
  await s.page.locator('video').evaluate((v) => (v.volume = 0.2))
  await sleep(500)
  const quiet = await level(s.page)
  await s.page.locator('video').evaluate((v) => (v.muted = true))
  await sleep(500)
  const muted = await level(s.page)
  await s.page.locator('video').evaluate((v) => {
    v.muted = false
    v.volume = 1
  })
  check(loud > 0.005, 'the delayed sound is playing', loud.toFixed(4))
  check(quiet < loud * 0.5, 'the volume control still works through the delay', `${loud.toFixed(4)} at full, ${quiet.toFixed(4)} at 20%`)
  check(muted < loud * 0.05, 'mute still works through the delay', muted.toFixed(4))

  await s.page.locator('video').evaluate((v) => (v.playbackRate = 1.5))
  await sleep(2500)
  const fast = await frames(s.page, 2500)
  check(fast.canvas > fast.video * 1.2, 'at 1.5x speed it keeps up', rate(fast))
  await s.page.locator('video').evaluate((v) => (v.playbackRate = 1))

  await s.page.keyboard.press('f')
  await sleep(2500)
  const full = await s.page.evaluate(() => !!document.fullscreenElement)
  const fullSize = await shown(s.page)
  const ff = await frames(s.page, 2500)
  check(full && !!fullSize && ff.canvas > ff.video * 1.4, 'in fullscreen it restarts and keeps going', `${full} ${fullSize} ${rate(ff)}`)
  await s.page.screenshot({ path: join(SHOTS, 'smooth-fullscreen.png') })
  lit.smooth = await brightness(s.page)
  await s.page.keyboard.press('f')
  await sleep(2000)
  const back = await frames(s.page, 2000)
  check(back.canvas > back.video * 1.4, 'back out of fullscreen it keeps going', rate(back))

  await s.page.locator('video').evaluate((v) => {
    v.currentTime = Math.min(v.duration - 30, v.currentTime + 60)
  })
  await sleep(3000)
  const sought = await frames(s.page, 2000)
  check(sought.canvas > sought.video * 1.2, 'after a seek it picks up again', rate(sought))

  await s.page.locator('video').evaluate((v) => v.pause())
  await sleep(800)
  const paused = await frames(s.page, 1500)
  check(paused.canvas <= 2, 'paused: the picture holds still', `${paused.canvas.toFixed(1)}/s`)
  await s.page.locator('video').evaluate((v) => v.play())
  await sleep(1500)

  if (await s.page.evaluate(() => 'documentPictureInPicture' in window)) {
    const opened = s.context.waitForEvent('page', { timeout: 15000 }).catch(() => null)
    const box = await s.page.locator('video').boundingBox()
    if (box) await s.page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
    await s.page.getByRole('button', { name: 'Picture in picture' }).click({ timeout: 10000 }).catch(() => {})
    const pip = await opened
    check(!!pip, 'picture-in-picture opens')
    if (pip) {
      await sleep(3000)
      const inPip = await pip.evaluate(() => {
        const v = document.querySelector('video')
        const canvas = v?.previousElementSibling
        return { playing: !!v && !v.paused, plain: !(canvas instanceof HTMLCanvasElement) || getComputedStyle(canvas).opacity !== '1' }
      })
      const pipDelay = await delayMs(s.page)
      check(inPip.playing && inPip.plain && pipDelay <= 5, 'in picture-in-picture it stands aside and the sound is not held back', `${JSON.stringify(inPip)} ${pipDelay} ms`)
      await pip.close()
      await sleep(4000)
      const pipBack = await shown(s.page)
      check(!!pipBack, 'back from picture-in-picture it comes back', pipBack)
    }
  } else {
    console.log('SKIP picture-in-picture is not in this browser build')
  }
  check(s.errors.length === 0, 'smooth motion alone: no errors', s.errors.join(' | '))
  await s.context.close()

  const big = { screen: { width: 2560, height: 1440 } }
  for (const tier of ['light', 'balanced', 'full']) {
    const both = await watch({ 'yoru.smooth': 'on', 'yoru.anime4k': tier }, big)
    const native = await both.page.locator('video').evaluate((v) => `${v.videoWidth}x${v.videoHeight}`)
    check(!!both.size, `with Anime4K ${tier} on a 1440p screen: the picture is shown`, `${both.size} from ${native}`)
    await sleep(5000)
    const fb = await frames(both.page, 4000)
    check(fb.canvas > fb.video * 1.4, `with Anime4K ${tier}: still more pictures than the video has`, rate(fb))
    await both.page.screenshot({ path: join(SHOTS, `smooth-anime4k-${tier}.png`) })
    if (tier === 'balanced') {
      await both.page.keyboard.press('f')
      await sleep(3000)
      const bf = await frames(both.page, 2500)
      const bothSize = await shown(both.page)
      check(!!bothSize && bf.canvas > bf.video * 1.4, 'with Anime4K, in fullscreen it restarts and keeps going', `${bothSize} ${rate(bf)}`)
      await both.page.screenshot({ path: join(SHOTS, 'smooth-anime4k-fullscreen.png') })
      lit.both = await brightness(both.page)
      await both.page.keyboard.press('f')
      await sleep(2000)
    }
    check(both.errors.length === 0, `with Anime4K ${tier}: no errors`, both.errors.join(' | '))
    await both.context.close()
  }

  await post('/api/prefs', { key: 'playback.smooth_held', value: 'true' })
  const cadence = await watch({ 'yoru.smooth': 'on', 'yoru.anime4k': 'off' })
  check(!!cadence.size, 'held drawings smoothed: the picture is shown', cadence.size)
  await sleep(5000)
  const fc = await frames(cadence.page, 4000)
  check(fc.canvas > fc.video * 1.4, 'held drawings smoothed: more pictures than the video has', rate(fc))
  await cadence.page.screenshot({ path: join(SHOTS, 'smooth-held.png') })
  check(cadence.errors.length === 0, 'held drawings smoothed: no errors', cadence.errors.join(' | '))
  await cadence.context.close()
  await post('/api/prefs', { key: 'playback.smooth_held', value: 'false' })

  for (const [pick, name] of [['low', 'Lowest'], ['mid', 'Middle'], ['high', 'Highest']]) {
    await post('/api/prefs', { key: 'playback.smooth_rate', value: pick })
    const r = await watch({ 'yoru.smooth': 'on', 'yoru.anime4k': 'off' })
    await sleep(5000)
    const fr = await frames(r.page, 4000)
    const target = Number(await r.page.evaluate(() => document.querySelector('video')?.previousElementSibling?.dataset?.target ?? 0))
    check(target >= 48 && target <= 120 && Math.abs(fr.canvas - target) <= target * 0.12, `frame rate ${name}: what was picked`, `${target} wanted, ${rate(fr)}`)
    check(r.errors.length === 0, `frame rate ${name}: no errors`, r.errors.join(' | '))
    await r.context.close()
  }
  await post('/api/prefs', { key: 'playback.smooth_rate', value: 'auto' })

  await post('/api/prefs', { key: 'playback.smooth', value: 'true' })
  const follows = await watch({ 'yoru.anime4k': 'off' })
  check(!!follows.size, 'a computer left on Auto follows Settings', follows.size)
  await follows.context.close()
  await post('/api/prefs', { key: 'playback.smooth', value: 'false' })

  const slow = await watch({ 'yoru.smooth': 'on', 'yoru.anime4k': 'off' })
  await sleep(3000)
  await slow.page.evaluate(async () => {
    const adapter = await navigator.gpu.requestAdapter()
    const device = await adapter.requestDevice()
    const module = device.createShaderModule({
      code: `@group(0) @binding(0) var<storage, read_write> o: array<f32>;
@compute @workgroup_size(256) fn main(@builtin(global_invocation_id) g: vec3u) {
  var x = f32(g.x);
  for (var i = 0u; i < 120000u; i++) { x = sin(x) * 1.0001 + cos(x * 0.5); }
  o[g.x % 1024u] = x;
}`,
    })
    const pipeline = device.createComputePipeline({ layout: 'auto', compute: { module, entryPoint: 'main' } })
    const buffer = device.createBuffer({ size: 4096, usage: GPUBufferUsage.STORAGE })
    const group = device.createBindGroup({ layout: pipeline.getBindGroupLayout(0), entries: [{ binding: 0, resource: { buffer } }] })
    window.__hog = true
    const burn = async () => {
      while (window.__hog) {
        for (let n = 0; n < 4; n++) {
          const enc = device.createCommandEncoder()
          const pass = enc.beginComputePass()
          pass.setPipeline(pipeline)
          pass.setBindGroup(0, group)
          pass.dispatchWorkgroups(4096)
          pass.end()
          device.queue.submit([enc.finish()])
        }
        await device.queue.onSubmittedWorkDone()
      }
    }
    void burn()
  })
  const gaveUp = await slow.page
    .getByText('Smooth motion turned itself off for this episode')
    .first()
    .waitFor({ timeout: 45000 })
    .then(() => true)
    .catch(() => false)
  await slow.page.evaluate(() => (window.__hog = false))
  await sleep(1500)
  const stillPlaying = await slow.page.locator('video').evaluate(async (v) => {
    const t = v.currentTime
    await new Promise((r) => setTimeout(r, 3000))
    return v.currentTime - t > 1.5
  })
  const slowDelay = await delayMs(slow.page)
  check(gaveUp && stillPlaying && slowDelay <= 5, 'a GPU that falls behind: it turns itself off, says so, and the sound goes back', `${gaveUp} ${stillPlaying} ${slowDelay} ms`)
  await slow.context.close()

  const off = await watch({ 'yoru.anime4k': 'off' })
  check(!off.size, 'off: nothing covers the video')
  check((await delayMs(off.page)) === 0, 'off: the sound is not held back')
  await off.page.keyboard.press('f')
  await sleep(2500)
  await off.page.screenshot({ path: join(SHOTS, 'plain-fullscreen.png') })
  lit.plain = await brightness(off.page)
  check(off.errors.length === 0, 'off: no errors', off.errors.join(' | '))
  await off.context.close()

  if (lit.plain > 8) {
    check(lit.smooth > lit.plain * 0.4, 'in fullscreen the smooth picture reaches the screen', `${lit.smooth} against ${lit.plain} plain`)
    check(lit.both > lit.plain * 0.4, 'in fullscreen the Anime4K and smooth picture reaches the screen', `${lit.both} against ${lit.plain} plain`)
  } else {
    console.log(`SKIP fullscreen screenshots come out dark even for the plain video (${lit.plain}), so they can't judge`)
  }
}

await browser.close()
console.log(failures ? `${failures} failed` : 'all passed')
process.exit(failures ? 1 : 0)
