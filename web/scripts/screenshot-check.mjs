import { readFileSync } from 'node:fs'
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = process.env.YORU_ANIME ?? '127230'
const SHOTS = process.env.SHOTS ?? '.'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const until = async (fn, ms = 30_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await new Promise((r) => setTimeout(r, 200))) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
  }
  return false
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')
await fetch(`${BASE}/api/prefs`, { method: 'POST', body: JSON.stringify({ key: 'playback.autoplay', value: 'true' }) })

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, acceptDownloads: true })
await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: new URL(BASE).origin })
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
const video = page.locator('video')
check(await until(() => video.evaluate((v) => v.readyState >= 2 && v.currentTime > 1), 60_000), 'the episode plays')
await video.evaluate((v) => v.pause())

const inspect = (bytes) =>
  page.evaluate(async (b64) => {
    const blob = await (await fetch(`data:image/png;base64,${b64}`)).blob()
    const image = await createImageBitmap(blob)
    const canvas = document.createElement('canvas')
    canvas.width = image.width
    canvas.height = image.height
    const context = canvas.getContext('2d')
    context.drawImage(image, 0, 0)
    const { data } = context.getImageData(0, 0, image.width, image.height)
    const colours = new Set()
    for (let i = 0; i < data.length; i += 4 * 97) colours.add((data[i] << 16) | (data[i + 1] << 8) | data[i + 2])
    return { width: image.width, height: image.height, colours: colours.size }
  }, bytes.toString('base64'))

const byKey = page.waitForEvent('download', { timeout: 15_000 })
await page.keyboard.press('g')
const saved = await byKey.catch(() => null)
check(!!saved, 'G saves a file')
if (saved) {
  const name = saved.suggestedFilename()
  check(/ \d+m\d{2}s\.png$/.test(name) && !/[\\/:*?"<>|]/.test(name), 'its name is safe on every system and says the time', name)
  const shot = await inspect(readFileSync(await saved.path()))
  const size = await video.evaluate((v) => [v.videoWidth, v.videoHeight])
  check(shot.width === size[0] && shot.height === size[1], 'it is the full size of the video', `${shot.width}x${shot.height}`)
  check(shot.colours > 8, 'it holds the real picture', `${shot.colours} colours`)
  await saved.saveAs(`${SHOTS}/screenshot.png`)
}
check(await until(() => page.getByText('Screenshot saved and copied').first().isVisible(), 5000), 'the player says it was saved and copied')
const copied = await page.evaluate(async () => (await navigator.clipboard.read()).some((i) => i.types.includes('image/png'))).catch(() => false)
check(copied, 'the picture is on the clipboard')

await page.mouse.move(400, 300)
const button = page.getByRole('button', { name: /^Screenshot/ })
check(await until(() => button.isVisible(), 5000), 'the player bar has the button')
const byButton = page.waitForEvent('download', { timeout: 15_000 })
await button.click()
check(!!(await byButton.catch(() => null)), 'the button saves a file too')

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()

const gpuBrowser = await chromium.launch({
  channel: 'chromium',
  args: ['--autoplay-policy=no-user-gesture-required', '--enable-unsafe-webgpu', '--enable-features=Vulkan'],
})
await fetch(`${BASE}/api/prefs`, { method: 'POST', body: JSON.stringify({ key: 'playback.anime4k', value: 'true' }) })
const gpuContext = await gpuBrowser.newContext({ viewport: { width: 1280, height: 800 }, acceptDownloads: true })
await gpuContext.addInitScript(() => {
  localStorage.setItem('yoru.anime4k', 'light')
  const draw = CanvasRenderingContext2D.prototype.drawImage
  CanvasRenderingContext2D.prototype.drawImage = function (source, ...rest) {
    window.__drawn?.push(source.tagName)
    return draw.call(this, source, ...rest)
  }
})
const gpuPage = await gpuContext.newPage()
await gpuPage.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
const shownCanvas = () =>
  gpuPage.evaluate(() => {
    const canvas = document.querySelector('video')?.previousElementSibling
    return canvas instanceof HTMLCanvasElement && getComputedStyle(canvas).opacity === '1'
  })
const upscaled = await until(shownCanvas, 60_000)
check(upscaled, 'Anime4K is on the picture')
if (upscaled) {
  const capture = async () => {
    await gpuPage.evaluate(() => (window.__drawn = []))
    const file = gpuPage.waitForEvent('download', { timeout: 15_000 })
    await gpuPage.keyboard.press('g')
    const saved = await file.catch(() => null)
    const drawn = await gpuPage.evaluate(() => window.__drawn)
    return !!saved && drawn.includes('CANVAS') && !drawn.includes('VIDEO') ? 'upscaled' : `not upscaled: ${drawn.join(',')}`
  }
  const playingShot = await capture()
  check(playingShot === 'upscaled', 'while playing, the screenshot keeps Anime4K', playingShot)
  await gpuPage.locator('video').evaluate((v) => v.pause())
  await new Promise((r) => setTimeout(r, 800))
  const pausedShot = await capture()
  check(pausedShot === 'upscaled', 'while paused, the screenshot keeps Anime4K', pausedShot)
}
await gpuBrowser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
