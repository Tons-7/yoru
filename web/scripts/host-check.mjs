// The host PC: This computer's rows, proxied devices, a full drive, Download all's confirm, credits, offline.
// Never presses "Start Yoru when I sign in": that writes the real startup list.
import { chromium } from 'playwright'
import { connect } from 'node:net'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
const ANIME = process.env.YORU_ANIME ?? '127230'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 10_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) if (await fn().catch(() => false)) return true
  return false
}
const prefs = async () => (await (await fetch(`${BASE}/api/prefs`)).json()).effective ?? {}

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

// A proxy on this machine: from loopback, but for someone else. Every way of saying so gets 401; the named one
// last, as Settings shows the last request a proxy passed on.
const bare = await new Promise((done) => {
  const { hostname, port } = new URL(BASE)
  const sock = connect(Number(port), hostname, () => sock.write(`GET /api/access HTTP/1.0\r\nHost: ${hostname}:${port}\r\n\r\n`))
  let text = ''
  sock.on('data', (b) => (text += b))
  sock.on('end', () => done(text.split(' ')[1]))
  sock.on('error', () => done('error'))
})
check(bare === '401', "nginx's bare proxy_pass (HTTP/1.0, no headers) needs the token", bare)
const viaProxy = { 'X-Forwarded-For': '203.0.113.4', 'X-Forwarded-Proto': 'https' }
const proxied = await fetch(`${BASE}/api/access`, { headers: { ...viaProxy, 'X-Forwarded-Host': 'evil.example' } })
check(proxied.status === 401, 'so does a proxy that names the client', `${proxied.status}`)
let own = await (await fetch(`${BASE}/api/access`)).json()
check(own.host === true && own.proxy?.client === '203.0.113.4' && !own.proxy?.site,
  'the host is still the host, learns who came through, and takes no address from a request without the token',
  JSON.stringify(own.proxy))
// The address goes into a link carrying the token, so only a paired device's request gives it.
await fetch(`${BASE}/api/health`, { headers: { ...viaProxy, 'X-Forwarded-Host': 'yoru.example.com', Authorization: `Bearer ${own.token}` } })
own = await (await fetch(`${BASE}/api/access`)).json()
check(own.proxy?.site === 'https://yoru.example.com', "and learns the proxy's address from a paired device", JSON.stringify(own.proxy))

const browser = await chromium.launch()
const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const section = (title) => page.locator('section', { has: page.getByRole('heading', { name: title, exact: true }) })
const row = (label) => page.locator('[data-row]', { has: page.getByText(label, { exact: true }) })

// This computer: sign-in shown as it stands, the two stay-awake switches saved both ways.
await page.goto(`${BASE}/settings?tab=Computer`, { waitUntil: 'domcontentloaded' })
const computer = section('This computer')
check(await until(() => computer.isVisible()), 'This computer has a tab of its own')
const signIn = computer.getByRole('switch', { name: /Start Yoru when I sign in/ })
check(await until(() => signIn.isVisible()), 'with a start-at-sign-in switch')
check((await signIn.getAttribute('aria-checked')) === 'false', 'off, as this copy was never added to the startup list')
for (const [label, key] of [['Stay awake while another device watches', 'power.awake_watching'], ['Stay awake while downloading', 'power.awake_downloading']]) {
  const sw = row(label).getByRole('switch')
  check((await sw.getAttribute('aria-checked')) === 'true', `${label}: on by default`)
  await sw.click()
  check(await until(async () => (await prefs())[key] === 'false'), `${label}: off is saved`)
  check(await until(() => row(label).getByText(/sleeps on its own schedule|stop while this computer sleeps/).isVisible()),
    `${label}: and says what off means`)
  await sw.click()
  check(await until(async () => (await prefs())[key] === 'true'), `${label}: on again is saved`)
}
check(await computer.getByText(/The screen still turns off/).isVisible(), 'staying awake still lets the screen turn off')
await computer.screenshot({ path: `${SHOTS}/host-this-computer.png` })

// Devices: the proxy row, with the link that pairs through it.
await page.goto(`${BASE}/settings?tab=Devices`, { waitUntil: 'domcontentloaded' })
check(await until(() => row('Behind a proxy').isVisible()), 'Devices says devices come through a proxy')
check(await row('Behind a proxy').getByText(/from 203\.0\.113\.4/).isVisible(), 'and from where')
check(await row('Behind a proxy').getByText(/^https:\/\/yoru\.example\.com\/\?token=/).isVisible(), 'with the link to pair through it')
await row('Behind a proxy').screenshot({ path: `${SHOTS}/host-proxy-row.png` })

// About: every source named with what it gives, TheTVDB linked as its terms ask.
await page.goto(`${BASE}/settings?tab=About`, { waitUntil: 'domcontentloaded' })
const data = section('Data')
check(await until(() => data.isVisible()), 'About has the Data section')
for (const name of ['AniList', 'TheTVDB and AniDB, through ani.zip', 'AniSkip', 'SeaDex', 'MyAnimeList, through Jikan and Tenrai']) {
  check(await data.getByText(name, { exact: true }).isVisible(), `credits ${name}`)
}
check((await data.getByRole('link', { name: 'thetvdb.com' }).getAttribute('href')) === 'https://thetvdb.com', 'links thetvdb.com')

// Downloads: free space in the header, a full drive in words on the row, the queue saying it waits for room.
const GB = 1 << 30
await page.route('**/api/cache', (route) => route.fulfill({ json: { bytes: 0, budget: 5 * GB, freeBytes: 1.5 * GB, drive: 'D:' } }))
await page.route('**/api/downloads', (route) => route.fulfill({ json: { count: 2, items: [
  { infoHash: 'f1', animeId: 7, title: 'Full Drive Show', name: '[G] Full Drive Show - 01.mkv', episode: '1', episodes: ['1'],
    totalBytes: GB, bytesOnDisk: GB / 3, percent: 33, pinned: false, kept: true, state: 'live',
    warning: "can't write the download to disk: There is not enough space on the disk.", freeBytes: 20 << 20, drive: 'D:' },
  { infoHash: 'f2', animeId: 8, title: 'Locked Folder Show', name: '[G] Locked Folder Show - 01.mkv', episode: '1', episodes: ['1'],
    totalBytes: GB, bytesOnDisk: GB / 2, percent: 50, pinned: false, kept: true, state: 'live',
    warning: "can't write the download to disk: Access is denied.", freeBytes: 300 * GB, drive: 'D:' },
] } }))
await page.route('**/api/download/queue', (route) => route.fulfill({ json: { items: [
  { animeId: 9, epKey: '2', episode: 2, state: 'pending', title: 'Waiting Show' },
], waiting: { 9: 1 }, paused: false, short: { freeBytes: 1.5 * GB, drive: 'D:', needBytes: 2 * GB } } }))
await page.goto(`${BASE}/downloads`, { waitUntil: 'domcontentloaded' })
check(await until(() => page.getByText(/1\.5 GB free on D:/).first().isVisible()), 'the header says the room left', '1.5 GB free on D:')
check(await until(() => page.getByText(/^Disk full · D: has 20\.0 MB free/).isVisible()), 'a full drive is said on its row')
check(await page.getByRole('link', { name: 'Free space' }).first().isVisible(), 'with a way to free some')
check(await page.getByText("Can't write the download to disk: Access is denied.").isVisible(),
  "another write failure says the engine's words, not disk full")
check(await page.getByText(/Downloads wait for room: D: has 1\.5 GB free, and they need 2\.0 GB/).isVisible(),
  'the queue says it waits for room')
await page.screenshot({ path: `${SHOTS}/host-downloads.png` })
await page.unroute('**/api/downloads')
await page.unroute('**/api/download/queue')

// Download all asks first when the show won't fit, and Cancel queues nothing.
const posted = []
await page.route('**/api/download/episodes', (route) => {
  posted.push(route.request().postData())
  return route.fulfill({ json: { queued: 0 } })
})
await page.goto(`${BASE}/anime/${ANIME}`, { waitUntil: 'domcontentloaded' })
const all = page.getByRole('button', { name: /^Download (all \d+|1 episode)$/ })
if (await until(() => all.isVisible(), 30_000)) {
  await all.click()
  check(await until(() => page.getByRole('button', { name: 'Download anyway' }).isVisible()), 'Download all asks first when it won\'t fit')
  check(await page.getByText(/need about .+, and D: has 1\.5 GB free|It needs about .+, and D: has 1\.5 GB free/).isVisible(),
    'saying how much it needs and what is free')
  await page.screenshot({ path: `${SHOTS}/host-download-all.png` })
  await page.getByRole('button', { name: 'Cancel' }).click()
  check(await until(() => all.isVisible()) && posted.length === 0, 'Cancel goes back and queues nothing')
} else {
  check(false, 'the show page offers Download all', 'no button (AniList unreachable?)')
}
await page.unroute('**/api/cache')

// The installed app's worker: pages still load through it, and a page that can't load says why, then recovers.
await page.goto(`${BASE}/settings`, { waitUntil: 'domcontentloaded' })
await page.evaluate(async () => {
  await navigator.serviceWorker.register('/sw.js')
  await navigator.serviceWorker.ready
})
await page.reload({ waitUntil: 'domcontentloaded' })
check(await until(() => page.evaluate(() => !!navigator.serviceWorker.controller)), 'the worker takes the page')
check(await until(() => page.getByRole('tab').first().isVisible()), 'pages load through it as before')
let down = true
await context.route('**/unreachable-probe', (route) => (down ? route.abort('connectionrefused') : route.fallback()))
await page.goto(`${BASE}/unreachable-probe`, { waitUntil: 'domcontentloaded' }).catch(() => {})
check(await until(() => page.getByRole('heading', { name: "Can't reach Yoru" }).isVisible()), "a page that can't load says Yoru can't be reached")
check(await page.getByRole('button', { name: 'Try again' }).isVisible(), 'with Try again')
await page.screenshot({ path: `${SHOTS}/host-offline.png` })
down = false
check(await until(() => page.getByRole('heading', { name: "Can't reach Yoru" }).isHidden(), 15_000), 'and loads the page once Yoru answers')

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
