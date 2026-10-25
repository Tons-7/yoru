// An episode that opens on dialogue shows its lines from the first frame, its fonts fast or slow, and a font
// arriving later never blanks them. Cue i is a block at x = 40 + 12i from 2i s, read against the video's clock;
// it ends at 2i + 1.09 s, as libass reads the generator's ".9" as hundredths.
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const LIB = process.env.YORU_LIB
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
// Episode 3's markers alone, then the same with 23 fonts (episode 7's file), so Next lands on the case reported.
const PLAIN = 8
const FONTS = 9

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const api = async (path, init) => {
  const res = await fetch(BASE + path, { ...init, headers: { 'content-type': 'application/json' } })
  const text = await res.text()
  try {
    return { ok: res.ok, body: JSON.parse(text) }
  } catch {
    return { ok: res.ok, body: text }
  }
}
const post = (path, body) => api(path, { method: 'POST', body: JSON.stringify(body) })

await post('/api/local/paths', { paths: [LIB] })
await post('/api/local/scan', {})
let files = []
for (let i = 0; i < 30 && files.length < 2; i++) {
  await sleep(1000)
  files = ((await api('/api/local/files')).body?.items ?? []).filter((f) => /Yoru Test Show - 0[37]/.test(String(f.path ?? '')))
}
check(files.length === 2, 'the marker episode and its copy with fonts are listed')
if (files.length < 2) process.exit(1)
for (const f of files) await post('/api/local/assign', { id: f.id, animeId: ANIME, episode: f.path.includes('- 07') ? FONTS : PLAIN })
await post('/api/prefs', { key: 'playback.autoplay', value: 'true' })

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
// What a miss is shown beside: renderers coming and going, the video's own events, the subtitle fetches.
await page.addInitScript(() => {
  window.__log = []
  const note = (what) => {
    const v = document.querySelector('video')
    window.__log.push(`${v ? v.currentTime.toFixed(2) : '-'} s ${what}`)
  }
  for (const name of ['loadedmetadata', 'resize', 'seeking', 'playing', 'waiting', 'emptied']) {
    document.addEventListener(name, (e) => e.target instanceof HTMLVideoElement && note(name), true)
  }
  const isRenderer = (n) => n instanceof HTMLCanvasElement && n.classList.contains('JASSUB')
  new MutationObserver((changes) => {
    for (const c of changes) {
      for (const n of c.addedNodes) if (isRenderer(n)) note('renderer up')
      for (const n of c.removedNodes) if (isRenderer(n)) note('renderer down')
    }
  }).observe(document, { childList: true, subtree: true })
})
page.on('request', (r) => {
  if (/\/(subtitle\/|fonts\b)/.test(r.url())) {
    page.evaluate((u) => window.__log?.push(`fetch ${u}`), r.url().replace(/^.*\/api\/stream\/[^/]+/, '')).catch(() => {})
  }
})

// The fonts come over one shared link of this many bytes a millisecond, as from a phone away from home.
let rate = Infinity
let linkFree = 0
let fontBytes = 0
// While true the words' own font is held back, so the first frames show the stand-in.
let holdWords = false
await page.route('**/font/**', async (route) => {
  while (holdWords && /comic/i.test(route.request().url())) await sleep(100)
  const res = await route.fetch()
  const body = await res.body()
  fontBytes += body.length
  linkFree = Math.max(Date.now(), linkFree) + body.length / rate
  await sleep(linkFree - Date.now())
  await route.fulfill({ response: res, body })
})

// Which cue the subtitle canvas shows, by the centroid of the marker band (y 290-350 of 720), and how wide the
// words at the top are (episode 9's line in Comic Sans MS): another font, another width.
const readSubtitle = () =>
  page.evaluate(() => {
    const v = document.querySelector('video')
    const canvas = document.querySelector('canvas.JASSUB')
    if (!v) return null
    const box = canvas?.getBoundingClientRect()
    const at = { t: v.currentTime, paused: v.paused, size: canvas ? `${canvas.width}x${canvas.height}@${Math.round(box.y)}` : '' }
    if (!canvas) return { ...at, blank: true, words: 0 }
    const probe = document.createElement('canvas')
    probe.width = 640
    probe.height = 360
    const ctx = probe.getContext('2d', { willReadFrequently: true })
    ctx.drawImage(canvas, 0, 0, probe.width, probe.height)
    const d = ctx.getImageData(0, 0, probe.width, probe.height).data
    const drawn = (x, y) => d[(y * probe.width + x) * 4 + 3] > 40
    let sum = 0
    let weighted = 0
    for (let y = 145; y < 175; y++) {
      for (let x = 0; x < probe.width; x++) {
        if (drawn(x, y)) {
          sum++
          weighted += x
        }
      }
    }
    let left = probe.width
    let right = -1
    for (let y = 10; y < 70; y++) {
      for (let x = 0; x < probe.width; x++) {
        if (drawn(x, y)) {
          left = Math.min(left, x)
          right = Math.max(right, x)
        }
      }
    }
    const words = right < 0 ? 0 : ((right - left + 1) / probe.width) * 1280
    if (sum === 0) return { ...at, blank: true, words }
    return { ...at, blank: false, words, cue: Math.round(((weighted / sum / probe.width) * 1280 - 45) / 12) }
  })

// Samples the first 6 s of the episode just asked for: when it started, and every line it missed.
async function opening(label, began, episode) {
  // Next leaves the last episode's clock up for a moment.
  for (const end = Date.now() + 15000; Date.now() < end; await sleep(40)) {
    const s = await readSubtitle().catch(() => null)
    if (page.url().endsWith(`/${episode}`) && s && s.t < 1) break
  }
  await page.evaluate(() => (window.__log = []))
  const samples = []
  for (const end = Date.now() + 25000; Date.now() < end; await sleep(40)) {
    const s = await readSubtitle().catch(() => null)
    if (s) samples.push({ ...s, wall: Date.now() })
    if (s && s.t > 6) break
  }
  const first = samples.find((s) => s.t > 0.05 && !s.paused)
  const held = first ? first.wall - began : Infinity
  // Inside each cue, clear of its edges: a frame or two either side is the clock, not the subtitles.
  const missed = samples.filter((s) => {
    const i = Math.floor(s.t / 2)
    const into = s.t - i * 2
    return !s.paused && s.t <= 6 && into >= 0.15 && into <= 0.95 && (s.blank || s.cue !== i)
  })
  const shown = missed.slice(0, 4).map((s) => `${s.t.toFixed(2)} s ${s.blank ? 'blank' : `cue ${s.cue}`}`)
  check(!!first, `${label}: it plays`)
  check(missed.length === 0, `${label}: every line is there from the first frame`, missed.length ? `${missed.length} samples: ${shown.join(', ')}` : '')
  if (missed.length) {
    const blanks = missed.filter((s) => s.blank).map((s) => s.t)
    console.log(`     blank ${Math.min(...blanks).toFixed(2)} to ${Math.max(...blanks).toFixed(2)} s; meanwhile:`)
    // The canvas as it changed: size, and where it sat.
    let last = ''
    for (const s of samples) {
      const now = `${s.size} ${s.blank ? 'blank' : `cue ${s.cue}`}`
      if (now !== last) console.log(`       ${s.t.toFixed(2)} s canvas ${now}`)
      last = now
    }
    for (const line of await page.evaluate(() => window.__log)) console.log(`       ${line}`)
  }
  return { held, at: first?.wall ?? Infinity, words: first?.words ?? 0 }
}

// No fonts to wait for: the start isn't held.
let began = Date.now()
await page.goto(`${BASE}/watch/${ANIME}/${PLAIN}`, { waitUntil: 'domcontentloaded' })
const plain = await opening('an episode without fonts', began, PLAIN)
console.log(`     started ${plain.held} ms after the page was asked for`)

// Next onto 11.9 MB of fonts at 4 MB/s: the start waits for them, then the first line is there.
rate = 4000
fontBytes = 0
await page.locator('body').click({ position: { x: 5, y: 400 } })
began = Date.now()
await page.keyboard.press('n')
const quick = await opening('Next onto an episode with 23 fonts', began, FONTS)
check(fontBytes > 5_000_000, 'its fonts were fetched', `${(fontBytes / 1e6).toFixed(1)} MB`)
check(quick.at >= linkFree - 250, 'its start waited for them', `played ${quick.held} ms in, fonts in at ${linkFree - began} ms`)

// The same at 1 MB/s, longer than the start waits: it plays on time with the lines drawn, the fonts landing after.
await page.goto('about:blank')
await post('/api/progress', { animeId: ANIME, episode: FONTS, position: 0, duration: 180, played: 0 })
rate = 1000
linkFree = 0
holdWords = true
began = Date.now()
await page.goto(`${BASE}/watch/${ANIME}/${FONTS}`, { waitUntil: 'domcontentloaded' })
const slow = await opening('fonts slower than the start waits', began, FONTS)
check(slow.held < 9000, 'the start waited its 5 s at most, not for every font', `${slow.held} ms`)
// Every font lands now; still drawn, and no page errors from adding them.
holdWords = false
await sleep(Math.max(0, linkFree - Date.now()) + 2500)
let after
for (let i = 0; i < 50; i++, await sleep(40)) {
  after = await readSubtitle()
  const into = after.t - Math.floor(after.t / 2) * 2
  if (into >= 0.15 && into <= 0.95) break
}
const due = Math.floor(after.t / 2)
check(!after.paused && !after.blank && after.cue === due, 'still drawn once the fonts are in', JSON.stringify(after))
// The words came first in the stand-in font, then in their own once it landed; the quick run had it from the start.
const near = (a, b) => Math.abs(a - b) <= 0.03 * b
check(slow.words > 0 && after.words > 0 && !near(after.words, slow.words), 'the episode\'s own font replaces the stand-in once it lands',
  `${Math.round(slow.words)} px wide, then ${Math.round(after.words)} px`)
check(near(quick.words, after.words), 'with its fonts in before the start, the first frame already uses them',
  `${Math.round(quick.words)} px, ${Math.round(after.words)} px in its own font`)

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
