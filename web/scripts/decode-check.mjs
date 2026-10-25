// Copied where decoded, converted where not or only claimed, 5.1 to six speakers; episodes 5-6 by YORU_HEVC=1.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 20_000) => {
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
const files = await api('/api/local/files')
for (const episode of [5, 6]) {
  const file = (files.items ?? []).find((f) => String(f.path ?? '').includes(`Yoru Test Show - 0${episode}`))
  check(!!file, `episode ${episode} is in the library`)
  if (file) await api('/api/local/assign', { id: file.id, animeId: ANIME, episode })
}
await api('/api/prefs', { key: 'playback.autoplay', value: 'false' })

const browser = await chromium.launch()
const errors = []

// Opens an episode in a fresh page, recording every stream open (its caps and the server's answer).
async function watch(context, episode = 5) {
  const page = await context.newPage()
  page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
  const opens = []
  page.on('response', async (r) => {
    if (!r.url().includes('/api/stream/open')) return
    const caps = new URL(r.url()).searchParams.get('caps') ?? ''
    opens.push({ caps, body: await r.json().catch(() => ({})) })
  })
  await page.goto(`${BASE}/watch/${ANIME}/${episode}`, { waitUntil: 'domcontentloaded' })
  return { page, opens }
}

// Copied for a device that decodes it: it plays as sent, with no converted reopen.
async function copiedAndPlays(context, episode, token, what) {
  const { page, opens } = await watch(context, episode)
  await until(() => opens.length > 0)
  const first = opens[0] ?? { caps: '', body: {} }
  if (!first.caps.split(',').includes(token)) {
    console.log(`skip ${what}: this browser doesn't decode it (${first.caps})`)
    return page.close()
  }
  check(first.body.plan?.videoCopy === true && first.body.plan?.forDevice === true, `${what} is copied`, first.body.plan?.reason)
  check(await plays(page), `${what} plays as sent`)
  check(opens.length === 1, 'with no converted reopen', `${opens.length} opens`)
  await page.close()
}

// Playing: frames presented and the clock moving.
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

// ---------------------------------------------------------------- this browser, as it is
{
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  const { page, opens } = await watch(context)
  check(await until(() => opens.length > 0), 'the open says what this browser decodes', opens[0]?.caps)
  const first = opens[0] ?? { caps: '', body: {} }
  const decodesHevc = first.caps.split(',').includes('hevc-10')
  const plan = first.body.plan ?? {}
  check(/(^|,)ch\d+$/.test(first.caps), 'and how many speakers it has', first.caps)
  check(plan.videoCopy === decodesHevc && (first.body.id === `${ANIME}-5`) !== decodesHevc,
    decodesHevc ? 'it decodes HEVC 10-bit: copied, in a session of its own' : "it doesn't: converted, in the episode's session",
    `${first.body.id} · ${plan.reason}`)
  check(plan.audioCopy === false && plan.audioCodec === 'aac', 'AC-3 is converted to AAC', JSON.stringify(plan))
  check(await plays(page), 'and it plays')
  await page.screenshot({ path: `${SHOTS}/decode-as-is.png` })
  await context.close()
}

// ---------------------------------------------------------------- claiming HEVC 10-bit and six speakers
{
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  await context.addInitScript(() => {
    const claims = (type) => /hvc1\.2/.test(type ?? '')
    for (const name of ['MediaSource', 'ManagedMediaSource']) {
      const MS = window[name]
      if (!MS) continue
      const real = MS.isTypeSupported.bind(MS)
      MS.isTypeSupported = (type) => claims(type) || real(type)
    }
    const caps = navigator.mediaCapabilities
    const realInfo = caps.decodingInfo.bind(caps)
    caps.decodingInfo = async (c) =>
      claims(c.video?.contentType) ? { supported: true, smooth: true, powerEfficient: true } : realInfo(c)
    Object.defineProperty(AudioDestinationNode.prototype, 'maxChannelCount', { get: () => 6 })
  })
  const { page, opens } = await watch(context)
  check(await until(() => opens.length > 0), 'a device that says it decodes HEVC 10-bit', opens[0]?.caps)
  const first = opens[0]?.body ?? {}
  check(first.plan?.videoCopy === true && first.plan?.forDevice === true && String(first.id).startsWith(`${ANIME}-5-dev`),
    'gets it copied, in a session of its own', `${first.id} · ${first.plan?.reason}`)
  check(first.plan?.channels === 6, 'six speakers get the 5.1 track as 5.1', `channels ${first.plan?.channels}`)

  // This Chromium can't decode HEVC, whatever was claimed: the player reopens it converted.
  const fellBack = await until(() => opens.length > 1, 30_000)
  const second = opens[1]?.body ?? {}
  check(fellBack && second.plan?.videoCopy === false && !opens[1].caps.includes('hevc'),
    "when it can't, the player opens it converted instead", fellBack ? `${opens[1].caps} · ${second.plan?.reason}` : 'no reopen')
  check(await page.getByText("couldn't play the original").first().isVisible().catch(() => false), 'and says so')
  check(await plays(page), 'and it plays')
  await page.keyboard.press('i')
  const info = await page.locator('[aria-label="Playback info"]').innerText().catch(() => '')
  check(/AAC 6 ch/.test(info) && /Converting/.test(info), 'Playback info shows what is sent', info.replace(/\n/g, ' | '))
  await page.screenshot({ path: `${SHOTS}/decode-fallback.png` })
  await context.close()
}

// ---------------------------------------------------------------- Hi10P here, and both in installed Chrome
{
  const context = await browser.newContext({ viewport: { width: 1280, height: 860 } })
  await copiedAndPlays(context, 6, 'h264-10', 'Hi10P (10-bit H.264)')
  await context.close()
}
// Playwright's Chromium has no HEVC decoder; Chrome uses the GPU's, where it has one.
const chrome = await chromium.launch({ channel: 'chrome' }).catch(() => null)
if (!chrome) console.log('skip installed Chrome: not found')
else {
  const context = await chrome.newContext({ viewport: { width: 1280, height: 860 } })
  await copiedAndPlays(context, 5, 'hevc-10', 'in Chrome, HEVC 10-bit')
  await copiedAndPlays(context, 6, 'h264-10', 'in Chrome, Hi10P')
  await chrome.close()
}

// ---------------------------------------------------------------- mpv's picture settings
{
  const page = await browser.newPage({ viewport: { width: 1280, height: 860 } })
  await page.goto(`${BASE}/settings?tab=Sound`, { waitUntil: 'domcontentloaded' })
  const hw = page.getByRole('switch', { name: 'Hardware decoding' })
  const deband = page.getByRole('switch', { name: 'Smooth banding' })
  check((await hw.getAttribute('aria-checked').catch(() => null)) === 'true', "mpv's hardware decoding is on by default")
  check((await deband.getAttribute('aria-checked').catch(() => null)) === 'false', 'smooth banding is off')
  await deband.click()
  check(await until(async () => (await api('/api/prefs')).effective?.['playback.mpv_deband'] === 'true', 5000), 'and turns on')
  await page.close()
}

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
