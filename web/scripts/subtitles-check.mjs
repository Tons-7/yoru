// Subtitles beyond the file's: the look, a file from this device, Jimaku's (site stood in for), PGS pictures.
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
const files = await api('/api/local/files')
const pictures = (files.items ?? []).find((f) => String(f.path ?? '').includes('Yoru Test Show - 04'))
check(!!pictures, 'the PGS episode is in the library')
if (pictures) await api('/api/local/assign', { id: pictures.id, animeId: ANIME, episode: 4 })
await api('/api/prefs', { key: 'playback.autoplay', value: 'false' })

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 860 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const video = page.locator('video').first()

// Pixels of the picture as shown, counted in the page: a paused frame differs only by what is drawn over it.
const count = async (test) => {
  // Reaching a row of a tall menu scrolls the page, taking the picture under the sticky header.
  await page.evaluate(() => window.scrollTo(0, 0))
  const box = await video.boundingBox()
  const png = await page.screenshot({ clip: box })
  return page.evaluate(
    async ({ b64, test }) => {
      const img = new Image()
      img.src = `data:image/png;base64,${b64}`
      await img.decode()
      const c = document.createElement('canvas')
      c.width = img.naturalWidth
      c.height = img.naturalHeight
      const ctx = c.getContext('2d')
      ctx.drawImage(img, 0, 0)
      const px = ctx.getImageData(0, 0, c.width, c.height).data
      const is = new Function('r', 'g', 'b', `return ${test}`)
      let n = 0
      for (let i = 0; i < px.length; i += 4) if (is(px[i], px[i + 1], px[i + 2])) n++
      return n
    },
    { b64: png.toString('base64'), test },
  )
}
// Near white: both renderers keep video range, where white is 235.
const WHITE = 'r > 225 && g > 225 && b > 225'
const YELLOW = 'r > 220 && g > 220 && b < 60'
const open = async (episode, at) => {
  await page.goto(`${BASE}/watch/${ANIME}/${episode}`, { waitUntil: 'domcontentloaded' })
  await video.waitFor({ timeout: 30_000 })
  await page.waitForFunction(() => (document.querySelector('video')?.readyState ?? 0) >= 2, null, { timeout: 60_000 })
  await video.evaluate((v, t) => {
    v.pause()
    v.currentTime = t
  }, at)
  await sleep(2500)
}
const subtitlesButton = page.getByRole('region', { name: 'Video player' }).getByRole('button', { name: 'Audio & subtitles' })
const menu = page.getByRole('dialog', { name: 'Audio & subtitles' })
const trackGroup = menu.getByRole('radiogroup', { name: 'Subtitle track' })
const openMenu = async () => {
  if (await menu.isVisible()) return
  await page.mouse.move(640, 400)
  await subtitlesButton.click()
  await menu.waitFor({ timeout: 3000 })
}
const closeMenu = async () => {
  if (!(await menu.isVisible())) return
  await menu.getByRole('button', { name: 'Done', exact: true }).click()
  await menu.waitFor({ state: 'hidden', timeout: 3000 })
}

// ---------------------------------------------------------------- a file from this device, and the look
// Paused, the bar's shade covers the bottom: drawn lines are measured mid-picture (\an5), white on colour bars.
const TEXT = WHITE
const middle = (text) =>
  `[Script Info]\nScriptType: v4.00+\nPlayResX: 1280\nPlayResY: 720\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\nStyle: Default,Arial,60,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,2,0,5,10,10,10,1\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\nDialogue: 0,0:00:00.00,0:03:00.00,Default,,0,0,0,,${text}\n`
const turnOff = async () => {
  await openMenu()
  await trackGroup.getByRole('radio', { name: 'Off', exact: true }).click()
  await closeMenu()
  await sleep(1000)
  return count(TEXT)
}
await page.goto(`${BASE}/settings?tab=Language`, { waitUntil: 'domcontentloaded' })
await page.getByRole('combobox', { name: 'Subtitle look' }).click()
await page.getByRole('option', { name: 'Yellow, outlined', exact: true }).click()
check(await until(async () => (await api('/api/prefs')).effective?.['subtitle.look'] === 'yellow'), 'Settings sets the subtitle look')
await open(1, 13)
const off = await turnOff()
const offYellow = await count(YELLOW)
await openMenu()
await page.locator('input[type=file][accept*=".srt"]').setInputFiles({
  name: 'mine.srt',
  mimeType: 'text/plain',
  buffer: Buffer.from('1\n00:00:00,000 --> 00:03:00,000\n<i>A line</i> from this device, long enough to see\n'),
})
// A file with no line Yoru can read is refused, so the .srt taken is one converted.
check(await until(() => trackGroup.getByRole('radio', { name: 'mine.srt', checked: true }).isVisible()), 'a loaded .srt becomes the track')
await openMenu()
await page.locator('input[type=file][accept*=".srt"]').setInputFiles({
  name: 'mine.ass',
  mimeType: 'text/plain',
  buffer: Buffer.from(middle('MY OWN FILE MY OWN FILE')),
})
await closeMenu()
// The outline hides some of the pattern's own yellow bar, so the look is judged against the line as made.
check(await until(async () => Math.abs((await count(YELLOW)) - offYellow) > 500), 'a loaded .ass is drawn')
await sleep(1500)
const yellow = await count(YELLOW)
await page.screenshot({ path: `${SHOTS}/subtitles-own-file.png` })
await openMenu()
await menu.getByRole('button', { name: /^Size, look and timing/ }).click()
await menu.getByRole('radiogroup', { name: 'Subtitle look' }).getByRole('radio', { name: 'As made' }).click()
check(await until(async () => (await api('/api/prefs')).effective?.['subtitle.look'] === ''), "the player's panel sets the look too")
await closeMenu()
check(await until(async () => (await count(TEXT)) > off + 1000 && (await count(YELLOW)) < yellow - 600),
  'yellow as set, white again as made', `${off} → ${await count(TEXT)} white; ${yellow} → ${await count(YELLOW)} yellow pixels`)

// ---------------------------------------------------------------- Jimaku, its site stood in for
const jimakuASS = middle('JIMAKU LINE JIMAKU LINE')
await page.route('**/api/jimaku', (r) => r.fulfill({ json: { set: true } }))
await page.route('**/api/stream/*/jimaku', (r) => {
  const id = new URL(r.request().url()).pathname.split('/')[3]
  return r.fulfill({ json: { files: [{ index: 0, name: 'Show - 01.ja.ass', url: `/api/stream/${id}/jimaku/0` }] } })
})
await page.route('**/api/stream/*/jimaku/0**', (r) => r.fulfill({ body: jimakuASS, headers: { 'X-Yoru-Complete': 'true' } }))
await open(1, 13)
const none = await turnOff()
await openMenu()
check(await until(() => menu.getByText('Jimaku · Japanese').isVisible()), 'with a key, the menu lists Jimaku')
await menu.getByRole('radiogroup', { name: 'Jimaku subtitles' }).getByRole('radio', { name: 'Show - 01.ja.ass' }).click()
const picked = () => trackGroup.getByRole('radio', { checked: true }).innerText({ timeout: 1000 }).catch(() => '')
check(await until(() => trackGroup.getByRole('radio', { name: 'Jimaku · Show - 01.ja.ass', checked: true }).isVisible()),
  'a Jimaku file becomes the track', await picked())
await closeMenu()
check(await until(async () => (await count(TEXT)) > none + 1000), 'and is drawn', `${none} → ${await count(TEXT)}`)
await page.unroute('**/api/jimaku')

// ---------------------------------------------------------------- PGS pictures
if (pictures) {
  await open(4, 30)
  await openMenu()
  check(await until(async () => /^(?!Off\b)\S/.test(await picked())), 'a PGS track is picked like any other', await picked())
  await closeMenu()
  check(await until(async () => (await count(WHITE)) > 6000, 20_000), 'its picture, the white box, is drawn over the video', String(await count(WHITE)))
  await page.screenshot({ path: `${SHOTS}/subtitles-pgs.png` })
  await openMenu()
  await trackGroup.getByRole('radio', { name: 'Off', exact: true }).click()
  await closeMenu()
  check(await until(async () => (await count(WHITE)) < 3000), 'and off, it goes', String(await count(WHITE)))
}

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
