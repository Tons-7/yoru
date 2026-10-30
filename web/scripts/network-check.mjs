// Per device: the mobile-data card, quality steps, Settings rows, softened flashing, the encoder, the New tab.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 15_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(200)) if (await fn()) return true
  return false
}
const api = async (path, body) =>
  (await fetch(`${BASE}${path}`, body ? { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) } : {}))
    .json()
    .catch(() => ({}))

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')
await api('/api/prefs', { key: 'playback.autoplay', value: 'false' })

const browser = await chromium.launch()
const errors = []

// A phone whose browser says it's on mobile data (or Wi-Fi), as Chrome on Android does.
async function phone(type, stored = {}) {
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  await context.addInitScript(
    ({ type, stored }) => {
      const connection = new EventTarget()
      connection.type = type
      Object.defineProperty(navigator, 'connection', { get: () => connection })
      for (const [k, v] of Object.entries(stored)) localStorage.setItem(k, v)
    },
    { type, stored },
  )
  const page = await context.newPage()
  page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
  const opens = []
  page.on('request', (r) => r.url().includes('/api/stream/open') && opens.push(new URL(r.url()).searchParams.get('quality') ?? ''))
  const plans = []
  page.on('response', async (r) => r.url().includes('/api/stream/open') && plans.push((await r.json().catch(() => ({}))).plan ?? {}))
  return { context, page, opens, plans }
}
const plays = (page) =>
  until(() =>
    page.evaluate(async () => {
      const v = document.querySelector('video')
      if (!v || v.readyState < 2) return false
      if (v.paused) await v.play().catch(() => {})
      const from = v.currentTime
      await new Promise((r) => setTimeout(r, 1200))
      return v.currentTime > from + 0.5
    }),
  )

// ---------------------------------------------------------------- Ask first, on mobile data
{
  const { context, page, opens, plans } = await phone('cellular')
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  const card = page.getByRole('dialog', { name: "You're on mobile data" })
  check(await until(() => card.isVisible()), 'on mobile data, the episode asks first')
  check(/This episode is about .+ Data saver plays it at 480p, about/.test((await card.innerText().catch(() => '')).replace(/\n/g, ' ')),
    'saying its size and data saver’s', (await card.innerText().catch(() => '')).replace(/\n/g, ' '))
  check(opens.length === 0, 'and streams nothing until answered', `${opens.length} opens`)
  await page.screenshot({ path: `${SHOTS}/network-ask.png` })
  await card.getByRole('button', { name: 'Play with data saver' }).click()
  check(await until(() => opens.includes('480')), 'data saver opens it at 480p', opens.join(','))
  check(await until(() => plans.some((p) => p.maxRate > 0 && p.height === 480)), 'capped by the server', JSON.stringify(plans.at(-1)))
  check(await plays(page), 'and it plays')
  // Opened again the same sitting: not asked again.
  await page.reload({ waitUntil: 'domcontentloaded' })
  check(await until(() => opens.filter((q) => q === '480').length >= 2), 'the sitting keeps the answer, unasked', opens.join(','))
  check(!(await card.isVisible().catch(() => false)), 'with no card')
  await context.close()
}

// ---------------------------------------------------------------- remembered on this device
{
  const { context, page, opens } = await phone('cellular')
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  const card = page.getByRole('dialog', { name: "You're on mobile data" })
  await card.waitFor({ timeout: 15_000 })
  await card.getByRole('checkbox', { name: 'Remember on this device' }).check()
  await card.getByRole('button', { name: 'Play', exact: true }).click()
  check(await until(() => opens.length > 0 && opens.at(-1) === ''), 'Play opens it at full quality', opens.join(','))
  check((await page.evaluate(() => localStorage.getItem('yoru.mobileData'))) === 'full', 'and remembers it on this device')
  await context.close()
}

// ---------------------------------------------------------------- Wi-Fi only
{
  const { context, page, opens } = await phone('cellular', { 'yoru.mobileData': 'wifi' })
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  const card = page.getByRole('dialog', { name: "You're on mobile data" })
  check(await until(async () => /Wi-Fi only/.test(await card.innerText().catch(() => ''))), 'Wi-Fi only stops it on mobile data, saying so')
  await card.getByRole('button', { name: 'Play once anyway' }).click()
  check(await until(() => opens.length === 1 && opens[0] === ''), 'with a way to play once anyway', opens.join(','))
  await context.close()
}

// ---------------------------------------------------------------- on Wi-Fi; the player's quality steps
{
  const { context, page, opens, plans } = await phone('wifi')
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  check(await until(() => opens.length === 1) && opens[0] === '', 'on Wi-Fi it just plays, as it is', opens.join(','))
  check(await plays(page), 'and it plays')
  await page.evaluate(() => (document.querySelector('video').currentTime = 60))
  await sleep(6000)
  await page.mouse.move(640, 400)
  await page.getByRole('button', { name: /^Player settings/ }).click()
  await page.getByRole('menuitem', { name: /^Quality/ }).click()
  await page.getByRole('menuitemradio', { name: '720p' }).click()
  check(await until(() => opens.includes('720')), "the player's Quality menu reopens it at 720p", opens.join(','))
  check(await until(() => plans.some((p) => p.maxRate === 3000000)), 'capped at 3 Mb/s', JSON.stringify(plans.at(-1)))
  const at = await until(() => page.evaluate(() => (document.querySelector('video')?.currentTime ?? 0) > 50), 20_000)
  check(at, 'where the picture was, not from the start', String(await page.evaluate(() => document.querySelector('video')?.currentTime)))
  check((await page.evaluate(() => localStorage.getItem('yoru.quality'))) === '720', 'kept for this device')
  await page.keyboard.press('i')
  const info = await page.locator('[aria-label="Playback info"]').innerText().catch(() => '')
  check(/capped at 480p|capped at 720p/.test(info), 'Playback info says the picture is capped', info.replace(/\n/g, ' | '))
  await page.screenshot({ path: `${SHOTS}/network-quality.png` })

  // Settings, Devices: the same two choices, this device's.
  await page.goto(`${BASE}/settings?tab=Devices`, { waitUntil: 'domcontentloaded' })
  const quality = page.getByRole('combobox', { name: 'Streaming quality' })
  check(await until(async () => (await quality.innerText().catch(() => '')) === '720p'), 'Settings shows this device’s quality')
  await quality.click()
  await page.getByRole('option', { name: 'Auto', exact: true }).click()
  await page.getByRole('combobox', { name: 'On mobile data' }).click()
  await page.getByRole('option', { name: 'Data saver', exact: true }).click()
  check((await page.evaluate(() => [localStorage.getItem('yoru.quality'), localStorage.getItem('yoru.mobileData')].join())) === ',saver',
    'and sets both')
  await context.close()
}

// ---------------------------------------------------------------- softened flashing, the encoder, the New tab
{
  const { context, page, plans } = await phone('wifi')
  await page.goto(`${BASE}/settings?tab=Sound`, { waitUntil: 'domcontentloaded' })
  const soften = page.getByRole('switch', { name: 'Soften flashing' })
  await soften.click()
  check(await until(async () => (await api('/api/prefs')).effective?.['playback.soften_flashing'] === 'true'), 'Soften flashing turns on')
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  check(await until(() => plans.some((p) => p.soften && !p.videoCopy)), 'and episodes are converted with it', JSON.stringify(plans.at(-1)))
  check(await plays(page), 'and play')
  await api('/api/prefs', { key: 'playback.soften_flashing', value: 'false' })

  await page.goto(`${BASE}/settings?tab=About`, { waitUntil: 'domcontentloaded' })
  const converting = page.locator('[data-row]', { hasText: 'Converting' })
  check(await until(() => converting.isVisible()), 'About names what converts', (await converting.innerText().catch(() => '')).replace(/\n/g, ' '))

  // As after an update from an older version.
  await page.route('**/api/update/since', (r) => r.fulfill({ json: { current: '2026.10.30', updatedFrom: '2026.10.20' } }))
  await api('/api/prefs', { key: 'settings.new_seen', value: '' })
  await page.goto(`${BASE}/settings`, { waitUntil: 'domcontentloaded' })
  const tab = page.getByRole('tab', { name: /New/ })
  check(await until(() => tab.isVisible()), 'after an update, Settings has a New tab')
  check(await tab.locator('[aria-label="not seen yet"]').isVisible().catch(() => false), 'marked until seen')
  await page.getByRole('button', { name: 'Profile and settings' }).click()
  check(await page.getByRole('menuitem', { name: /Settings\s*New/ }).isVisible().catch(() => false), 'and in the profile menu')
  await page.keyboard.press('Escape')
  await tab.click()
  await page.getByText('Subtitle look').first().waitFor({ timeout: 10_000 }).catch(() => {})
  const body = await page.locator('body').innerText().catch(() => '')
  check(['Subtitle look', 'Soften flashing', 'Streaming quality'].every((l) => body.includes(l)), 'it lists the new settings')
  check(/Download all/.test(body) && /Before: every episode not on disk · Now: the unwatched ones/.test(body), 'and what changed, before and now')
  check(!body.includes('Voice boost'), 'and nothing older')
  await page.getByRole('switch', { name: 'Soften flashing' }).click()
  check(await until(async () => (await api('/api/prefs')).effective?.['playback.soften_flashing'] === 'true'), 'its switches work right there')
  await api('/api/prefs', { key: 'playback.soften_flashing', value: 'false' })
  await page.screenshot({ path: `${SHOTS}/network-new-tab.png`, fullPage: true })
  await page.getByRole('button', { name: 'Seen them' }).click()
  check(await until(async () => !(await tab.locator('[aria-label="not seen yet"]').isVisible().catch(() => false))), 'Seen them clears the mark')
  await context.close()
}

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
