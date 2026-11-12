import { spawn, spawnSync } from 'node:child_process'
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { delimiter, join } from 'node:path'
import { chromium } from 'playwright'

const REPO = process.env.YORU_REPO ?? join(process.cwd(), '..')
const PORT = 4396
const BASE = `http://127.0.0.1:${PORT}`
const SHOTS = process.env.SHOTS

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const api = async (path, init) => {
  const res = await fetch(BASE + path, { ...init, headers: { 'content-type': 'application/json' } })
  return { ok: res.ok, status: res.status, body: await res.json().catch(() => null) }
}
const pref = async (key) => (await api('/api/prefs')).body?.effective?.[key]
const seen = (locator, ms = 8000) => locator.first().waitFor({ timeout: ms }).then(() => true, () => false)

const scratch = join(tmpdir(), 'yoru-setup-check')
rmSync(scratch, { recursive: true, force: true, maxRetries: 60, retryDelay: 500 })
const root = join(scratch, 'app')
const data = join(scratch, 'data')
for (const d of [root, data]) mkdirSync(d, { recursive: true })
if (SHOTS) mkdirSync(SHOTS, { recursive: true })

const binDir = process.env.YORU_BIN_DIR ?? join(tmpdir(), 'yoru-e2e-bin')
mkdirSync(binDir, { recursive: true })
const exe = join(binDir, process.platform === 'win32' ? 'yoru.exe' : 'yoru')
const built = spawnSync('go', ['build', '-o', exe, './cmd/yoru'], { cwd: REPO, stdio: 'inherit', env: { ...process.env, CGO_ENABLED: '0' } })
check(built.status === 0, 'built Yoru')

const pathKey = Object.keys(process.env).find((k) => k.toLowerCase() === 'path') ?? 'PATH'
const tools = ['ffmpeg', 'ffprobe', 'mpv', 'vlc', 'cloudflared'].map((n) => (process.platform === 'win32' ? `${n}.exe` : n))
const bare = (process.env[pathKey] ?? '').split(delimiter).filter((d) => !tools.some((t) => existsSync(join(d, t)))).join(delimiter)

writeFileSync(join(root, 'config.toml'), `addr = "127.0.0.1:${PORT}"\n\n[torrent]\nlisten_port = 4336\nupnp = false\n`)
const server = spawn(exe, [], {
  cwd: root,
  env: { ...process.env, [pathKey]: bare, YORU_ROOT: root, LOCALAPPDATA: data, APPDATA: data, YORU_NO_WINDOW: '1', YORU_UPDATE_API: 'http://127.0.0.1:9' },
})
let log = ''
server.stdout.on('data', (d) => (log += d))
server.stderr.on('data', (d) => (log += d))

let up = false
for (let i = 0; i < 120 && !up; i++) {
  up = await api('/api/setup').then((r) => r.ok, () => false)
  if (!up) await sleep(500)
}
check(up, 'Yoru answering')

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const installs = []
await page.route('**/api/setup/install/*', (route) => {
  installs.push(route.request().url().split('/').pop())
  return route.fulfill({ status: 202, contentType: 'application/json', body: '{}' })
})
await page.route('**/api/app/startup', (route) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ on: route.request().method() === 'POST', tray: true }) }),
)
const shot = async (name) => SHOTS && page.screenshot({ path: join(SHOTS, `setup-${name}.png`) })
const heading = (name) => page.getByRole('heading', { level: 1, name })
const rail = page.getByRole('navigation', { name: 'Setup steps' }).first()

await page.goto(BASE, { waitUntil: 'domcontentloaded' })
check(await page.waitForURL(/\/setup$/, { timeout: 15000 }).then(() => true, () => false), 'a fresh Yoru opens the setup guide')
check(await seen(heading('Welcome to Yoru')), 'with a welcome')
check((await rail.getByRole('link').count()) === 6, 'six steps on the left', String(await rail.getByRole('link').count()))
check((await page.getByRole('navigation', { name: 'Main' }).count()) === 0, "without the app's own bar")
await shot('0-welcome')

await page.getByRole('link', { name: 'Start setup' }).click()
check(await seen(heading('Get the essentials')), 'Start setup opens the essentials')
check((await rail.locator('[aria-current="step"]').innerText()).includes('Essentials'), 'the left marks the step you are on')
const boxes = page.getByRole('checkbox')
check((await boxes.count()) === 5, 'five parts to pick from', String(await boxes.count()))
check((await page.getByRole('checkbox', { checked: true }).count()) === 3, 'three picked to begin with')
check((await page.locator('input[type=checkbox]').evaluateAll((all) => all.filter((b) => getComputedStyle(b.nextElementSibling).borderRadius.startsWith('9999') || parseFloat(getComputedStyle(b.nextElementSibling).borderRadius) >= 12).length)) === 5, 'the ticks are round')
const forMpv = page.getByRole('checkbox', { name: /Anime4K for the desktop player/ })
check(await forMpv.isDisabled(), 'the mpv extra waits for the desktop player')
await page.getByRole('checkbox', { name: /Desktop player/ }).check({ force: true })
check(await forMpv.isEnabled(), 'and unlocks once it is picked')
await page.getByRole('checkbox', { name: /Desktop player/ }).uncheck({ force: true })
await shot('1-essentials')
await page.getByRole('button', { name: 'Download and continue' }).click()
check(await seen(heading('Where episodes come from')), 'continuing opens the sources')
check(installs.sort().join(',') === 'cloudflared,ffmpeg,flyhigh', 'and starts the three picked downloads', installs.join(','))

check(await seen(page.getByText('Nothing can play without one')), 'the sources step says what is missing')
await page.getByLabel('Site address').fill('https://example.invalid')
await page.getByRole('button', { name: 'Add', exact: true }).click()
check(await seen(page.getByText('https://example.invalid')), 'a site is added in place')
for (let i = 0; i < 20 && !(await rail.innerText()).includes('1 site'); i++) await sleep(250)
check((await rail.innerText()).includes('1 site'), 'and the left shows it', (await rail.innerText()).replace(/\n/g, ' | '))
await shot('2-sources')
await page.getByRole('button', { name: 'Continue' }).click()

check(await seen(heading('Your list and your name')), 'then the account')
check(await seen(page.getByRole('region', { name: 'AniList' })) && (await seen(page.getByRole('region', { name: 'MyAnimeList' }))), 'both list sites are offered')
check(await page.getByRole('radio', { name: /My AniList name/ }).isDisabled(), "a site's name waits for that site to be connected")
check(await seen(page.getByText('Connect AniList first')), 'and says so')
const own = page.getByLabel('My own name')
await own.fill('Tester')
await own.blur()
let profile = {}
for (let i = 0; i < 20 && profile.shown !== 'Tester'; i++) {
  await sleep(250)
  profile = (await api('/api/profile')).body ?? {}
}
check(profile.shown === 'Tester' && profile.nameFrom === 'own', 'a typed name is the one shown', JSON.stringify(profile))
check(await page.getByRole('radio', { name: 'A name of my own' }).isChecked(), 'and its choice is the picked one')
await shot('3-account')
await page.getByRole('button', { name: 'Continue' }).click()

check(await seen(heading('How you like to watch')), 'then watching')
await page.getByRole('radio', { name: /English dub/ }).check()
await page.getByRole('switch', { name: 'Skip openings and endings by itself' }).click()
let dub = ''
for (let i = 0; i < 20 && dub !== 'dub'; i++) {
  await sleep(250)
  dub = await pref('audio.prefer')
}
check(dub === 'dub', 'a sound choice is saved at once', dub)
check((await pref('playback.autoskip_op')) === 'auto' && (await pref('playback.autoskip_ed')) === 'auto', 'skipping covers openings and endings')
await shot('4-watching')
await page.getByRole('button', { name: 'Continue' }).click()

check(await seen(heading('Where things go')), 'then storage')
await page.getByRole('radio', { name: '40 GB' }).click()
let budget = ''
for (let i = 0; i < 20 && budget !== String(40 * 1024 ** 3); i++) {
  await sleep(250)
  budget = await pref('cache.budget_bytes')
}
check(budget === String(40 * 1024 ** 3), 'the space choice is saved', budget)
await shot('5-storage')
await page.getByRole('button', { name: 'Continue' }).click()

check(await seen(heading('Yoru on this computer')), 'then this computer')
check((await page.getByRole('switch').count()) === 3 && (await page.getByRole('radiogroup', { name: 'When I close the window' }).count()) === 1, 'switches and a two-way choice, no drop-downs',
  `${await page.getByRole('switch').count()} switches, ${await page.locator('select').count()} selects`)
const autoSwitch = page.getByRole('switch', { name: 'Update automatically' })
check(await autoSwitch.isDisabled() && (await page.getByText("A copy built from source doesn't update.").isVisible()),
  'in a copy built from source the update switch is shown greyed out, with why')
await page.getByRole('radio', { name: 'Keep in the tray' }).click()
await shot('6-computer')
await page.getByRole('button', { name: 'Finish' }).click()

check(await seen(heading("You're all set")), 'Finish ends on the summary')
const tiles = page.getByRole('list', { name: 'What you chose' }).getByRole('link')
check((await tiles.count()) === 6, 'six tiles, one per step', String(await tiles.count()))
check((await tiles.nth(0).innerText()).includes("The player engine isn't installed") && (await tiles.nth(0).innerText()).includes('Fix'), 'a tile says what still needs a look', (await tiles.nth(0).innerText()).replace(/\n/g, ' | '))
check((await tiles.nth(1).innerText()).includes('1 site'), 'and what was chosen', (await tiles.nth(1).innerText()).replace(/\n/g, ' | '))
check((await tiles.nth(3).innerText()).includes('English dub'), 'in your own choices', (await tiles.nth(3).innerText()).replace(/\n/g, ' | '))
const phone = page.getByRole('region', { name: 'Other devices' })
const reachable = (await api('/api/access')).body?.reachable
check(await seen(phone), 'the phone card is there')
check(((await phone.getByAltText('Pairing QR code').count()) === 1) === !!reachable, 'its code shows only while other devices are allowed', `reachable=${reachable}`)
await shot('7-done')

await rail.getByRole('link', { name: /Watching/ }).click()
check(await seen(heading('How you like to watch')), 'any step opens from the left')
check((await rail.getByRole('img', { name: 'Done' }).count()) === 3, 'steps that are done carry a tick, skipped ones keep their number', String(await rail.getByRole('img', { name: 'Done' }).count()))
const top = async (title) => {
  await rail.getByRole('link', { name: new RegExp(title) }).click()
  await page.waitForTimeout(250)
  return Math.round((await page.getByRole('heading', { level: 1 }).boundingBox()).y)
}
const tops = [await top('Essentials'), await top('Sources'), await top('Account'), await top('Watching'), await top('Storage'), await top('This computer')]
check(new Set(tops).size === 1, 'every step puts its title at the same height', tops.join(','))

for (const [width, height] of [[2000, 1250], [2560, 1440]]) {
  await page.setViewportSize({ width, height })
  for (const step of ['welcome', 'storage']) {
    await page.goto(`${BASE}/setup${step === 'welcome' ? '' : `?step=${step}`}`, { waitUntil: 'domcontentloaded' })
    await page.getByRole('heading', { level: 1 }).waitFor({ timeout: 10000 })
    await page.waitForTimeout(400)
    const gaps = await page.evaluate(() => {
      const title = document.querySelector('h1').getBoundingClientRect()
      const box = document.querySelector('h1').closest('.max-w-\\[1000px\\]')
      const [column, pane] = [box.getBoundingClientRect(), box.parentElement.getBoundingClientRect()]
      return { left: Math.round(column.left - pane.left), right: Math.round(pane.right - column.right), title: Math.round(title.left) }
    })
    check(Math.abs(gaps.left - gaps.right) <= 2, `at ${width} px wide the ${step} screen sits in the middle`, JSON.stringify(gaps))
  }
  if (width === 2000) await shot('9-wide-welcome')
}
await page.setViewportSize({ width: 820, height: 900 })
await page.waitForTimeout(300)
const navs = page.locator('nav[aria-label="Setup steps"]')
check(!(await navs.nth(0).isVisible()) && (await navs.nth(1).isVisible()), 'a narrow window moves the steps to the top')
check(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'with nothing spilling sideways')
await shot('8-narrow')
await page.setViewportSize({ width: 1440, height: 900 })

await page.goto(`${BASE}/setup?step=done`, { waitUntil: 'domcontentloaded' })
await page.getByRole('link', { name: 'Start watching' }).click()
await page.waitForTimeout(1500)
check(new URL(page.url()).pathname === '/', 'Start watching opens Yoru and stays there', page.url())

await page.goto(`${BASE}/settings?tab=About`, { waitUntil: 'domcontentloaded' })
const again = page.getByRole('link', { name: 'Open', exact: true })
check(await seen(again, 15000), 'Settings › About offers the setup guide again')
await again.click()
check(await seen(heading('Welcome to Yoru')), 'and it opens on the welcome')
check(errors.length === 0, 'no page errors', errors.join(' | '))

await browser.close()
if (failures) console.log(log.slice(-2000))
if (process.platform === 'win32') spawnSync('taskkill', ['/PID', String(server.pid), '/T', '/F'], { stdio: 'ignore' })
else server.kill()
await sleep(500)
if (!process.env.KEEP) rmSync(scratch, { recursive: true, force: true, maxRetries: 20, retryDelay: 500 })

console.log(failures ? `\n${failures} check(s) failed` : '\nall checks passed')
process.exit(failures ? 1 : 0)
