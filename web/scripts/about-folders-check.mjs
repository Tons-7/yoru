import { mkdirSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const seen = (locator, ms = 15000) => locator.first().waitFor({ timeout: ms }).then(() => true, () => false)
const until = async (fn, ms = 10000) => {
  for (const end = Date.now() + ms; Date.now() < end; await new Promise((r) => setTimeout(r, 250))) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
  }
  return false
}

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
const shown = []
await page.route('**/api/setup/show', (route) => {
  shown.push(JSON.parse(route.request().postData() ?? '{}').which)
  return route.fulfill({ status: 204 })
})
let picked = { cancelled: true }
const picks = []
await page.route('**/api/setup/pick', (route) => {
  if (route.request().method() === 'GET') return route.fulfill({ json: { available: true } })
  picks.push(JSON.parse(route.request().postData() ?? '{}'))
  return route.fulfill({ json: picked })
})
await page.goto(`${BASE}/settings?tab=Storage`, { waitUntil: 'domcontentloaded' })
const heading = page.getByRole('heading', { name: 'Where things go' })
check(await seen(heading), 'Storage lists where things go')
const setup = await (await fetch(`${BASE}/api/setup`)).json()
const card = page.locator('section', { has: heading })
const row = (title) => card.locator('li', { has: page.getByText(title, { exact: true }) })

for (const [title, path] of [
  ['History and settings', setup.dataDir],
  ['Episodes', setup.cacheDir],
  ['Programs', setup.binDir],
]) {
  check(await row(title).getByText(path, { exact: true }).isVisible().catch(() => false), `${title} shows its folder`, path)
}
check(!(await card.getByText('data_dir', { exact: true }).isVisible().catch(() => false)), 'no setting names beside the titles')
check(/ of /.test(await row('Episodes').innerText()), 'Episodes says how much of its room is used', (await row('Episodes').innerText()).replace(/\n/g, ' | '))

await row('Episodes').getByRole('button', { name: 'Open' }).click()
await row('Programs').getByRole('button', { name: 'Open' }).click()
await card.getByRole('button', { name: 'Show config.toml' }).click()
await page.waitForTimeout(500)
check(shown.join(',') === 'cache,bin,config', 'each Open asks Yoru for its own folder', shown.join(','))

const ownLink = row('Your own files').getByRole('link')
check((await ownLink.getAttribute('href')) === '/settings?tab=Library', 'Your own files leads to where folders are added', await ownLink.getAttribute('href'))

const move = row('History and settings').getByRole('button', { name: 'Move' })
await move.click()
const folding = row('History and settings').getByRole('button', { name: 'Cancel' })
check(await seen(folding, 3000) && (await folding.getAttribute('aria-expanded')) === 'true', 'Move opens the folder field under its row')
await page.getByRole('button', { name: "use Yoru's own folder" }).click()
check((await page.getByRole('textbox', { name: 'New folder for history and settings' }).inputValue()) === 'data', "the shortcut fills in Yoru's own folder")
picked = { path: 'D:\\Yoru\\data' }
await row('History and settings').getByRole('button', { name: 'Browse…' }).click()
check(await until(async () => (await page.getByRole('textbox', { name: 'New folder for history and settings' }).inputValue()) === 'D:\\Yoru\\data'),
  'Browse… fills in the folder picked in the window')
check(picks.at(-1)?.start === setup.dataDir, 'the window opens on the folder in use', JSON.stringify(picks.at(-1)))
picked = { cancelled: true }
await heading.scrollIntoViewIfNeeded()
await page.screenshot({ path: `${SHOTS}/about-folders.png` })
await row('History and settings').getByRole('button', { name: 'Cancel' }).click()
check(!(await page.getByRole('textbox', { name: 'New folder for history and settings' }).isVisible().catch(() => false)), 'Cancel folds it away again')

await page.goto(`${BASE}/settings?tab=Library`, { waitUntil: 'domcontentloaded' })
const own = page.locator('section', { has: page.getByRole('heading', { name: 'Your own files' }) })
check(await seen(own), 'the Library tab has Your own files')
check(await own.getByText(/Add a folder of anime you already have/).isVisible(), 'with no folder yet it says what folders are for')
const shelf = join(tmpdir(), 'yoru-e2e-own-files')
mkdirSync(shelf, { recursive: true })
const field = own.getByRole('textbox', { name: 'Folder to add' })
check(await own.getByRole('button', { name: 'Add folder' }).isDisabled(), 'Add folder waits for a path')
await field.fill(shelf)
await field.press('Enter')
const shelfRow = own.locator('li', { has: page.getByText(shelf, { exact: true }) })
check(await seen(shelfRow), 'Enter adds the folder as its own row')
check(await seen(own.getByText('Files', { exact: true })), 'and the totals show under the folders')
await shelfRow.getByRole('button', { name: 'Open' }).click()
await page.waitForTimeout(400)
check(shown.at(-1) === 'library', 'Open asks Yoru for that folder', shown.join(','))
await shelfRow.getByRole('button', { name: `Remove ${shelf}` }).click()
const undo = page.getByRole('button', { name: 'Undo' })
check(await seen(undo, 5000), 'Remove offers Undo')
await undo.click()
check(await seen(shelfRow), 'Undo puts the folder back')
await page.screenshot({ path: `${SHOTS}/own-files.png` })
await shelfRow.getByRole('button', { name: `Remove ${shelf}` }).click()
check(await until(async () => (await shelfRow.count()) === 0), 'removed again, it is gone')
const browse = own.getByRole('button', { name: 'Browse…' })
await browse.click()
await page.waitForTimeout(800)
check((await shelfRow.count()) === 0 && (await page.getByRole('alert').count()) === 0, 'closing the window without a pick changes nothing')
picked = { path: shelf }
await browse.click()
check(await seen(shelfRow), 'a folder picked in the window is added at once')
await shelfRow.getByRole('button', { name: `Remove ${shelf}` }).click()
check(await until(async () => (await shelfRow.count()) === 0), 'and removed for the next run')
rmSync(shelf, { recursive: true, force: true })
await page.goto(`${BASE}/settings?tab=Storage`, { waitUntil: 'domcontentloaded' })
await heading.waitFor({ timeout: 15000 })

await page.setViewportSize({ width: 390, height: 844 })
await page.waitForTimeout(400)
check(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'on a phone nothing spills sideways')
await heading.scrollIntoViewIfNeeded()
await page.screenshot({ path: `${SHOTS}/about-folders-phone.png` })

await browser.close()
console.log(failures === 0 ? '\nall checks passed' : `\n${failures} check(s) failed`)
process.exit(failures === 0 ? 0 : 1)
