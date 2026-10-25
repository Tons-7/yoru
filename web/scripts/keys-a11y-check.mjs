// Keys and keyboard paths: Playback info, speed, frames, C, 0-9, held L, menus, skip link, focus, rows.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 8000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) if (await fn()) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, permissions: ['clipboard-read', 'clipboard-write'] })
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
const video = page.locator('video').first()
await video.waitFor({ timeout: 30_000 })
await page.waitForFunction(() => (document.querySelector('video')?.readyState ?? 0) >= 2, null, { timeout: 30_000 }).catch(() => {})
// Played, not just loaded: an episode's start waits for its subtitles to draw.
await page.waitForFunction(() => {
  const v = document.querySelector('video')
  return !!v && !v.paused && v.currentTime > 0.2
}, null, { timeout: 30_000 }).catch(() => {})
await page.locator('body').click({ position: { x: 5, y: 400 } })
const state = () => video.evaluate((v) => ({ paused: v.paused, time: v.currentTime, rate: v.playbackRate, duration: v.duration }))
// What a screen reader hears: the player's live regions.
const heard = () => page.evaluate(() => [...document.querySelectorAll('[role=status]')].map((p) => p.textContent ?? '').join(' | '))

// ---------------------------------------------------------------- Playback info
await page.keyboard.press('i')
const info = page.getByRole('region', { name: 'Playback info' })
check(await info.waitFor({ timeout: 3000 }).then(() => true, () => false), 'I opens Playback info')
await until(async () => /Buffered/.test(await info.innerText()))
const infoText = (await info.innerText()).replace(/\n/g, ' | ')
check(/Method/.test(infoText) && /Source[ |]+H\.264/.test(infoText) && /Buffered[ |]+\d+ s ahead/.test(infoText) && /Subtitles/.test(infoText),
  'it says how the episode plays', infoText)
await page.screenshot({ path: `${SHOTS}/playback-info.png` })
await info.getByRole('button', { name: 'Copy details' }).click()
const copied = await page.evaluate(() => navigator.clipboard.readText()).catch((e) => `unreadable: ${e}`)
check(/^Method: /m.test(copied) && /^Source: /m.test(copied), 'Copy details puts the rows on the clipboard', copied.replace(/\n/g, ' | '))
await page.keyboard.press('i')
check(await until(async () => (await info.count()) === 0), 'I closes it again')

// ---------------------------------------------------------------- speed, frames, subtitles, tenths
await page.keyboard.press('>')
check((await state()).rate === 1.25 && /Speed 1\.25×/.test(await heard()), '> speeds up, and says so', `${(await state()).rate} ${await heard()}`)
await page.keyboard.press('<')
check((await state()).rate === 1, '< slows back down')

if (!(await state()).paused) await page.keyboard.press('k')
await sleep(300)
const before = (await state()).time
await page.keyboard.press('.')
await sleep(300)
const stepped = (await state()).time - before
check(Math.abs(stepped - 1 / 23.976) < 0.02, '. steps one frame while paused', `${stepped.toFixed(3)} s`)
await page.keyboard.press(',')
await sleep(300)
check(Math.abs((await state()).time - before) < 0.02, ', steps back one')

await page.keyboard.press('c')
await sleep(300)
const first = await heard()
await page.keyboard.press('c')
await sleep(300)
const second = await heard()
check(/Subtitles( off|: )/.test(first) && /Subtitles( off|: )/.test(second) && first !== second, 'C goes round the subtitles and off', `${first} → ${second}`)

await page.keyboard.press('5')
await sleep(500)
const half = await state()
check(Math.abs(half.time - half.duration / 2) < 2, '5 jumps to half way', `${half.time.toFixed(1)} of ${half.duration.toFixed(1)}`)

// Held: the first press seeks, the repeats play at double speed until it's let go.
await page.keyboard.down('l')
await page.keyboard.down('l')
await sleep(200)
check((await state()).rate === 2 && /2× while held/.test(await heard()), 'L held plays at 2×')
await page.keyboard.up('l')
await sleep(200)
check((await state()).rate === 1, 'and let go, back to normal')

// ---------------------------------------------------------------- the episode rows
const row = page.getByRole('link', { name: /^Episode 1\b.*playing/ })
check((await row.count()) > 0, "the playing episode's row says so to a screen reader", await page.getByRole('link', { name: /^Episode 1\b/ }).first().getAttribute('aria-label'))

// ---------------------------------------------------------------- the player menus by keyboard
const gear = page.getByRole('button', { name: /^Player settings/ })
await page.mouse.move(700, 400)
await gear.focus()
await page.keyboard.press('Enter')
const focusedRole = () => page.evaluate(() => `${document.activeElement?.getAttribute('role')}:${document.activeElement?.textContent?.trim().slice(0, 30)}`)
check(await until(async () => /^menuitem/.test(await focusedRole())), 'Enter on the settings button opens it with the first item focused', await focusedRole())
const top = await focusedRole()
await page.keyboard.press('ArrowDown')
const next = await focusedRole()
check(/^menuitem/.test(next) && next !== top, 'ArrowDown walks to the next item', `${top} → ${next}`)
await page.keyboard.press('End')
await page.keyboard.press('Enter')
await sleep(300)
await page.keyboard.press('Escape')
await sleep(200)
check(await page.evaluate(() => document.activeElement?.getAttribute('aria-haspopup') === 'menu' && !!document.activeElement?.getAttribute('aria-label')?.startsWith('Player settings')),
  'Escape closes it and focus is back on its button', await focusedRole())

// ---------------------------------------------------------------- Settings' row
await page.goto(`${BASE}/settings?tab=Playback`, { waitUntil: 'domcontentloaded' })
await page.getByRole('button', { name: 'Change keys' }).click()
check(await page.getByRole('dialog', { name: 'Keyboard shortcuts' }).waitFor({ timeout: 3000 }).then(() => true, () => false), "Settings' Change keys opens the shortcuts list")
await page.keyboard.press('Escape')

// ---------------------------------------------------------------- the skip link and a new page's focus
await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
await sleep(500)
await page.keyboard.press('Tab')
const skip = page.getByRole('link', { name: 'Skip to content' })
check(await skip.evaluate((a) => document.activeElement === a && a.getBoundingClientRect().width > 20), 'Tab first reaches Skip to content, shown')
await page.keyboard.press('Enter')
check(await page.evaluate(() => document.activeElement?.id === 'main'), 'which moves focus to the page')
await page.getByRole('link', { name: 'Schedule', exact: true }).first().click()
check(await until(() => page.evaluate(() => ['H1', 'MAIN'].includes(document.activeElement?.tagName ?? ''))), 'a new page takes focus at its heading',
  await page.evaluate(() => document.activeElement?.tagName))
check(await until(async () => (await heard()).trim().length > 0), 'and its name is said', await heard())

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
