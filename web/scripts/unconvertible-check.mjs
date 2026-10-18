// A release that can't be converted: the player says so and offers another, rather than buffering forever.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

// What the server answers once its session is broken: 422, with ffmpeg's words.
let asked = 0
await page.route('**/api/stream/*/*.mp4', (route) => {
  asked++
  return route.fulfill({
    status: 422,
    json: { error: 'the encoder failed on every try: [flac @ 0] invalid block size: 1', code: 'unconvertible' },
  })
})

await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
// At the first answer: hls.js alone would retry it for a minute.
const says = page.getByText(/couldn't convert this release/)
check(await says.waitFor({ timeout: 5000 }).then(() => true, () => false), 'the player says the release could not be converted')

const before = asked
await sleep(3000)
check(asked === before, 'and stops asking for it', `${asked - before} more requests`)
check((await page.getByRole('button', { name: 'Try again' }).count()) === 0, 'no Try again, which would fail the same way')
await page.screenshot({ path: `${SHOTS}/unconvertible.png` })

await page.getByRole('button', { name: 'Choose another release' }).click()
check(await page.getByRole('dialog', { name: 'Choose a release' }).waitFor({ timeout: 5000 }).then(() => true, () => false),
  'Choose another release opens the release picker')

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
