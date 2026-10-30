import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

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

const tracker = (provider, name, extra) => ({
  provider, name, clientId: 'x', hasSecret: true, configured: true, connected: true,
  redirect: '', register: '', secretRequired: true, ...extra,
})
const counts = {
  total: 214, favourites: 9,
  statuses: { CURRENT: 12, COMPLETED: 180, PAUSED: 3, DROPPED: 5, PLANNING: 14, REPEATING: 0 },
}

const browser = await chromium.launch()
const errors = []
const settled = (menu) => menu.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))

async function open(viewport, trackers) {
  const page = await browser.newPage({ viewport })
  page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
  if (trackers) {
    await page.route('**/api/trackers', (r) => r.fulfill({ json: { trackers } }))
    await page.route('**/api/library/counts', (r) => r.fulfill({ json: counts }))
  }
  await page.goto(`${BASE}/history`, { waitUntil: 'domcontentloaded' })
  const button = page.getByRole('button', { name: 'Profile and settings' })
  await button.waitFor({ timeout: 15_000 })
  return { page, button, menu: page.getByRole('menu', { name: 'Profile' }) }
}

// Nobody signed in: the menu says so and where to fix it.
{
  const { page, button, menu } = await open({ width: 1280, height: 900 })
  await button.click()
  await menu.waitFor()
  check(await menu.getByText('Not signed in').isVisible(), 'signed out, the menu says so')
  check(await menu.getByText('Connect AniList to sync your list').isVisible(), 'and what to do about it')
  check(await menu.getByRole('menuitem', { name: /Watching/ }).isVisible(), 'the list statuses are there')
  await menu.screenshot({ path: `${SHOTS}/profile-menu-signed-out.png` })
  await page.close()
}

// Signed in to both trackers: the initial on the button, the name, the counts.
{
  const { page, button, menu } = await open({ width: 1280, height: 900 }, [
    tracker('anilist', 'AniList', { user: 'Yuki' }),
    tracker('mal', 'MyAnimeList', { user: 'yuki' }),
  ])
  await page.waitForFunction(() => document.querySelector('[aria-label="Profile and settings"]')?.textContent === 'Y')
  check((await button.textContent()) === 'Y', "the button shows the account's initial")
  await button.click()
  await menu.waitFor()
  await page.getByText('All 214').waitFor({ timeout: 5000 }).catch(() => {})
  check(await menu.getByText('Yuki', { exact: true }).isVisible(), 'the account name heads the menu')
  check(await menu.getByText('AniList · MyAnimeList').isVisible(), 'with the trackers it syncs to')
  check(await menu.getByRole('menuitem', { name: 'All 214' }).isVisible(), 'the whole list, counted')
  check((await menu.getByRole('menuitem', { name: /Watching/ }).textContent()) === 'Watching12', 'each status with its count')
  await page.mouse.move(0, 0)
  await settled(menu)
  await menu.screenshot({ path: `${SHOTS}/profile-menu-desktop.png` })
  await page.screenshot({ path: `${SHOTS}/profile-menu-header.png`, clip: { x: 700, y: 0, width: 580, height: 560 } })

  const box = await menu.boundingBox()
  check(box.x + box.width <= 1280, 'the menu stays on screen', `${Math.round(box.x)}..${Math.round(box.x + box.width)}`)

  await menu.getByRole('menuitem', { name: /Watching/ }).click()
  await page.waitForURL(/\/library\?status=CURRENT/, { timeout: 5000 }).catch(() => {})
  check(page.url().includes('/library?status=CURRENT'), 'a status opens the library on it')
  check(!(await menu.isVisible()), 'and the menu closes')

  // Keyboard: Enter opens on the first item, arrows walk, End reaches the last, Escape returns focus.
  await button.focus()
  await page.keyboard.press('Enter')
  await menu.waitFor()
  const focused = () => page.evaluate(() => document.activeElement?.textContent?.trim())
  check((await focused())?.includes('Yuki'), 'opened from the keyboard, focus starts on the first item', await focused())
  await page.keyboard.press('ArrowDown')
  check((await focused()) === 'All 214', 'down moves to the next', await focused())
  await page.keyboard.press('End')
  check((await focused())?.startsWith('Exit Yoru'), 'End reaches the last', await focused())
  await page.keyboard.press('ArrowDown')
  check((await focused())?.includes('Yuki'), 'and down from there wraps to the first', await focused())
  await page.keyboard.press('Escape')
  check(!(await menu.isVisible()), 'Escape closes it')
  check(await button.evaluate((b) => b === document.activeElement), 'and focus goes back to the button')
  await page.close()
}

// A tracker that refused its login: amber on the button and the reason in the menu.
{
  const { page, button, menu } = await open({ width: 1280, height: 900 }, [
    tracker('anilist', 'AniList', { user: 'Yuki', reconnect: true }),
    tracker('mal', 'MyAnimeList', { connected: false, configured: false }),
  ])
  await page.waitForFunction(() => document.querySelector('[aria-label="Profile and settings"] .bg-amber-400'))
  check(true, 'the button carries a dot when a tracker needs signing in again')
  await button.click()
  await menu.waitFor()
  check(await menu.getByText('Reconnect to resume syncing').isVisible(), 'and the menu says why')
  await page.close()
}

for (const width of [390, 360]) {
  const page = await browser.newPage({ viewport: { width, height: 844 } })
  page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
  await page.route('**/api/trackers', (r) => r.fulfill({ json: { trackers: [tracker('anilist', 'AniList', { user: 'Yuki' })] } }))
  await page.goto(`${BASE}/you`, { waitUntil: 'domcontentloaded' })
  const name = page.getByText('Yuki', { exact: true })
  await name.waitFor({ timeout: 15_000 }).catch(() => {})
  check(await name.isVisible(), `at ${width}px the You page names who is signed in`)
  check(!(await page.getByRole('button', { name: 'Profile and settings' }).isVisible()), `at ${width}px it replaces the profile menu`)
  const across = await page.evaluate(() => document.documentElement.scrollWidth)
  check(across <= width, `at ${width}px it fits`, String(across))
  check(!(await page.getByText(/Keyboard shortcuts/).isVisible()), `at ${width}px no keyboard shortcuts row`)
  if (width === 390) await page.screenshot({ path: `${SHOTS}/you-phone.png` })
  await page.close()
}

{
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true })
  const page = await context.newPage()
  await page.goto(`${BASE}/you`, { waitUntil: 'domcontentloaded' })
  await page.getByRole('link', { name: /^Downloads/ }).first().waitFor({ timeout: 15_000 })
  const heights = await page.locator('main li > a, main li > button, main h1 ~ a').evaluateAll((els) =>
    els.filter((el) => el.offsetParent).map((el) => [el.textContent.trim().slice(0, 30), Math.round(el.getBoundingClientRect().height)]))
  const short = heights.filter(([, h]) => h < 44)
  check(heights.length >= 5 && short.length === 0, 'on touch every You row is tall enough to hit',
    short.map(([n, h]) => `${n} ${h}px`).join(', ') || String(heights.length))
  await context.close()
}

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} failed` : '\nall passed')
process.exit(failures ? 1 : 0)
