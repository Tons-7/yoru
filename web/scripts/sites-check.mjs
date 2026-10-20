// Release sites edited in the app: tested, searched with at once (no restart), reordered, removed.
import { createServer } from 'node:http'
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 8000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) if (await fn().catch(() => false)) return true
  return false
}
const sites = async () => (await fetch(`${BASE}/api/sites`)).json()

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

// A TokyoTosho-style feed answering every search with one release.
const feed = createServer((req, res) => {
  res.setHeader('Content-Type', 'application/rss+xml')
  res.end(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><item><category>Anime</category>
<title>[Group] Sites Check Show - 01 (1080p).mkv</title>
<description><![CDATA[<a href="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567">Magnet Link</a><br />Size: 1.2GB]]></description>
<guid><![CDATA[https://index.example/details.php?id=1]]></guid><pubDate>Tue, 01 Sep 2026 15:02:33 GMT</pubDate></item></channel></rss>`)
})
await new Promise((r) => feed.listen(0, '127.0.0.1', r))
const feedURL = `http://127.0.0.1:${feed.address().port}`

const browser = await chromium.launch()
try {
  const before = await sites()
  check(before.saved === false && before.sites.length === 1, "config.toml's site is listed until a list is saved", JSON.stringify(before.sites))

  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
  const errors = []
  page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
  await page.goto(`${BASE}/settings?tab=Downloads`, { waitUntil: 'domcontentloaded' })
  const section = page.locator('section', { hasText: 'Release sites' })
  check(await until(() => section.getByText(/A change here moves the list into Yoru/).isVisible()), 'it says the list is config.toml’s')

  // Test first: a dead address says why, with a way to try again; the working one counts its results.
  await section.getByLabel('Feed type').selectOption('tokyotosho')
  const address = section.getByLabel('Site address')
  await address.fill('http://127.0.0.1:1')
  await section.getByRole('button', { name: 'Test' }).click()
  check(await until(() => section.getByRole('button', { name: 'Try again' }).isVisible(), 25_000), 'a dead site fails its test, with Try again')
  await address.fill(feedURL)
  check(!(await section.getByRole('button', { name: 'Try again' }).isVisible()), 'an edited address drops the old result')
  await section.getByRole('button', { name: 'Test' }).click()
  check(await until(() => section.getByText(/^1 result · /).isVisible(), 15_000), 'a working site counts its results')

  await section.getByRole('button', { name: 'Add' }).click()
  check(await until(async () => (await sites()).saved === true && (await sites()).sites.length === 2), 'Add saves the list in the app')
  check(await until(() => section.getByText(feedURL, { exact: true }).isVisible()), 'and lists the new site')
  const found = await (await fetch(`${BASE}/api/torrents?q=sites+check`)).json()
  check((found.results ?? []).some((r) => r.title.includes('Sites Check Show')), 'a search uses it at once, no restart', JSON.stringify(found).slice(0, 160))

  // Order: the first wins a shared release; moving up changes it, and Remove has an Undo.
  await section.getByRole('button', { name: `Move ${feedURL} up` }).click()
  check(await until(async () => (await sites()).sites[0].url === feedURL), 'Move up puts it first')
  await page.screenshot({ path: `${SHOTS}/sites-settings.png` })
  const dead = (await sites()).sites[1].url
  await section.getByRole('button', { name: `Remove ${dead}` }).click()
  check(await until(async () => (await sites()).sites.length === 1), 'Remove takes it out of the list')
  await page.getByRole('button', { name: 'Undo' }).click()
  check(await until(async () => (await sites()).sites.length === 2), 'Undo puts it back')

  // Setup shows the same editor, for a first run.
  await page.goto(`${BASE}/setup`, { waitUntil: 'domcontentloaded' })
  check(await until(() => page.getByText(feedURL, { exact: true }).isVisible()), 'Setup lists the sites too')
  await page.screenshot({ path: `${SHOTS}/sites-setup.png`, fullPage: true })

  // Phone width: the form wraps, nothing runs off the edge.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(`${BASE}/settings?tab=Downloads`, { waitUntil: 'domcontentloaded' })
  await section.getByLabel('Site address').waitFor()
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  check(overflow <= 0, 'no sideways scroll on a phone', `${overflow}px`)
  await section.screenshot({ path: `${SHOTS}/sites-phone.png` })
  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
  feed.close()
}
console.log(failures === 0 ? '\nall checks passed' : `\n${failures} check(s) failed`)
process.exit(failures === 0 ? 0 : 1)
