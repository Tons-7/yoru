import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
const top = page.locator('section', { has: page.getByText('Top 10', { exact: true }) }).first()
await top.locator('a[href^="/anime/"]').first().waitFor({ timeout: 60000 })
const ranks = (await top.getByText(/^Number \d+$/).allTextContents()).map((t) => Number(t.replace(/\D/g, '')))
check(ranks.length === 10 && ranks.every((n, i) => n === i + 1), 'Top 10 ranks ten shows in order', ranks.join(','))
await top.screenshot({ path: `${SHOTS}/top-ten.png` })

// Recently released: the hover is the full panel.
await page.goto(`${BASE}/recent`, { waitUntil: 'domcontentloaded' })
const card = page.locator('a[href^="/watch/"]').first()
await card.waitFor({ timeout: 60000 })
await page.mouse.move(0, 0)
// The middle of the art: its top-left corner holds the Up next toggle, a control of its own.
await card.hover()
const panel = page.locator('div.fixed.z-50.rounded-sheet')
const shown = await panel.waitFor({ timeout: 5000 }).then(() => true, () => false)
const text = shown ? await panel.innerText() : await page.locator('body').innerText()
check(shown, 'hover panel opens on a released card')
check(/\beps?\b/i.test(text) && /Play ep \d+/.test(text), 'it has the episode count and plays this episode', text.slice(0, 160).replace(/\n/g, ' | '))
const box = await panel.boundingBox().catch(() => null)
if (box) await page.screenshot({ path: `${SHOTS}/released-hover.png`, clip: box })

// Settings: Off / Next episode / Next 2 episodes, and it sticks.
await page.goto(`${BASE}/settings?tab=Downloads`, { waitUntil: 'domcontentloaded' })
const select = page.getByRole('combobox', { name: 'Download ahead while watching' })
await select.waitFor({ timeout: 30000 })
await select.click()
await page.getByRole('option', { name: 'Next 2 episodes', exact: true }).click()
await page.waitForTimeout(800)
await page.reload({ waitUntil: 'domcontentloaded' })
await select.waitFor({ timeout: 30000 })
check((await select.innerText()) === 'Next 2 episodes', 'next 2 episodes is saved', await select.innerText())
const prefs = await (await fetch(`${BASE}/api/prefs`)).json().catch(() => ({}))
const eff = prefs.effective ?? {}
check(eff['cache.prefetch_next'] === 'true' && eff['cache.prefetch_count'] === '2', 'both settings written', JSON.stringify([eff['cache.prefetch_next'], eff['cache.prefetch_count']]))
await select.click()
await page.getByRole('option', { name: 'Off', exact: true }).click()
await page.waitForTimeout(800)
const off = await (await fetch(`${BASE}/api/prefs`)).json().catch(() => ({}))
check(off.effective?.['cache.prefetch_next'] === 'false', 'off turns it off')
await select.scrollIntoViewIfNeeded()
await page.screenshot({ path: `${SHOTS}/download-ahead.png` })

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
