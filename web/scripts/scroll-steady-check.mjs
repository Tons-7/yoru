import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

const MB = 1 << 20
const items = Array.from({ length: 40 }, (_, i) => ({
  infoHash: `h${String(i).padStart(3, '0')}`, animeId: 1000 + i, title: `Show ${i}`, name: `[G] Show ${i} - 01 [1080p].mkv`,
  episodes: ['1'], episode: '1', totalBytes: 500 * MB, bytesOnDisk: 500 * MB, percent: 100, pinned: false, kept: true,
  state: 'live', paused: false, group: 'G', resolution: '1080p', parts: [],
}))

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
await page.route('**/api/downloads', (route) => route.fulfill({ json: { items, count: items.length } }))
await page.route('**/api/download/queue', (route) => route.fulfill({ json: { items: [], waiting: {}, paused: false } }))
await page.goto(`${BASE}/downloads`, { waitUntil: 'domcontentloaded' })
await page.locator('main').getByText('Show 39').first().waitFor({ timeout: 10_000 })
await page.waitForTimeout(300)

const y = () => page.evaluate(() => Math.round(window.scrollY))
await page.evaluate(() => window.scrollTo(0, document.scrollingElement.scrollHeight))
await page.waitForTimeout(300)
const bottom = await y()
check(bottom > 1000, 'the long list is scrolled to its end', String(bottom))

await page.locator('#yoru-search').click()
await page.waitForTimeout(200)
check(Math.abs((await y()) - bottom) <= 2, 'clicking the header search keeps the page where it was', `${bottom} → ${await y()}`)
const seen = []
for (const ch of 'frieren') {
  await page.keyboard.type(ch)
  await page.waitForTimeout(150)
  seen.push(await y())
}
check(seen.every((v) => Math.abs(v - bottom) <= 2), 'typing in it does not scroll the page a letter at a time', seen.join(' → '))
await page.getByRole('button', { name: 'Clear search' }).click()
await page.waitForTimeout(200)
check(Math.abs((await y()) - bottom) <= 2, 'clearing it keeps the page where it was', `${bottom} → ${await y()}`)

await page.locator('main').click({ position: { x: 5, y: 5 } })
const under = await page.evaluate(() => {
  const target = [...document.querySelectorAll('main button')].find((b) => b.offsetParent && b.getBoundingClientRect().height > 0)
  if (!target) return null
  window.scrollBy(0, target.getBoundingClientRect().top - 20)
  target.setAttribute('data-probe', '1')
  return true
})
await page.waitForTimeout(200)
if (under) {
  await page.locator('[data-probe]').first().focus()
  await page.waitForTimeout(200)
  const top = await page.locator('[data-probe]').first().evaluate((el) => Math.round(el.getBoundingClientRect().top))
  check(top >= 64, 'a control focused under the header still comes clear of it', String(top))
}

const browse = async (p) => {
  p.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
  await p.route('**/api/browse**', (route) => route.fulfill({ json: { items: [], total: 0, hasMore: false, count: 0, capped: false } }))
  await p.goto(`${BASE}/browse`, { waitUntil: 'domcontentloaded' })
  const field = p.locator('main form[role="search"] input')
  await field.waitFor({ timeout: 10_000 })
  await p.evaluate(() => {
    document.querySelector('main form[role="search"]').parentElement.style.minHeight = '6000px'
    window.scrollTo(0, 2000)
  })
  await p.waitForTimeout(300)
  return field
}

const field = await browse(page)
await field.focus()
await page.waitForTimeout(300)
const fieldTop = await field.evaluate((el) => Math.round(el.getBoundingClientRect().top))
check(fieldTop >= 64, 'on a wide screen, the search field focused from below comes clear of the header', String(fieldTop))

const phone = await browser.newPage({ viewport: { width: 390, height: 844 } })
const phoneField = await browse(phone)
const before = await phone.evaluate(() => Math.round(window.scrollY))
const stuck = await phoneField.evaluate((el) => Math.round(el.getBoundingClientRect().top))
check(stuck >= 0 && stuck < 200, 'on a phone, the search bar is stuck in view while scrolled down', String(stuck))
await phoneField.click()
await phone.waitForTimeout(200)
const typed = [await phone.evaluate(() => Math.round(window.scrollY))]
for (const ch of 'frieren') {
  await phone.keyboard.type(ch)
  await phone.waitForTimeout(150)
  typed.push(await phone.evaluate(() => Math.round(window.scrollY)))
}
check(typed.every((v) => Math.abs(v - before) <= 2), 'on a phone, typing in the stuck search bar keeps the page where it was', `${before} → ${typed.join(' → ')}`)

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures === 0 ? 'all passed' : `${failures} failed`)
process.exit(failures === 0 ? 0 : 1)
