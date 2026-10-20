// Casting: the button, what the TV is sent (signed HLS link, fMP4, subtitles, the time reached), the remote
// panel, and stopping back to the video here. Google's SDK is stood in for.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const SHOTS = process.env.SHOTS ?? '.'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 8000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) if (await fn().catch(() => false)) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')

// The SDK's surface as the player uses it: one device, a session that records what it is asked to load.
const fakeSdk = `(() => {
  const listeners = {}
  const on = (t, f) => (listeners[t] ??= []).push(f)
  const off = (t, f) => (listeners[t] = (listeners[t] ?? []).filter((g) => g !== f))
  const fire = (t) => (listeners[t] ?? []).forEach((f) => f())
  let state = 'NOT_CONNECTED'
  const player = { currentTime: 0, duration: 0, isPaused: true, isConnected: false }
  const session = {
    getCastDevice: () => ({ friendlyName: 'Living Room TV' }),
    loadMedia: async (req) => {
      window.__castLoaded = JSON.parse(JSON.stringify(req))
      Object.assign(player, { currentTime: req.currentTime, duration: 180, isPaused: false, isConnected: true,
        mediaInfo: { contentId: req.media.contentId } })
      fire('anyChanged')
    },
  }
  // Another sender's stream on the TV.
  window.__castOther = () => {
    Object.assign(player, { currentTime: 999, duration: 1400, mediaInfo: { contentId: 'http://elsewhere/other.m3u8' } })
    fire('anyChanged')
  }
  const ctx = {
    setOptions: (o) => (window.__castOptions = o),
    getCastState: () => state,
    getCurrentSession: () => (state === 'CONNECTED' ? session : null),
    requestSession: async () => { state = 'CONNECTED'; fire('caststatechanged') },
    endCurrentSession: () => { state = 'NOT_CONNECTED'; player.isConnected = false; fire('caststatechanged') },
    addEventListener: on, removeEventListener: off,
  }
  class Obj { constructor(...a) { this.args = a } }
  window.cast = { framework: {
    CastContext: { getInstance: () => ctx },
    CastContextEventType: { CAST_STATE_CHANGED: 'caststatechanged' },
    RemotePlayer: function () { return player },
    RemotePlayerController: function () {
      return { playOrPause: () => { player.isPaused = !player.isPaused; fire('anyChanged') },
        seek: () => fire('anyChanged'), addEventListener: on, removeEventListener: off }
    },
    RemotePlayerEventType: { ANY_CHANGE: 'anyChanged' },
  } }
  class MediaInfo { constructor(contentId, contentType) { this.contentId = contentId; this.contentType = contentType } }
  class LoadRequest { constructor(media) { this.media = media } }
  class Track { constructor(trackId, type) { this.trackId = trackId; this.type = type } }
  window.chrome = window.chrome || {}
  window.chrome.cast = {
    AutoJoinPolicy: { ORIGIN_SCOPED: 'origin_scoped' },
    Image: class { constructor(url) { this.url = url } },
    media: {
      DEFAULT_MEDIA_RECEIVER_APP_ID: 'CC1AD845', MediaInfo, GenericMediaMetadata: Obj, LoadRequest, Track,
      TrackType: { TEXT: 'TEXT' }, TextTrackType: { SUBTITLES: 'SUBTITLES' }, StreamType: { BUFFERED: 'BUFFERED' },
      HlsSegmentFormat: { FMP4: 'fmp4' }, HlsVideoSegmentFormat: { FMP4: 'fmp4' },
    },
  }
  setTimeout(() => window.__onGCastApiAvailable?.(true), 50)
})()`

const browser = await chromium.launch({ channel: 'chromium' })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
await page.route('https://www.gstatic.com/cv/js/sender/**', (route) => route.fulfill({ contentType: 'text/javascript', body: fakeSdk }))

try {
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  const video = page.locator('video').first()
  await video.waitFor({ timeout: 30_000 })
  await page.waitForFunction(() => (document.querySelector('video')?.readyState ?? 0) >= 2, null, { timeout: 30_000 }).catch(() => {})
  await video.evaluate((v) => (v.currentTime = 42))
  await video.hover()

  const castButton = page.getByRole('button', { name: 'Cast to a TV' })
  check(await until(() => castButton.isVisible(), 15_000), 'the player offers a Cast button where a device is about')

  // This instance answers on this machine only: the TV could not reach it, and Yoru says so.
  await castButton.click()
  check(await until(() => page.getByText(/Turn on access from other devices/).isVisible()), 'casting with Yoru unreachable from the TV says what to turn on')

  // Reachable: the link the server signs is what the TV is given.
  await page.route('**/api/stream/*/cast', (route) => route.fulfill({ json: {
    url: `${BASE}/c/signed/playlist.m3u8`, subtitles: [{ index: 2, language: 'eng', url: `${BASE}/c/signed/sub-2.vtt` }],
  } }))
  await video.hover()
  await castButton.click()
  const loaded = await until(() => page.evaluate(() => !!window.__castLoaded), 8000) && await page.evaluate(() => window.__castLoaded)
  const media = loaded?.media ?? {}
  check(media.contentId === `${BASE}/c/signed/playlist.m3u8` && media.contentType === 'application/x-mpegURL',
    "the TV is sent Yoru's signed playlist", JSON.stringify(media).slice(0, 200))
  check(media.hlsSegmentFormat === 'fmp4' && media.hlsVideoSegmentFormat === 'fmp4', 'told the segments are fragmented MP4')
  check(Math.abs((loaded?.currentTime ?? 0) - 42) < 2, 'it starts where the episode had got to', String(loaded?.currentTime))
  check(media.tracks?.length === 1 && media.tracks[0].trackContentType === 'text/vtt', 'with the subtitles as WebVTT')

  const panel = page.getByText('Playing on')
  check(await until(() => panel.isVisible()), 'the player becomes the TV’s remote')
  check(await page.getByText('Living Room TV').isVisible(), 'naming the TV')
  check(await video.evaluate((v) => v.paused), 'the video here waits')
  await page.screenshot({ path: `${SHOTS}/cast-panel.png` })
  await page.getByRole('button', { name: 'Pause on the TV' }).click()
  check(await until(() => page.getByRole('button', { name: 'Play on the TV' }).isVisible()), 'pause reaches the TV')

  await page.getByRole('button', { name: 'Stop casting', exact: true }).click()
  check(await until(async () => !(await panel.isVisible())), 'Stop casting hands back to this device')

  // The session stays, but something else now plays on the TV: not this episode's remote, nor its progress.
  await video.hover()
  await castButton.click()
  check(await until(() => panel.isVisible()), 'cast again')
  await page.evaluate(() => window.__castOther())
  check(await until(async () => !(await panel.isVisible())), 'another stream on the TV hands this player back')
  check(await video.evaluate((v) => v.currentTime < 900), "the video here doesn't take the other stream's time",
    String(await video.evaluate((v) => v.currentTime)))
  await page.evaluate(() => window.cast.framework.CastContext.getInstance().endCurrentSession(true))

  // Drawn in, with the setting on: the track shown here goes into the picture, and no text track beside it.
  await fetch(`${BASE}/api/prefs`, {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ key: 'cast.burn_subtitles', value: 'true' }),
  })
  let asked = ''
  await page.unroute('**/api/stream/*/cast')
  await page.route('**/api/stream/*/cast*', (route) => {
    asked = route.request().url()
    return route.fulfill({ json: { url: `${BASE}/c/burned/playlist.m3u8`, subtitles: [], burned: true } })
  })
  await page.evaluate(() => (window.__castLoaded = undefined))
  await page.reload({ waitUntil: 'domcontentloaded' })
  await video.waitFor({ timeout: 30_000 })
  await page.waitForFunction(() => (document.querySelector('video')?.readyState ?? 0) >= 2, null, { timeout: 30_000 }).catch(() => {})
  await video.hover()
  await page.getByRole('button', { name: 'Cast to a TV' }).click()
  const burned = await until(() => page.evaluate(() => !!window.__castLoaded), 8000) && await page.evaluate(() => window.__castLoaded)
  check(/[?&]burn=\d+&delay=/.test(asked), 'with the setting on, the shown track is asked for drawn in, with its delay', asked)
  check(burned?.media?.contentId === `${BASE}/c/burned/playlist.m3u8` && (burned?.media?.tracks ?? []).length === 0,
    'and the TV gets the picture with them, no text track beside', JSON.stringify(burned?.media ?? {}).slice(0, 160))
  await page.getByRole('button', { name: 'Stop casting', exact: true }).click().catch(() => {})
  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures === 0 ? '\nall checks passed' : `\n${failures} check(s) failed`)
process.exit(failures === 0 ? 0 : 1)
