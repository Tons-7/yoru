// Alerts: a pushed alert's buttons, picture and count; Out and Delayed; Notify me; "not out yet"; the bell.
// AniList's and the sites' answers are fed to the page where the instance can't have them.
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 10_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
  }
  return false
}
const api = async (path) => (await fetch(`${BASE}${path}`)).json()

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

for (let i = 0; i < 60; i++) {
  try {
    if ((await fetch(`${BASE}/api/setup`)).ok) break
  } catch {}
  await sleep(1000)
}

// Full Chromium: the headless shell reports notifications as blocked whatever is granted.
const browser = await chromium.launch({ channel: 'chromium' })
const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
await context.grantPermissions(['notifications'], { origin: BASE })
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

// A release alert as Yoru sends it: the worker shows its picture and buttons and sets the icon's count.
await page.goto(`${BASE}/settings`, { waitUntil: 'domcontentloaded' })
await page.evaluate(async () => {
  await navigator.serviceWorker.register('/sw.js')
  await navigator.serviceWorker.ready
})
const cdp = await context.newCDPSession(page)
let registrationId
cdp.on('ServiceWorker.workerRegistrationUpdated', (e) => {
  for (const reg of e.registrations) if (!reg.isDeleted) registrationId = reg.registrationId
})
await cdp.send('ServiceWorker.enable')
await until(() => !!registrationId)
const push = (data) => cdp.send('ServiceWorker.deliverPushMessage', { origin: new URL(BASE).origin, registrationId, data: JSON.stringify(data) })
const shown = () =>
  page.evaluate(async () =>
    (await (await navigator.serviceWorker.getRegistration()).getNotifications()).map((n) => ({
      title: n.title, body: n.body, tag: n.tag, image: n.image, actions: n.actions.map((a) => a.title), data: n.data,
    })))
await push({
  web_push: 8030,
  notification: { title: 'Frieren', body: 'Episode 6 is available', tag: 'yoru-71', navigate: `${BASE}/watch/${ANIME}/6`, app_badge: '3' },
  yoru: { image: 'https://img.example/banner.jpg', actions: ['download', 'upnext'], animeId: ANIME, episode: 6 },
})
let alert
check(await until(async () => (alert = (await shown()).find((n) => n.tag === 'yoru-71'))), 'a release alert shows')
check(alert?.actions.join(',') === 'Download,Up next', 'with Download and Up next', JSON.stringify(alert?.actions))
check(alert?.image === 'https://img.example/banner.jpg', 'and the show\'s picture')

// Up next from the alert: done without opening Yoru, and said.
const [worker] = context.serviceWorkers()
await worker.evaluate(({ id }) => act('upnext', { animeId: id, episode: 6 }, 'yoru-71'), { id: ANIME })
check(await until(async () => (await api('/api/upnext')).items?.some((u) => u.animeId === ANIME && u.episode === 6)),
  "the alert's Up next lines the episode up")
check(await until(async () => (await shown()).some((n) => n.tag === 'yoru-71' && n.body === 'Episode 6 is in Up next')),
  'and says so in its place')
await fetch(`${BASE}/api/upnext/${ANIME}`, { method: 'DELETE' })

// A new season's alert offers Follow and Add to Planning.
await push({
  web_push: 8030,
  notification: { title: 'Frieren Season 2 is on AniList', body: 'Follows Frieren · from Jan 2027', tag: 'yoru-72', navigate: `${BASE}/anime/${ANIME}` },
  yoru: { actions: ['follow', 'plan'], animeId: ANIME },
})
check(await until(async () => (await shown()).find((n) => n.tag === 'yoru-72')?.actions.join(',') === 'Follow,Add to Planning'),
  'a new-season alert offers Follow and Add to Planning')

// The schedule: an episode out on the sites, and one AniList moved later.
const now = Math.floor(Date.now() / 1000)
const today = new Date().toISOString().slice(0, 10)
const item = (animeId, title, episode, airingAt, more) => ({
  animeId, title, romaji: title, episode, airingAt, onList: true, progress: episode - 1, behind: 0, watched: false,
  jstWeekday: new Date(airingAt * 1000).toLocaleDateString('en-US', { weekday: 'long' }), ...more,
})
const items = [
  item(1, 'Out Show', 5, now - 3600, { out: true }),
  item(3, 'Early Show', 2, now + 1800, { out: true }),
  item(2, 'Late Show', 8, now + 3 * 86400, { delayedFrom: now - 4 * 86400 }),
]
await page.route('**/api/schedule*', (route) => route.fulfill({ json: {
  items, count: items.length, start: now - 86400, end: now + 7 * 86400,
  days: [{ date: today, weekday: new Date().toLocaleDateString('en-US', { weekday: 'long' }), today: true, items }],
} }))
await page.goto(`${BASE}/schedule`, { waitUntil: 'domcontentloaded' })
const rowOf = (title) => page.locator('li', { hasText: title })
check(await until(() => page.getByRole('button', { name: /^Earlier today · 1 out/ }).isVisible()), 'what aired earlier today folds into one row')
check(await rowOf('Early Show').getByText('Ready to play', { exact: true }).first().isVisible(), 'the schedule marks an episode already out before its time')
check(await rowOf('Late Show').getByText(/^Delayed, was due/).first().isVisible(), 'and one AniList moved later')
check(await rowOf('Late Show').getByText(/was due/).isVisible(), 'saying when it was due')
await page.screenshot({ path: `${SHOTS}/notices-schedule.png` })

// The show page: a delay chip, and Notify me saying its state and why.
await page.route(`**/api/anime/${ANIME}`, async (route) => {
  const res = await route.fetch()
  const body = await res.json()
  return route.fulfill({ response: res, json: { ...body, delay: { episode: 9, wasAt: now - 86400, nowAt: null } } })
})
await page.goto(`${BASE}/anime/${ANIME}`, { waitUntil: 'domcontentloaded' })
const notify = page.getByRole('switch', { name: /Alert me about new episodes/ })
if (await until(() => notify.isVisible(), 30_000)) {
  check(await page.getByText('Ep 9 delayed · no new date yet').isVisible(), 'the show page says an episode is delayed, without a date')
  const was = (await notify.getAttribute('aria-checked')) === 'true'
  await notify.click()
  check(await until(async () => (await api(`/api/anime/${ANIME}`)).notify?.on === !was), 'Notify me turns alerts', was ? 'off' : 'on')
  check(await until(async () => (await notify.getAttribute('aria-checked')) === String(!was)), 'and the switch says so')
  await page.screenshot({ path: `${SHOTS}/notices-show.png` })
  await notify.click()
  await until(async () => (await api(`/api/anime/${ANIME}`)).notify?.on === was)
} else {
  check(false, 'the show page loads', 'AniList unreachable?')
}

// The player, before the episode is out: an alert then, or the download.
await page.route('**/api/play', (route) => route.fulfill({ status: 502, json: { error: 'episode 13 has not aired yet (airs in 2 days)', notOut: true } }))
await page.goto(`${BASE}/watch/${ANIME}/13`, { waitUntil: 'domcontentloaded' })
const tell = page.getByRole('button', { name: "Tell me when it's out" })
check(await until(() => tell.isVisible(), 20_000), "an episode not out yet offers to say when it's out")
await page.getByRole('button', { name: "Download it when it's out" }).click()
check(await until(() => page.getByText('Episode 13 downloads when it is out, and you get an alert.').isVisible()), 'or to download it then, and says so')
check(await until(async () => (await api(`/api/anime/${ANIME}`)).notify?.wanted === 13), 'the episode is marked wanted')
await page.screenshot({ path: `${SHOTS}/notices-not-out.png` })
await fetch(`${BASE}/api/anime/${ANIME}/want`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{"episode":0}' })

// The bell: a delay and a new season open the show, not an episode.
await page.route('**/api/notifications*', (route) => route.fulfill({ json: { unread: 2, items: [
  { id: 91, kind: 'delay', animeId: 2, episode: 8, title: 'Late Show · Episode 8 delayed', body: 'Now Sat 2 Nov, 01:30', createdAt: now, read: false },
  { id: 92, kind: 'season', animeId: 3, title: 'Next Show Season 2 is on AniList', body: 'Follows Next Show · from Jan 2027', createdAt: now, read: false },
] } }))
await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
await page.getByRole('button', { name: '2 unread notifications' }).click()
const delayRow = page.getByRole('link', { name: /Late Show · Episode 8 delayed/ })
check(await until(() => delayRow.isVisible()), 'the bell lists the delay')
check((await delayRow.getAttribute('href')) === '/anime/2', 'opening the show')
check((await page.getByRole('link', { name: /Next Show Season 2 is on AniList/ }).getAttribute('href')) === '/anime/3', 'and the new season opens its page')
await page.screenshot({ path: `${SHOTS}/notices-bell.png` })

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
