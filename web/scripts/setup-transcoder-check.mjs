import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

const setup = await (await fetch(`${BASE}/api/setup`)).json()
const row = setup.components.find((c) => c.name === 'ffmpeg')
check(row && !row.present, 'ffmpeg without ffprobe does not count as installed', JSON.stringify({ present: row?.present }))
check(/ffmpeg is on this system but ffprobe is not/.test(row?.problem ?? ''), 'the server says which half is missing', row?.problem)

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
await page.goto(`${BASE}/setup?step=essentials`, { waitUntil: 'load', timeout: 60000 })

const item = page.locator('li').filter({ hasText: 'Player engine' }).first()
await item.waitFor({ timeout: 30000 })
const text = await item.innerText()
check(/\bNeeded\b/.test(text), 'the player engine is marked as needed', text.replace(/\n/g, ' | '))
check(text.includes('Prepares every episode for the built-in player'), 'and says what it is for')
check(text.includes('ffprobe is not; Yoru needs both'), 'the guide shows which half is missing')
check(await page.getByRole('checkbox', { name: /Player engine/ }).isChecked(), 'the pair is picked to be fetched')
check(await page.getByRole('button', { name: 'Download and continue' }).isVisible(), 'and the step offers to download it')
await page.screenshot({ path: `${SHOTS}/setup-transcoder.png` })

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `${failures} failed` : 'all passed')
process.exit(failures ? 1 : 0)
