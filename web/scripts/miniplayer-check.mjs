// The miniplayer: leaving the watch page while playing docks the same video, keys left to the page; Play,
// Expand and Close work; leaving while paused ends it, as before.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 15_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(200)) if (await fn().catch(() => false)) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')
const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const playing = () => page.evaluate(() => { const v = document.querySelector('video'); return !!v && !v.paused })
const time = () => page.evaluate(() => document.querySelector('video')?.currentTime ?? -1)
const dock = page.getByRole('button', { name: 'Close the player' })

try {
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  await page.locator('video').first().waitFor({ timeout: 30_000 })
  await page.evaluate(() => { const v = document.querySelector('video'); v.muted = true; v.play().catch(() => {}) })
  check(await until(async () => (await playing()) && (await time()) > 1, 30_000), 'the episode plays')
  // A mark on the element: the same video after docking, not a new one.
  await page.evaluate(() => (document.querySelector('video').dataset.mark = 'same'))

  await page.getByRole('link', { name: 'Home', exact: true }).first().click()
  check(await until(async () => new URL(page.url()).pathname === '/'), 'Home opens')
  check(await until(() => dock.isVisible()), 'the episode docks in a corner')
  check(await page.evaluate(() => document.querySelector('video')?.dataset.mark === 'same'), 'the same video, not reloaded')
  const before = await time()
  await sleep(1500)
  check((await time()) > before && (await playing()), 'and keeps playing', `${before} → ${await time()}`)

  await page.keyboard.press(' ')
  await sleep(400)
  check(await playing(), "Space is the page's, not the docked player's")

  await page.getByRole('button', { name: 'Pause', exact: true }).click()
  check(await until(async () => !(await playing())), 'Pause on the dock pauses it')
  await page.getByRole('button', { name: 'Play', exact: true }).click()
  check(await until(() => playing()), 'and Play plays it')

  // Held and moved: let go past the middle, it settles in the top-right corner, and a press still presses.
  const box = () => page.locator('video').first().evaluate((v) => v.closest('.fixed').getBoundingClientRect().toJSON())
  const start = await box()
  await page.mouse.move(start.x + 40, start.y + 40)
  await page.mouse.down()
  await page.mouse.move(start.x - 260, 300, { steps: 8 })
  const held = await box()
  // Every frame after letting go: it glides on from there, never back to where it started first.
  const frames = page.evaluate(() => new Promise((done) => {
    const el = document.querySelector('video').closest('.fixed')
    const seen = []
    const end = performance.now() + 600
    const tick = () => {
      const r = el.getBoundingClientRect()
      seen.push([r.left, r.top])
      if (performance.now() < end) requestAnimationFrame(tick)
      else done(seen)
    }
    requestAnimationFrame(tick)
  }))
  await page.mouse.up()
  const path = await frames
  await sleep(200)
  const moved = await box()
  const steps = path.slice(1).map(([x, y], i) => [x - path[i][0], y - path[i][1]])
  const smooth = steps.every(([dx, dy]) => dx >= -1 && dy <= 1 && Math.hypot(dx, dy) < 120)
  check(Math.abs(path[0][0] - held.x) < 5 && Math.abs(path[0][1] - held.y) < 5 && smooth, 'let go, it glides from there without jumping',
    `${JSON.stringify(path[0])} held at ${Math.round(held.x)},${Math.round(held.y)}; ${path.length} frames`)
  check(moved.top < 120 && moved.right > 1300, 'dragged up, it docks top right', JSON.stringify({ top: moved.top, right: moved.right }))
  check(await playing(), 'and the drag paused nothing')
  await page.getByRole('button', { name: 'Pause', exact: true }).click()
  check(await until(async () => !(await playing())), 'a press after a drag still works')
  await page.getByRole('button', { name: 'Play', exact: true }).click()

  await page.getByRole('link', { name: 'Search', exact: true }).first().click()
  check(await until(async () => new URL(page.url()).pathname === '/browse') && (await dock.isVisible()), 'it stays docked page to page')
  check((await box()).top < 120, 'in the corner it was put in')

  await page.getByRole('button', { name: 'Expand the player' }).first().click()
  check(await until(async () => new URL(page.url()).pathname === `/watch/${ANIME}/1`), 'Expand goes back to the watch page')
  await sleep(500)
  const marks = await page.evaluate(() => [...document.querySelectorAll('video')].map((v) => v.dataset.mark ?? 'none'))
  check(marks.length === 1 && marks[0] === 'same' && !(await dock.isVisible()), 'the same video, full size again', JSON.stringify(marks))

  await page.getByRole('link', { name: 'Home', exact: true }).first().click()
  await until(() => dock.isVisible())
  await dock.click()
  check(await until(async () => (await page.locator('video').count()) === 0), 'Close ends it')

  // Paused when you leave: nothing docks.
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  await page.locator('video').first().waitFor({ timeout: 30_000 })
  await page.evaluate(() => document.querySelector('video').pause())
  await page.getByRole('link', { name: 'Home', exact: true }).first().click()
  await sleep(800)
  check((await page.locator('video').count()) === 0 && !(await dock.isVisible()), 'left paused, it ends as before')

  // A phone: a bar above the tabs, clear of the page's width.
  await page.evaluate(() => localStorage.removeItem('yoru.mini.corner'))
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  await page.locator('video').first().waitFor({ timeout: 30_000 })
  await page.evaluate(() => { const v = document.querySelector('video'); v.muted = true; v.play().catch(() => {}) })
  await until(() => playing())
  await page.locator('video').first().hover()
  await page.getByRole('button', { name: 'Minimize', exact: true }).click()
  check(await until(() => dock.isVisible()), 'on a phone Minimize docks it')
  const bar = await page.locator('video').first().evaluate((v) => v.closest('.fixed')?.getBoundingClientRect().toJSON())
  const tabs = await page.locator('nav').last().boundingBox()
  check(bar && tabs && bar.bottom <= tabs.y + 1 && bar.right <= 390, 'as a bar above the tabs', JSON.stringify({ bar, tabs }))
  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
