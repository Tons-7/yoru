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
// window.__castNoTV before it loads: no device about until __castFound().
const fakeSdk = `(() => {
  const listeners = {}
  const on = (t, f) => (listeners[t] ??= []).push(f)
  const off = (t, f) => (listeners[t] = (listeners[t] ?? []).filter((g) => g !== f))
  const fire = (t) => (listeners[t] ?? []).forEach((f) => f())
  let state = window.__castNoTV ? 'NO_DEVICES_AVAILABLE' : 'NOT_CONNECTED'
  window.__castFound = () => { state = 'NOT_CONNECTED'; fire('caststatechanged') }
  window.__castLost = () => { state = 'NO_DEVICES_AVAILABLE'; fire('caststatechanged') }
  const player = { currentTime: 0, duration: 0, isPaused: true, isConnected: false }
  const session = {
    getCastDevice: () => ({ friendlyName: 'Living Room TV' }),
    loadMedia: async (req) => {
      window.__castLoaded = JSON.parse(JSON.stringify(req))
      Object.assign(player, { currentTime: req.currentTime, duration: 180, isPaused: false, isConnected: true,
        mediaInfo: { contentId: req.media.contentId } })
      fire('anyChanged')
    },
    // A whole queue: the TV plays the first, and __castNextItem() moves it on as its autoplay would.
    getSessionObj: () => ({
      queueLoad: (req, done) => {
        window.__castQueued = JSON.parse(JSON.stringify(req))
        const first = req.items[req.startIndex ?? 0]
        Object.assign(player, { currentTime: first.startTime, duration: 180, isPaused: false, isConnected: true,
          mediaInfo: { contentId: first.media.contentId } })
        let at = req.startIndex ?? 0
        window.__castNextItem = () => {
          at++
          Object.assign(player, { currentTime: 0, mediaInfo: { contentId: req.items[at].media.contentId } })
          fire('anyChanged')
        }
        fire('anyChanged')
        done()
      },
    }),
  }
  // The TV's time and length change on their own, as when the next item's arrive before its media info.
  window.__castTVAt = (currentTime, duration) => {
    Object.assign(player, { currentTime, duration: duration ?? player.duration })
    fire('anyChanged')
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
    // Chrome's own list: it searches, and a TV picked connects; window.__castCancel closes it without one.
    requestSession: async () => {
      if (window.__castCancel) {
        window.__castClosed = true
        throw new Error('cancel')
      }
      state = 'CONNECTED'
      fire('caststatechanged')
    },
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
  class QueueItem { constructor(media) { this.media = media } }
  class QueueLoadRequest { constructor(items) { this.items = items } }
  window.chrome = window.chrome || {}
  window.chrome.cast = {
    AutoJoinPolicy: { ORIGIN_SCOPED: 'origin_scoped' },
    Image: class { constructor(url) { this.url = url } },
    media: {
      DEFAULT_MEDIA_RECEIVER_APP_ID: 'CC1AD845', MediaInfo, GenericMediaMetadata: Obj, LoadRequest, Track, TextTrackStyle: Obj,
      QueueItem, QueueLoadRequest,
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

  // This instance answers on this machine only, out of a TV's reach: the button says what casting needs.
  await castButton.click()
  const needs = page.getByRole('dialog', { name: 'Cast to a TV' })
  check(await until(() => needs.isVisible()), 'with Yoru unreachable from the TV, Cast lists what casting needs')
  const need = (label) => needs.locator('li', { has: page.getByText(label, { exact: true }) })
  check(await need('Access from other devices').getByLabel('Missing').isVisible(), 'naming access from other devices as missing')
  check(await need('Access from other devices').getByRole('button', { name: 'Turn on' }).isVisible(), 'with Turn on right there')
  check(await need('A browser that can cast').getByLabel('Done').isVisible() && await need('A secure address').getByLabel('Done').isVisible(),
    'and what is already met as done')
  await page.keyboard.press('Escape')
  check(await until(async () => !(await needs.isVisible())), 'Escape closes it')

  // Reachable: the link the server signs is what the TV is given.
  await page.route('**/api/access', (route) => route.fulfill({ json: { reachable: true, urls: [], canSwitch: true, host: true } }))
  await page.route('**/api/stream/*/cast*', (route) => route.fulfill({ json: {
    url: `${BASE}/c/signed/playlist.m3u8`, subtitles: [{ index: 2, language: 'eng', url: `${BASE}/c/signed/sub-2.vtt` }],
  } }))
  await page.reload({ waitUntil: 'domcontentloaded' })
  await video.waitFor({ timeout: 30_000 })
  await page.waitForFunction(() => (document.querySelector('video')?.readyState ?? 0) >= 2, null, { timeout: 30_000 }).catch(() => {})
  await video.evaluate((v) => (v.currentTime = 42))
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

  // With Auto next on, the TV is given what plays after too, and goes on by itself; this page follows it.
  await page.unroute('**/api/stream/*/cast*')
  const queued = (n) => ({
    key: `${ANIME}-${n}`, url: `${BASE}/c/next${n}/playlist.m3u8`, title: 'Show', subtitle: `Episode ${n}`,
    subtitles: [{ index: 0, language: 'eng', url: `${BASE}/c/next${n}/sub-auto.vtt` }],
  })
  let castAsked = ''
  await page.route('**/api/stream/*/cast*', (route) => {
    castAsked = route.request().url()
    return route.fulfill({ json: {
      url: `${BASE}/c/signed/playlist.m3u8`, subtitles: [{ index: 2, language: 'eng', url: `${BASE}/c/signed/sub-2.vtt` }],
      queue: [queued(2), queued(3)],
    } })
  })
  await video.evaluate((v) => (v.currentTime = 42))
  await video.hover()
  await castButton.click()
  const queue = await until(() => page.evaluate(() => !!window.__castQueued), 8000) && await page.evaluate(() => window.__castQueued)
  const items = queue?.items ?? []
  check(/[?&]sub=eng/.test(castAsked), "the shown subtitles' language is asked for, for the queued episodes to follow", castAsked)
  check(items.length === 3 && items[1].media.contentId === `${BASE}/c/next2/playlist.m3u8`,
    'the TV is given this episode and the two after', JSON.stringify(items.map((i) => i.media.contentId)))
  check(Math.abs((items[0]?.startTime ?? 0) - 42) < 2 && items[1]?.startTime === 0, 'the first from where it got to, the next from the start')
  check(items.every((i) => i.autoplay === true && i.preloadTime > 0), 'each plays on its own, loaded a little before')
  check(items[1]?.media.tracks?.[0]?.trackContentId?.endsWith('/sub-auto.vtt') && items[1]?.activeTrackIds?.[0] === 1,
    "with subtitles in this episode's language shown", JSON.stringify(items[1]?.media.tracks ?? []))
  check(await until(() => panel.isVisible()), 'the player is the remote for it')
  const reports = []
  await page.route('**/api/progress', (route) => {
    reports.push(route.request().postDataJSON())
    return route.continue()
  })
  await page.evaluate(() => window.__castTVAt(150))
  await sleep(300)
  // The next one's time and length first, then which it is: the last report for this one keeps its own.
  await page.evaluate(() => window.__castTVAt(1, 1300))
  await sleep(300)
  await page.evaluate(() => window.__castNextItem())
  check(await until(async () => page.url().endsWith(`/watch/${ANIME}/2`), 10_000), 'the TV going on to episode 2 takes this page there',
    page.url())
  check(await page.evaluate(() => [...document.querySelectorAll('video')].every((v) => v.paused)), "and nothing plays here while the TV does")
  // Both of them: the TV's, and the page's on leaving the episode to follow it.
  const ones = reports.filter((r) => r.episode === 1)
  check(ones.length > 0 && ones.every((r) => Math.abs(r.position - 150) < 1 && Math.abs(r.duration - 180) < 1),
    "episode 1's last reports are where the TV left it, not the next one's place or where casting began", JSON.stringify(reports))
  await page.unroute('**/api/progress')
  await page.evaluate(() => window.cast.framework.CastContext.getInstance().endCurrentSession(true))
  await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  await video.waitFor({ timeout: 30_000 })
  await page.waitForFunction(() => (document.querySelector('video')?.readyState ?? 0) >= 2, null, { timeout: 30_000 }).catch(() => {})

  // Drawn in, with the setting on: the track shown here goes into the picture, and no text track beside it.
  await fetch(`${BASE}/api/prefs`, {
    method: 'POST', headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ key: 'cast.burn_subtitles', value: 'true' }),
  })
  let asked = ''
  await page.unroute('**/api/stream/*/cast*')
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
  await page.getByRole('button', { name: 'Stop casting', exact: true }).click()
  check(await until(async () => !(await panel.isVisible())), 'Stop casting hands the drawn-in cast back')

  // No TV about: the button stays, and says what it is looking for; one turning up makes it ready.
  await page.addInitScript(() => (window.__castNoTV = true))
  await page.reload({ waitUntil: 'domcontentloaded' })
  await video.waitFor({ timeout: 30_000 })
  await video.hover()
  check(await until(() => castButton.isVisible(), 15_000), 'with no TV found, the Cast button is still there')
  await castButton.click()
  check(await until(() => need('A TV on this Wi-Fi').getByText(/None found yet/).isVisible()), 'saying no TV is found yet, and what kind')
  await sleep(400)
  await page.screenshot({ path: `${SHOTS}/cast-needs.png` })
  await page.setViewportSize({ width: 390, height: 844 })
  await sleep(400)
  let box = await needs.boundingBox()
  check(!!box && box.y >= 0, 'turned to a phone with the list open, it stays on screen', JSON.stringify(box))
  // Opened on the phone itself, Look for TVs taller in it.
  await page.keyboard.press('Escape')
  await video.hover()
  await castButton.click()
  await sleep(400)
  await page.screenshot({ path: `${SHOTS}/cast-needs-phone.png` })
  box = await needs.boundingBox()
  check(!!box && box.x >= 0 && box.x + box.width <= 390 && box.y >= 0, 'on a phone the list fits the screen', JSON.stringify(box))
  // The bar's last button stays on screen beside Cast, with two audio tracks and subtitles in it too.
  for (const width of [390, 360]) {
    await page.setViewportSize({ width, height: 844 })
    let fs = null
    for (let i = 0; i < 15; i++) {
      await sleep(200)
      fs = await page.getByRole('region', { name: 'Video player' }).getByRole('button', { name: 'Full screen', exact: true }).boundingBox()
      if (fs && fs.x + fs.width <= width) break
    }
    check(!!fs && fs.x + fs.width <= width, `at ${width}px Full screen is still in the bar`, JSON.stringify(fs))
  }
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.evaluate(() => window.__castFound())
  check(await until(() => needs.getByRole('button', { name: 'Choose a TV' }).isVisible()), 'a TV turning up makes it ready to cast')

  // Chrome on a Mac searches only once something asks (a friend's TV appeared only after YouTube's button did):
  // Look for TVs opens Chrome's list. Closed without a pick, nothing goes to a TV.
  await page.evaluate(() => window.__castLost())
  asked = ''
  await page.evaluate(() => {
    window.__castLoaded = null
    window.__castClosed = false
    window.__castCancel = true
  })
  const look = () => need('A TV on this Wi-Fi').getByRole('button', { name: 'Look for TVs' })
  check(await until(() => look().isVisible()), 'with none found, the list offers to look for TVs')
  await look().click()
  // Counted from the list closing, not the click: anything sent after it shows within this.
  check(await until(() => page.evaluate(() => window.__castClosed), 5000), "Look for TVs opens Chrome's list")
  const sentAnyway = await until(async () => asked !== '', 1500)
  check(!sentAnyway && (await page.evaluate(() => !window.__castLoaded)), "closing Chrome's list without a TV sends nothing", asked)
  // A TV picked there gets the episode.
  await page.evaluate(() => (window.__castCancel = false))
  await castButton.click()
  await look().click()
  check(await until(() => page.evaluate(() => !!window.__castLoaded), 8000), 'a TV picked from that list gets the episode')
  await page.getByRole('button', { name: 'Stop casting', exact: true }).click()
  check(await until(async () => !(await panel.isVisible())), 'and Stop casting hands it back')

  // A browser with casting turned off (Brave's Media Router): the button says how to turn it on.
  await page.unroute('https://www.gstatic.com/cv/js/sender/**')
  await page.route('https://www.gstatic.com/cv/js/sender/**', (route) =>
    route.fulfill({ contentType: 'text/javascript', body: 'setTimeout(() => window.__onGCastApiAvailable?.(false), 50)' }))
  await page.addInitScript(() => Object.defineProperty(navigator, 'brave', { value: { isBrave: async () => true } }))
  await page.reload({ waitUntil: 'domcontentloaded' })
  await video.waitFor({ timeout: 30_000 })
  await video.hover()
  await castButton.click()
  check(await until(() => need('A browser that can cast').getByText(/turn on Media Router/).isVisible()), "in Brave with casting off, it names Brave's switch")
  check(await need('A TV on this Wi-Fi').getByLabel('Not known yet').isVisible(), 'and cannot say yet whether a TV is about')
  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures === 0 ? '\nall checks passed' : `\n${failures} check(s) failed`)
process.exit(failures === 0 ? 0 : 1)
