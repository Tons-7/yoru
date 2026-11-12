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
check(names.join('|') ===
  'Playback|Language and display|Sound and picture|Accounts|Library|Notifications|Downloads|Storage|Devices|This computer|About',
  'eleven tabs, in order', names.join(' | '))
check(await until(() => visible('Auto next episode')), 'Playback opens first, with the automatic options')
check(!(await visible('Download ahead while watching')), 'download ahead is not under Playback')
check(!(await visible('Show titles as')) && !(await visible('Open Yoru')), 'nor language or how Yoru runs')

const section = (title) => page.locator('section', { has: page.getByRole('heading', { name: title, exact: true }) })
check(await section('Casting').getByText('Draw subtitles into the picture', { exact: true }).isVisible(),
  'Casting holds its subtitles choice')
check(await section('Casting').getByText(/Cast button in the player's bar/).isVisible(), 'and says where casting starts')
await page.screenshot({ path: `${SHOTS}/settings-playback.png`, fullPage: true })
await page.getByRole('tab', { name: 'This computer' }).click()
check(await until(() => section('This computer').getByText('Open Yoru', { exact: true }).isVisible()),
  'This computer holds how Yoru opens')
if (process.platform === 'win32') {
  check(await section('This computer').getByText('When the window closes', { exact: true }).isVisible(), 'and what closing it does')
}
check(await section('This computer').getByText('Stay awake while downloading', { exact: true }).isVisible(), 'and staying awake')
await page.getByRole('tab', { name: 'Language and display' }).click()
check(await until(() => visible('Show titles as')) && (await visible('Hide spoilers')), 'Language and display holds titles and spoilers')

const stacked = page.locator('[data-row]', { has: page.getByText('Subtitle languages', { exact: true }) })
const gap = await stacked.evaluate((row) =>
  Math.round(row.lastElementChild.getBoundingClientRect().top - row.firstElementChild.lastElementChild.getBoundingClientRect().bottom))
check(gap >= 0 && gap <= 16, 'a stacked row keeps its control under its words', `${gap}px apart`)
await stacked.screenshot({ path: `${SHOTS}/settings-stacked-row.png` })

await page.goto(`${BASE}/settings?tab=Quality`, { waitUntil: 'domcontentloaded' })
check(await until(() => page.getByRole('tab', { name: 'Downloads', selected: true }).isVisible()), 'an old Quality link opens Downloads')
check(await until(() => visible('Download ahead while watching')) && (await visible('Download queue while watching')),
  'both download-while-watching choices sit together there')

const quality = page.getByRole('combobox', { name: 'Preferred quality' })
check(await until(() => quality.isVisible()), 'Downloads offers a preferred quality')
await quality.click()
await page.getByRole('option', { name: 'Up to 720p', exact: true }).click()
check(await until(async () => (await prefs())['quality.prefer'] === '720p'), 'up to 720p is saved')
check(await until(() => page.getByText(/a bigger one only when nothing smaller exists/).isVisible()), 'and says what it means')
await quality.click()
await page.getByRole('option', { name: 'Best available', exact: true }).click()
check(await until(async () => (await prefs())['quality.prefer'] === 'best'), 'best available is saved back')
await page.screenshot({ path: `${SHOTS}/settings-downloads.png` })

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
await search.fill('chromecast')
check(await until(() => visible('Draw subtitles into the picture')), '"chromecast" finds the Casting section')
await search.fill('tray')
check(await until(() => visible('When the window closes')) || process.platform !== 'win32', '"tray" finds what closing the window does')
await search.fill('sound')
const soundRows = ['Voice boost', 'Night mode', 'Headphones', 'Mono', 'Volume boost']
check(await until(async () => (await Promise.all(soundRows.map(visible))).every(Boolean)), '"sound" shows the whole Sound section')
await search.fill('zzzz no such thing')
check(await until(() => page.getByText(/No setting matches/).isVisible()), 'no match says so')
await page.getByRole('main').getByRole('button', { name: 'Clear search' }).last().click()
check(await until(async () => (await search.inputValue()) === '' && (await tabs.first().isVisible())), 'Clear search brings the tabs back')
const x = search.locator('xpath=..').getByRole('button', { name: 'Clear search' })
check((await x.count()) === 0, 'an empty search box shows no X')
await search.fill('speed')
check(await until(() => x.isVisible()), 'typing shows an X in the box')
await x.click()
check(await until(async () => (await search.inputValue()) === '' && (await tabs.first().isVisible())), 'the X empties it and brings the tabs back')
check(await search.evaluate((el) => el === document.activeElement), 'with the cursor still in the box')
await search.fill('speed')
await search.press('Escape')
check(await until(async () => (await search.inputValue()) === ''), 'Escape empties it too')

await search.fill('speed')
await page.getByRole('button', { name: /^Downloads ›$/ }).click()
check(await until(async () => new URL(page.url()).searchParams.get('tab') === 'Downloads' && !new URL(page.url()).searchParams.get('q')),
  'a result heading opens its tab', page.url())

await page.goto(`${BASE}/settings?q=download%20queue`, { waitUntil: 'domcontentloaded' })
check(await until(() => visible('Download queue while watching')), 'a ?q= link opens on the setting it names')
check(!(await visible('Download ahead while watching')), 'and only that one')

await search.press('Escape')
check(await until(async () => !new URL(page.url()).searchParams.get('q')), 'Escape clears the search')

await page.setViewportSize({ width: 390, height: 844 })
await page.goto(`${BASE}/settings`, { waitUntil: 'domcontentloaded' })
const sections = page.getByRole('navigation', { name: 'Settings sections' })
check(await until(() => sections.getByRole('link', { name: /^Downloads/ }).isVisible()), 'a phone gets the tabs as a menu')
await page.goto(`${BASE}/settings?tab=Downloads`, { waitUntil: 'domcontentloaded' })
check(await until(() => visible('Preferred quality')), 'open on the tab the link names')
const over = () => page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
check((await over()) <= 0, 'no sideways scroll on a phone', `${await over()}px over`)
await page.screenshot({ path: `${SHOTS}/settings-phone.png`, fullPage: true })
await page.goto(`${BASE}/settings`, { waitUntil: 'domcontentloaded' })
await search.fill('budget')
check(await until(() => visible('Budget')), 'searching works on a phone too')
check((await over()) <= 0, 'and its results fit the width', `${await over()}px over`)
await page.screenshot({ path: `${SHOTS}/settings-phone-search.png` })

await page.setViewportSize({ width: 1280, height: 900 })
await page.goto(`${BASE}/settings?tab=Storage`, { waitUntil: 'domcontentloaded' })
const cleared = page.waitForRequest((r) => r.url().includes('/api/downloads/clear?scope=completed'))
await page.getByRole('button', { name: 'Clear cache' }).click()
check(await page.getByText("Streamed episodes go; what's playing or downloading, and downloads you keep, stay").isVisible(),
  'Clear cache asks first, saying what stays')
await page.getByRole('button', { name: 'Clear cache' }).click()
await cleared
check(await until(() => page.getByText(/^Nothing to clear: /).isVisible()), 'and says what it cleared')
const hint = page.locator('[data-row]', { hasText: 'In use' }).locator('p').nth(1)
await hint.evaluate((el) => {
  window.__hints = []
  new MutationObserver(() => window.__hints.push(el.textContent)).observe(el, { childList: true, characterData: true, subtree: true })
})
const again = page.waitForResponse((r) => r.url().includes('/api/downloads/clear'))
await page.getByRole('button', { name: 'Clear cache' }).click()
await page.getByRole('button', { name: 'Clear cache' }).click()
await again
await sleep(500)
const seen = await page.evaluate(() => window.__hints)
check(!seen.some((t) => / cached · .* downloaded$/.test(t)), 'pressed again, it never blinks back', JSON.stringify(seen))

let passwordFields = 0
let masked = 0
for (const tab of ['Playback', 'Language', 'Sound', 'Accounts', 'Library', 'Notifications', 'Downloads', 'Storage', 'Devices', 'Computer', 'About', 'New']) {
  await page.goto(`${BASE}/settings?tab=${tab}`, { waitUntil: 'domcontentloaded' })
  await sleep(700)
  passwordFields += await page.locator('input[type="password"]').count()
  masked += await page.locator('input[data-secret]').evaluateAll((els) => els.filter((el) => getComputedStyle(el).webkitTextSecurity === 'disc').length)
}
check(passwordFields === 0, 'no Settings tab has a password-type field for a password manager to fill', `${passwordFields} found`)
const box = page.getByRole('searchbox', { name: 'Search settings' })
check((await box.getAttribute('autocomplete')) === 'off' && (await box.inputValue()) === '', 'and the Settings search box takes no saved entries', await box.inputValue())
console.log(`     secret fields seen masked: ${masked}`)

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
