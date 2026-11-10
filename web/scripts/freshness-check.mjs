import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const until = async (fn, ms = 15000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (await fn().catch(() => false)) return true
    await new Promise((r) => setTimeout(r, 100))
  }
  return false
}

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 160)))

const mark = page.locator('header [data-catalogue]')
const state = () => mark.first().getAttribute('data-catalogue')

await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
check(await until(async () => (await state()) === 'fresh'), 'with live answers the top bar says Up to date', (await state()) ?? 'missing')
check(await mark.first().getByText('Up to date').isVisible(), 'in words on a wide screen')
const rgb = (el, prop) => {
  const c = document.createElement('canvas')
  c.width = c.height = 1
  const g = c.getContext('2d', { willReadFrequently: true })
  g.fillStyle = getComputedStyle(el)[prop]
  g.fillRect(0, 0, 1, 1)
  return [...g.getImageData(0, 0, 1, 1).data].slice(0, 3)
}
const green = await mark.first().locator('span').first().evaluate(rgb, 'backgroundColor')
check(green[1] > green[0] + 40 && green[1] > green[2] + 20, 'with a green dot', green.join(','))
const box = await mark.first().boundingBox()
check(!!box && box.x > 720 && box.y < 64, 'at the right of the top bar', JSON.stringify(box))

const asked = new Map()
let behind = true
await page.route('**/api/discover**', async (route) => {
  const url = route.request().url()
  const res = await route.fetch()
  const n = (asked.get(url) ?? 0) + 1
  asked.set(url, n)
  const headers = { ...res.headers() }
  if (behind && n <= 2) {
    headers['x-yoru-refreshing'] = '1'
    delete headers['x-yoru-live']
  }
  await route.fulfill({ response: res, headers })
})
await page.reload({ waitUntil: 'domcontentloaded' })
check(await until(async () => (await state()) === 'updating'), 'while saved answers are being refreshed it says Updating', (await state()) ?? 'missing')
check(await mark.first().getByText('Updating').isVisible(), 'in words')
const violet = await mark.first().locator('span').first().evaluate(rgb, 'borderTopColor')
check(violet[2] > violet[1] + 40 && violet[2] > 150 && violet[0] > 80, 'with a violet spinner', violet.join(','))
await page.screenshot({ path: `${SHOTS}/freshness-updating.png` })
check(await until(() => page.locator('main a[href^="/anime/"]').first().isVisible()), 'the saved answers are on screen meanwhile')
check(
  await until(async () => asked.size > 0 && [...asked.values()].every((n) => n >= 3), 20000),
  'each one is asked again by itself until it is current',
  JSON.stringify([...asked.values()]),
)
behind = false
check(await until(async () => (await state()) === 'fresh', 30000), 'then it says Up to date again', (await state()) ?? 'missing')
const settled = new Map(asked)
await new Promise((r) => setTimeout(r, 5000))
const grew = [...settled].filter(([url, n]) => asked.get(url) !== n).length
check(grew === 0 && (await state()) === 'fresh', 'and stops asking', `${grew} of ${settled.size} asked again`)

await page.unrouteAll({ behavior: 'ignoreErrors' })
const dayAgo = Math.floor(Date.now() / 1000) - 26 * 3600
await page.route('**/api/discover**', async (route) => {
  const res = await route.fetch()
  const headers = { ...res.headers(), 'x-yoru-saved': String(dayAgo) }
  delete headers['x-yoru-live']
  await route.fulfill({ response: res, headers })
})
await page.reload({ waitUntil: 'domcontentloaded' })
check(await until(async () => (await state()) === 'saved'), 'with AniList down it says Saved copy', (await state()) ?? 'missing')
await page.unrouteAll({ behavior: 'ignoreErrors' })

const phone = await browser.newPage({ viewport: { width: 390, height: 800 }, hasTouch: true, isMobile: true })
await phone.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
const small = phone.locator('header [data-catalogue]')
check(await until(async () => (await small.first().getAttribute('data-catalogue')) === 'fresh'), 'a phone shows the mark too')
check((await small.first().getAttribute('aria-label')) === 'Catalogue: Up to date', 'named for screen readers', (await small.first().getAttribute('aria-label')) ?? '')
const fits = await phone.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)
check(fits, 'without widening the page')
await phone.screenshot({ path: `${SHOTS}/freshness-phone.png` })

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
