// Settings as regrouped: nine tabs, old links still landing, and a search that finds a setting in any tab.
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 8000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) if (await fn().catch(() => false)) return true
  return false
}
const prefs = async () => (await (await fetch(`${BASE}/api/prefs`)).json()).effective ?? {}

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

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const visible = (text) => page.getByText(text, { exact: true }).first().isVisible()

await page.goto(`${BASE}/settings`, { waitUntil: 'domcontentloaded' })
const tabs = page.getByRole('tab')
await tabs.first().waitFor({ timeout: 15_000 })
const names = await tabs.allInnerTexts()
check(names.join('|') === 'Playback|Sound & picture|Downloads|Storage|Accounts|Library|Notifications|Devices|About',
  'nine tabs, in the new order', names.join(' | '))
check(await until(() => visible('Auto next episode')), 'Playback opens first, with the automatic options')
check(!(await visible('Download ahead while watching')), 'download ahead is no longer under Playback')

// A stacked row (the control under its words) keeps the control right under them.
const stacked = page.locator('[data-row]', { hasText: 'Subtitle languages' })
const gap = await stacked.evaluate((row) =>
  Math.round(row.lastElementChild.getBoundingClientRect().top - row.firstElementChild.lastElementChild.getBoundingClientRect().bottom))
check(gap >= 0 && gap <= 16, 'a stacked row keeps its control under its words', `${gap}px apart`)
await stacked.screenshot({ path: `${SHOTS}/settings-stacked-row.png` })

// A link from before the regrouping still lands where its settings went.
await page.goto(`${BASE}/settings?tab=Quality`, { waitUntil: 'domcontentloaded' })
check(await until(() => page.getByRole('tab', { name: 'Downloads', selected: true }).isVisible()), 'an old Quality link opens Downloads')
check(await until(() => visible('Download ahead while watching')) && (await visible('Download queue while watching')),
  'both download-while-watching choices sit together there')

// The quality ladder at last has a control: up to 720p is saved as such, and back to the best.
const quality = page.getByLabel('Preferred quality')
check(await until(() => quality.isVisible()), 'Downloads offers a preferred quality')
await quality.selectOption('720p')
check(await until(async () => (await prefs())['quality.prefer'] === '720p'), 'up to 720p is saved')
check(await until(() => page.getByText(/a bigger one only when nothing smaller exists/).isVisible()), 'and says what it means')
await quality.selectOption('best')
check(await until(async () => (await prefs())['quality.prefer'] === 'best'), 'best available is saved back')
await page.screenshot({ path: `${SHOTS}/settings-downloads.png` })

// Search: by a label, by a hint's word, by a keyword, and a whole section by its title.
const search = page.getByRole('searchbox', { name: 'Search settings' })
await search.fill('speed')
check(await until(() => visible('Download speed limit')), '"speed" finds the speed limit')
check(await visible('Show speeds in'), 'and the speed units')
check(!(await visible('Budget')) && !(await visible('Auto next episode')), 'and nothing unrelated')
check(await page.getByRole('button', { name: /^Downloads ›$/ }).isVisible(), 'under the tab they live in')
check(!(await page.getByRole('button', { name: /^Storage ›$/ }).isVisible()), 'a tab with no match drops out')
await page.screenshot({ path: `${SHOTS}/settings-search.png` })
await search.fill('vpn')
check(await until(() => visible('Network for torrents')), '"vpn" finds the network for torrents, by a keyword')
await search.fill('sound')
const soundRows = ['Voice boost', 'Night mode', 'Headphones', 'Mono', 'Volume boost']
check(await until(async () => (await Promise.all(soundRows.map(visible))).every(Boolean)), '"sound" shows the whole Sound section')
await search.fill('zzzz no such thing')
check(await until(() => page.getByText(/No setting matches/).isVisible()), 'no match says so')
await page.getByRole('button', { name: 'Clear search' }).click()
check(await until(async () => (await search.inputValue()) === '' && (await tabs.first().isVisible())), 'Clear search brings the tabs back')

// From a search result to its tab, the search let go.
await search.fill('speed')
await page.getByRole('button', { name: /^Downloads ›$/ }).click()
check(await until(async () => new URL(page.url()).searchParams.get('tab') === 'Downloads' && !new URL(page.url()).searchParams.get('q')),
  'a result heading opens its tab', page.url())

// Another screen can link straight to one setting.
await page.goto(`${BASE}/settings?q=download%20queue`, { waitUntil: 'domcontentloaded' })
check(await until(() => visible('Download queue while watching')), 'a ?q= link opens on the setting it names')
check(!(await visible('Download ahead while watching')), 'and only that one')

// Escape clears the search.
await search.press('Escape')
check(await until(async () => !new URL(page.url()).searchParams.get('q')), 'Escape clears the search')

// A phone: the tabs are a menu, searching still works, and nothing pushes the page sideways.
await page.setViewportSize({ width: 390, height: 844 })
await page.goto(`${BASE}/settings?tab=Downloads`, { waitUntil: 'domcontentloaded' })
check(await until(() => page.getByLabel('Settings section').isVisible()), 'a phone gets the tabs as a menu')
check(await until(() => visible('Preferred quality')), 'open on the tab the link names')
const over = () => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
check((await over()) <= 0, 'no sideways scroll on a phone', `${await over()}px over`)
await page.screenshot({ path: `${SHOTS}/settings-phone.png`, fullPage: true })
await search.fill('budget')
check(await until(() => visible('Budget')), 'searching works on a phone too')
check((await over()) <= 0, 'and its results fit the width', `${await over()}px over`)
await page.screenshot({ path: `${SHOTS}/settings-phone-search.png` })

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
