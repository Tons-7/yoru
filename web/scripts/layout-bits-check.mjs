import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const until = async (fn, ms = 30_000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
    await new Promise((r) => setTimeout(r, 150))
  }
  return false
}

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

await page.goto(`${BASE}/schedule?view=coming`, { waitUntil: 'domcontentloaded' })
const week = page.getByRole('tab', { name: 'Week', exact: true }).or(page.getByRole('radio', { name: 'Week', exact: true })).first()
check(await until(() => week.isVisible(), 60_000), 'the schedule offers Week')
await week.click()
const days = page.locator('[data-week-day]')
check(await until(() => days.count().then((n) => n === 7), 60_000), 'the week shows seven days')
await page.reload({ waitUntil: 'domcontentloaded' })
check(await until(() => days.count().then((n) => n === 7), 60_000), 'Week is still chosen after a reload')
const heights = await days.evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().height)))
check(new Set(heights).size > 1, 'each day ends after its own episodes', heights.join(' '))
await page.mouse.wheel(0, 3000)
await page.waitForTimeout(600)
const headerTop = await page.getByRole('heading', { level: 2 }).filter({ hasText: /episode/ }).first().evaluate((e) => Math.round(e.getBoundingClientRect().top))
check(headerTop >= 0 && headerTop < 140, 'the day names stay on screen while scrolling', String(headerTop))
await page.screenshot({ path: `${SHOTS}/schedule-week.png` })

const up = page.getByRole('button', { name: 'Scroll to top' })
const down = page.getByRole('button', { name: 'Scroll to bottom' })
check(await until(() => up.isVisible() && down.isVisible(), 5000), 'a long page has the up and down arrows')
await page.evaluate(() => window.scrollTo(0, 400))
await page.waitForTimeout(400)
await down.click()
check(await until(() => page.evaluate(() => window.scrollY + window.innerHeight >= document.documentElement.scrollHeight - 30)), 'down goes to the bottom')
check(await until(() => down.isDisabled()), 'where down rests')
await up.click()
check(await until(() => page.evaluate(() => window.scrollY < 5)), 'up goes to the top')
check(await until(() => up.isDisabled() && down.isEnabled()), 'where up rests and down stays')
await page.goto(`${BASE}/settings?tab=About`, { waitUntil: 'domcontentloaded' })
await page.waitForTimeout(1500)
const short = await page.evaluate(() => document.documentElement.scrollHeight - window.innerHeight < window.innerHeight * 0.5)
if (short) check(await until(() => up.count().then((n) => n === 0), 5000), 'a short page has no arrows')

await page.goto(`${BASE}/browse`, { waitUntil: 'domcontentloaded' })
const tagsPill = page.getByRole('button', { name: /^Tags/ })
check(await until(() => tagsPill.isVisible(), 60_000), 'Search has a Tags filter')
await tagsPill.click()
const yuri = page.getByRole('option', { name: 'Yuri', exact: true })
check(await until(() => yuri.isVisible(), 30_000), 'Yuri is among the popular tags')
await page.screenshot({ path: `${SHOTS}/browse-tags.png` })
await yuri.click()
check(await until(() => new URL(page.url()).searchParams.get('tags') === 'Yuri'), 'picking it lands in the URL', page.url())
await page.keyboard.press('Escape')
check(await until(() => page.getByRole('button', { name: 'Remove Yuri' }).isVisible()), 'as a chip')
check(await until(() => page.locator('a[href^="/anime/"]').count().then((n) => n > 3), 60_000), 'and lists Yuri shows')

const phone = await browser.newPage({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true })
phone.on('pageerror', (e) => errors.push(`phone: ${e.message.slice(0, 200)}`))
await phone.goto(`${BASE}/browse`, { waitUntil: 'domcontentloaded' })
check(await until(() => phone.evaluate(() => document.documentElement.scrollHeight > window.innerHeight * 2), 60_000), 'a long page on a phone')
const arrows = phone.getByRole('group', { name: 'Scroll' })
const opacity = () => arrows.evaluate((e) => Number(getComputedStyle(e).opacity)).catch(() => 0)
await phone.evaluate(() => window.scrollTo(0, 0))
check(await until(async () => (await opacity()) === 0, 8000), 'on a phone the arrows stay out of the way at rest')
await phone.evaluate(() => window.scrollBy(0, 600))
check(await until(async () => (await opacity()) > 0.9, 3000), 'and show while scrolling')
await phone.screenshot({ path: `${SHOTS}/phone-arrows.png` })
check(await until(async () => (await opacity()) === 0, 6000), 'then fade once scrolling stops')
await phone.close()

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
