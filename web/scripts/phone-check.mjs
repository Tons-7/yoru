// A phone: bottom tabs, touch on the picture, the floating window's subtitles, installing, manifest shortcuts.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 8000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) if (await fn()) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const context = await browser.newContext({
  viewport: { width: 412, height: 915 },
  hasTouch: true,
  isMobile: true,
  userAgent: 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Mobile Safari/537.36',
})
// The system's floating window, not Chromium's whole-player one, as on a phone.
await context.addInitScript(() => {
  delete window.documentPictureInPicture
  delete Window.prototype.documentPictureInPicture
})
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const cdp = await context.newCDPSession(page)
const touch = (type, points) =>
  cdp.send('Input.dispatchTouchEvent', { type, touchPoints: points.map(([x, y], id) => ({ x, y, id })) })

// ---------------------------------------------------------------- tabs along the bottom
await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
const tabs = page.getByRole('navigation', { name: 'Main' })
check(await until(() => tabs.isVisible()), 'a phone has its tabs along the bottom')
const names = await tabs.getByRole('link').allInnerTexts()
check(names.join(',') === 'Home,Browse,Schedule,Library,Downloads', 'Home, Browse, Schedule, Library, Downloads', names.join(','))
const box = await tabs.boundingBox()
check(!!box && Math.abs(box.y + box.height - 915) < 2, 'at the very bottom', JSON.stringify(box))
await page.screenshot({ path: `${SHOTS}/phone-tabs.png` })
await tabs.getByRole('link', { name: 'Schedule' }).click()
check(await until(() => page.url().endsWith('/schedule')), 'the Schedule tab opens Schedule')
check(await until(async () => (await tabs.getByRole('link', { name: 'Schedule' }).getAttribute('aria-current')) === 'page'), 'and shows it as the current place')
await page.getByRole('tab', { name: 'Recently released' }).click()
check(await until(() => page.getByRole('heading', { name: 'Recently released' }).isVisible()) && page.url().includes('view=recent'),
  'Recently released is a tab of Schedule', page.url())

// ---------------------------------------------------------------- the manifest
const manifest = await (await fetch(`${BASE}/manifest.webmanifest`)).json()
check(manifest.shortcuts?.map((s) => s.name).join(',') === 'Continue watching,Schedule,Downloads,Search', 'the installed app has shortcuts',
  JSON.stringify(manifest.shortcuts?.map((s) => s.name)))
check(manifest.launch_handler?.client_mode?.[0] === 'navigate-existing', 'and opens in the window already open')

// ---------------------------------------------------------------- installing
await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
await page.getByRole('button', { name: 'Profile and settings' }).click()
await page.getByRole('menuitem', { name: 'Install Yoru' }).click()
check(await until(() => page.url().includes('tab=Devices')), 'Install Yoru with no offer from the browser goes to Settings')
check(await until(() => page.getByText('Install on this device', { exact: true }).isVisible()) &&
  (await until(() => page.getByText(/^Use your browser's menu/).isVisible())), 'which says how')
await page.evaluate(() => {
  const offer = new Event('beforeinstallprompt')
  offer.prompt = async () => {
    window.__prompted = true
  }
  offer.userChoice = Promise.resolve({ outcome: 'accepted' })
  window.dispatchEvent(offer)
})
await page.getByRole('button', { name: 'Install Yoru' }).click()
check(await until(() => page.evaluate(() => window.__prompted === true)), "with the browser's offer, one tap asks it")
check(await until(() => page.getByText('Installed.', { exact: true }).isVisible()), 'and says it was installed')

// ---------------------------------------------------------------- touch on the picture
await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
const video = page.locator('video').first()
check(await until(() => video.evaluate((v) => v.readyState >= 2), 60_000), 'the episode loads')
// Played, not just loaded: an episode's start waits for its subtitles to draw.
await until(() => video.evaluate((v) => !v.paused && v.currentTime > 0.2), 30_000)
const vbox = await video.boundingBox()
const at = (fx, fy = 0.5) => [vbox.x + vbox.width * fx, vbox.y + vbox.height * fy]
const state = () => video.evaluate((v) => ({ paused: v.paused, time: v.currentTime, rate: v.playbackRate, cls: v.className }))

await touch('touchStart', [at(0.5)])
await sleep(800)
check((await state()).rate === 2, 'a finger held still plays at 2×', JSON.stringify(await state()))
await touch('touchEnd', [])
await sleep(200)
check((await state()).rate === 1, 'and lifted, back to normal')

await video.evaluate((v) => {
  v.pause()
  v.currentTime = 30
})
await sleep(400)
await touch('touchStart', [at(0.3)])
for (let i = 1; i <= 8; i++) {
  await touch('touchMove', [at(0.3 + (0.25 * i) / 8)])
  await sleep(30)
}
await touch('touchEnd', [])
await sleep(500)
const scrubbed = await state()
check(Math.abs(scrubbed.time - 60) < 3 && scrubbed.paused, 'a sideways drag a quarter across moves 30 s, without playing', JSON.stringify(scrubbed))

// High on the picture, clear of the Skip button.
const pinch = async (from, to) => {
  await touch('touchStart', [at(0.5 - from, 0.25), at(0.5 + from, 0.25)])
  await touch('touchMove', [at(0.5 - (from + to) / 2, 0.25), at(0.5 + (from + to) / 2, 0.25)])
  await touch('touchMove', [at(0.5 - to, 0.25), at(0.5 + to, 0.25)])
  await touch('touchEnd', [])
}
await pinch(0.05, 0.35)
check(await until(async () => /object-cover/.test((await state()).cls)), 'two fingers spread fill the screen')
await pinch(0.35, 0.05)
check(await until(async () => !/object-cover/.test((await state()).cls)), 'and pinched, the whole picture again')

await page.mouse.move(vbox.x + 10, vbox.y + 10)
await page.getByRole('button', { name: 'Fullscreen' }).click()
const full = await until(() => page.evaluate(() => !!document.fullscreenElement), 4000)
if (!full) {
  console.log('skip the swipe out of fullscreen: this browser refused fullscreen')
} else {
  const fbox = { x: 0, y: 0, width: 412, height: 915 }
  await touch('touchStart', [[fbox.width / 2, fbox.height * 0.3]])
  for (let i = 1; i <= 6; i++) {
    await touch('touchMove', [[fbox.width / 2, fbox.height * (0.3 + 0.07 * i)]])
    await sleep(30)
  }
  await touch('touchEnd', [])
  check(await until(() => page.evaluate(() => !document.fullscreenElement)), 'in fullscreen, a swipe down leaves it')
}

// ---------------------------------------------------------------- the system's floating window
const captions = () =>
  video.evaluate((v) => {
    const t = [...v.textTracks].find((tt) => tt.label === 'Subtitles')
    return t ? { mode: t.mode, cues: t.cues?.length ?? 0 } : null
  })
const pipButton = page.getByRole('button', { name: 'Picture in picture' })
await page.mouse.move(vbox.x + 20, vbox.y + 20)
check(await until(() => pipButton.isVisible()), 'with no whole-player window, the video alone floats')
await pipButton.click()
// A headless browser may turn the window down; the player hears the same event either way.
if (!(await until(() => page.evaluate(() => !!document.pictureInPictureElement), 2000))) {
  await video.evaluate((v) => v.dispatchEvent(new Event('enterpictureinpicture')))
}
check(await until(async () => (await captions())?.mode === 'showing' && (await captions()).cues > 0, 15_000),
  'the subtitles go with it, as text', JSON.stringify(await captions()))
await page.evaluate(() => document.pictureInPictureElement && document.exitPictureInPicture())
await video.evaluate((v) => v.dispatchEvent(new Event('leavepictureinpicture')))
check(await until(async () => (await captions())?.mode === 'hidden'), 'and back in the page, the page draws them again', JSON.stringify(await captions()))
await video.evaluate((v) => v.dispatchEvent(new Event('webkitbeginfullscreen')))
check(await until(async () => (await captions())?.mode === 'showing'), "an iPhone's fullscreen shows them as text too")
await video.evaluate((v) => v.dispatchEvent(new Event('webkitendfullscreen')))

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
