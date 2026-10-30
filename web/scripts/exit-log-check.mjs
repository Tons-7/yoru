// With no console, Yoru's log and its exit are in the app: About saves and copies the log, Playback says what
// closing the window does, and Exit Yoru asks only when something would stop. Last, as it ends the instance.
import { readFileSync } from 'node:fs'
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 10_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(200)) if (await fn().catch(() => false)) return true
  return false
}
const prefs = async () => (await (await fetch(`${BASE}/api/prefs`)).json()).effective ?? {}
const alive = () => fetch(`${BASE}/api/health`).then((r) => r.ok, () => false)

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

// A Windows host's browser, whose page offers the tray choice.
const browser = await chromium.launch()
const context = await browser.newContext({
  viewport: { width: 1280, height: 900 },
  acceptDownloads: true,
  permissions: ['clipboard-read', 'clipboard-write'],
})
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

// ---------------------------------------------------------------- the log, in About
await page.goto(`${BASE}/settings?tab=About`, { waitUntil: 'domcontentloaded' })
const save = page.getByRole('link', { name: 'Save log' })
check(await until(() => save.isVisible(), 20_000), 'About offers the log')
check(await page.getByRole('button', { name: 'Show in folder' }).isVisible(), 'and shows it in its folder')
const [download] = await Promise.all([page.waitForEvent('download'), save.click()])
const saved = readFileSync(await download.path(), 'utf8')
check(/^yoru-log-.+\.txt$/.test(download.suggestedFilename()), 'Save log names a text file', download.suggestedFilename())
check(saved.includes('==== This run') && saved.includes('msg='), 'holding this run\'s log', saved.slice(0, 120))
await page.getByRole('button', { name: 'Copy log' }).click()
check(await until(() => page.getByRole('button', { name: 'Copied' }).isVisible()), 'Copy log copies it')
const clip = await page.evaluate(() => navigator.clipboard.readText()).catch(() => '')
check(clip.includes('==== This run'), 'the log is on the clipboard', clip.slice(0, 80))

// ---------------------------------------------------------------- closing the window, in Playback
await page.goto(`${BASE}/settings?tab=Computer`, { waitUntil: 'domcontentloaded' })
const closing =page.locator('[data-row]', { has: page.getByText('When the window closes', { exact: true }) })
if (process.platform === 'win32') {
  check(await until(() => closing.isVisible(), 15_000), 'Playback says what closing the window does')
  check(await closing.getByText('Yoru exits').isVisible(), 'by default Yoru exits with it')
  await closing.getByRole('combobox').click()
  await page.getByRole('option', { name: 'Keep running in the tray', exact: true }).click()
  check(await until(async () => (await prefs())['window.on_close'] === 'tray'), 'keeping it in the tray is saved')
  check(await until(() => closing.getByText(/tray icon opens or exits it/).isVisible()), 'and says where Yoru then is')
  await closing.getByRole('combobox').click()
  await page.getByRole('option', { name: 'Exit Yoru', exact: true }).click()
  check(await until(async () => (await prefs())['window.on_close'] === 'exit'), 'back to exiting')
} else {
  check(!(await closing.isVisible()), 'no tray choice where there is no tray')
}

// ---------------------------------------------------------------- Exit Yoru, from the profile menu
const openMenu = () => page.getByRole('button', { name: 'Profile and settings' }).click()
const exitItem = page.getByRole('menuitem', { name: /^Exit Yoru/ })

// With another device playing (stood in for), the row says so and exiting asks, naming what stops.
await page.route('**/api/app/activity', (route) =>
  route.fulfill({ json: { watching: [{ device: 'Chrome on Android', title: 'Frieren', episode: 5 }] } }))
await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
await openMenu()
check(await until(() => exitItem.isVisible()), 'the profile menu offers Exit Yoru')
check(await until(() => exitItem.getByText('Chrome on Android is watching').isVisible()), 'saying what runs, before a click')
await exitItem.click()
const dialog = page.getByRole('alertdialog', { name: 'Exit Yoru?' })
check(await until(() => dialog.isVisible()), 'something would stop, so it asks, in the middle of the screen')
check(await dialog.getByText('Chrome on Android is watching Frieren, episode 5').isVisible(), 'naming what stops')
check(await dialog.getByRole('button', { name: 'Cancel' }).evaluate((b) => b === document.activeElement),
  'with Cancel, the safe answer, focused')
await page.screenshot({ path: `${process.env.SHOTS ?? '.'}/exit-dialog.png` })
await dialog.getByRole('button', { name: 'Cancel' }).click()
check(await until(async () => !(await dialog.isVisible())) && (await alive()), 'Cancel leaves Yoru running')

// Nothing running: no question, Yoru just exits.
await page.unroute('**/api/app/activity')
await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
await openMenu()
check(await until(() => exitItem.isVisible()), 'the menu again')
check(!(await exitItem.getByText(/watching|downloading/).isVisible()), 'with nothing running, nothing said')
await exitItem.click()
check(!(await dialog.isVisible()), 'and no question asked')
check(await until(() => page.getByText('Yoru is exiting…').isVisible()), 'it says Yoru is exiting')
check(await until(async () => !(await alive()), 20_000), 'and Yoru stops')

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
