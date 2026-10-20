// Keys while watching: D shows the downloads over the player without stopping it, S skips the opening,
// ? lists every shortcut. The downloads list and skip range are fed to the page.
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

const GB = 1 << 30
const download = (infoHash, name, more = {}) => ({
  infoHash, name, episodes: [], totalBytes: GB, bytesOnDisk: GB * 0.4, percent: 40,
  pinned: false, kept: false, state: 'live', mbps: 1.2, peers: 2, ...more,
})
const cover = 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="4" height="6"><rect width="4" height="6" fill="#88f"/></svg>')
const items = [
  download('a3', '[Group] Show One - 03 [1080p].mkv', { animeId: 1, title: 'Show One', episode: '3', episodes: ['3'], pinned: true, cover }),
  download('a4', '[Group] Show One - 04 [1080p].mkv', { animeId: 1, title: 'Show One', episode: '4', episodes: ['4'], percent: 20, bytesOnDisk: GB * 0.2, cover }),
  // The queue gave up on this one: its entry below has failed, and this is what it got.
  download('a9', '[Group] Show One - 09 [1080p].mkv', { animeId: 1, title: 'Show One', episode: '9', episodes: ['9'], percent: 87, bytesOnDisk: GB * 0.87, paused: true, cover }),
  download('x1', '[Stray] Other Show - 01 [1080p].mkv', { percent: 5, bytesOnDisk: GB * 0.05, mbps: 0 }),
  download('b1', '[Group] Show Two - 01 [1080p].mkv', { animeId: 2, title: 'Show Two', episode: '1', episodes: ['1'], percent: 100, bytesOnDisk: GB }),
]
const queued = [
  { animeId: 3, epKey: '7', episode: 7, state: 'failed', error: 'no release found', title: 'Show Three' },
  { animeId: 1, epKey: '5', episode: 5, state: 'pending', title: 'Show One', cover },
  { animeId: 1, epKey: '9', episode: 9, state: 'failed', title: 'Show One', cover, error: 'no peers sent anything for 10 minutes; 87% is on disk' },
  // A whole season queued while an episode plays.
  ...Array.from({ length: 22 }, (_, i) => ({ animeId: 4, epKey: String(i + 1), episode: i + 1, state: 'pending', title: 'Show Four' })),
]

const browser = await chromium.launch()
const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, permissions: ['clipboard-read', 'clipboard-write'] })
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
let proceeded = 0
let pausedAll = 0
await page.route('**/api/download/queue', (route) => route.fulfill({ json: { items: queued, waiting: {}, watching: proceeded === 0 } }))
await page.route('**/api/download/queue/pause', (route) => {
  pausedAll++
  return route.fulfill({ json: { paused: true } })
})
await page.route('**/api/download/queue/proceed', (route) => {
  proceeded++
  return route.fulfill({ json: { watching: false } })
})
await page.route('**/api/downloads', (route) => route.fulfill({ json: { items, count: items.length } }))
// An opening over the first minute and a half, so the skip button shows from the start.
await page.route('**/api/episode/skips**', (route) => route.fulfill({ json: { ranges: [{ kind: 'op', start: 0, end: 90 }] } }))

await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
const video = page.locator('video').first()
await video.waitFor({ timeout: 30_000 })
await page.waitForFunction(() => (document.querySelector('video')?.readyState ?? 0) >= 2, null, { timeout: 30_000 }).catch(() => {})
const state = () => video.evaluate((v) => ({ paused: v.paused, time: v.currentTime, duration: v.duration }))
// Keys go to the page, not to a control that happens to hold focus.
await page.locator('body').click({ position: { x: 5, y: 400 } })

const panel = page.getByRole('dialog', { name: 'Downloads' })
check((await panel.count()) === 0, 'the downloads panel starts closed')
const before = await state()
await page.keyboard.press('d')
check(await panel.waitFor({ timeout: 3000 }).then(() => true, () => false), 'D opens the downloads over the player')
// A glance keeps the keys with the player: Space pauses the video, never presses the panel's Pause all.
await page.keyboard.press(' ')
await sleep(400)
check((await state()).paused !== before.paused && pausedAll === 0, 'Space still plays or pauses the video with the panel up',
  `paused all ${pausedAll}x`)
await page.keyboard.press(' ')
await sleep(300)
const text = (await panel.innerText().catch(() => '')).replace(/\n/g, ' | ')
// The show is the card, its episodes the lines under it: the name is said once.
check(/Show One[ |]+Episode 3[ |]+playing[ |]+40%/.test(text) && /1\.2 Mbps/.test(text) && /2 peers/.test(text), 'a download shows its show, episode, progress, speed and peers', text)
check((text.match(/Show One/g) ?? []).length === 1 && /Episode 4[ |]+20%/.test(text), 'two episodes of one show share its card')
check(/Episode 4[ |]+20%[^|]*\|[ |]*Episode 5[ |]+waiting/.test(text), 'the one waiting its turn is a line of the same card, marked waiting', text)
check(!/DOWNLOADING|WAITING|FAILED/.test(text), 'shows are the only grouping: no downloading, waiting or failed headings')
check((await panel.locator('img[src^="data:"]').count()) === 1, 'the card carries the show poster, once')
check(/\[Stray\] Other Show - 01/.test(text), 'a download under no show is listed by its release name')
check(/Show Three[ |]+Episode 7[ |]+no release found/.test(text), 'a failed one says why')
check((text.match(/Episode 9/g) ?? []).length === 1 && /Episode 9[ |]+87% on disk[ |]+no peers sent anything/.test(text),
  'a failed download is one line with what it got, not that and a paused one', text)
check(!/Show Two/.test(text) && /1 on disk/.test(text), 'finished ones are counted, not listed')
check(/Show Four[ |]+Episodes 1–22[ |]+waiting/.test(text) && !/Episode 15\b/.test(text),
  'a season waiting its turn is one line, not twenty-two', text)
check(/Waiting until the episode you're watching has downloaded/.test(text), 'it says the queue waits for the episode on screen')
await panel.getByRole('button', { name: 'Download anyway' }).click()
check(await until(() => proceeded > 0), 'Download anyway asks the queue to carry on')
check(await until(() => panel.getByText(/Waiting until the episode/).isHidden()), 'and the note goes once it does')
check(page.url().includes('/watch/'), 'the episode page is still open')
check((await state()).paused === before.paused, 'opening it does not pause or start the video')
await sleep(600)
await page.screenshot({ path: `${SHOTS}/shortcuts-downloads.png` })

// The player keeps its keys while the panel is up.
await page.keyboard.press('k')
await sleep(400)
check((await state()).paused !== before.paused, 'K still plays or pauses with the panel open')
await page.keyboard.press('k')

await panel.getByRole('button', { name: 'Copy details' }).click()
await sleep(300)
const copied = await page.evaluate(() => navigator.clipboard.readText()).catch((e) => `unreadable: ${e}`)
check(/Downloading \(3\)/.test(copied) && /Show One episode 3 — 40%/.test(copied) && /\[Stray\] Other Show/.test(copied) && /Failed \(2\)/.test(copied) && /episode 9 — 87% on disk — no peers/.test(copied) && /On disk \(1\)/.test(copied),
  'copy details puts the whole list on the clipboard as text', copied.replace(/\n/g, ' | ').slice(0, 300))

await page.keyboard.press('d')
await sleep(300)
check((await panel.count()) === 0, 'D closes it again')
await page.keyboard.press('d')
await panel.waitFor({ timeout: 3000 }).catch(() => {})
await page.keyboard.press('Escape')
await sleep(300)
check((await panel.count()) === 0, 'Escape closes it too')

// Typing a "d" is not a shortcut.
await page.locator('#yoru-search').fill('d')
await page.locator('#yoru-search').press('d')
await sleep(300)
check((await panel.count()) === 0, 'typing in search does not open it')
await page.locator('#yoru-search').fill('')
await page.locator('body').click({ position: { x: 5, y: 400 } })

// S is the skip button, pressed.
const skip = page.getByRole('button', { name: /^Skip opening/ })
check(await skip.waitFor({ timeout: 8000 }).then(() => true, () => false), 'the skip button shows during the opening')
await video.evaluate((v) => (v.currentTime = 1))
await page.keyboard.press('s')
await sleep(800)
const after = await state()
check(after.time >= Math.min(90, after.duration - 1) - 1, 'S jumps to the end of the opening', JSON.stringify(after))

// ? lists the keys, the new ones included.
await page.keyboard.press('?')
const help = page.getByRole('dialog', { name: 'Keyboard shortcuts' })
check(await help.waitFor({ timeout: 3000 }).then(() => true, () => false), '? opens the list of shortcuts')
const listed = await help.innerText().catch(() => '')
check(/Play or pause/.test(listed) && /Skip the opening/.test(listed) && /Show or hide downloads/.test(listed) && /Subtitles earlier or later/.test(listed), 'it lists the player keys and the new ones')
check(/Next episode/.test(listed), 'and N for the next episode')
await sleep(600)
await page.screenshot({ path: `${SHOTS}/shortcuts-list.png` })
await page.keyboard.press('Escape')
await sleep(300)
check((await help.count()) === 0, 'Escape closes the list')

// The player teaches its own keys: each button's tooltip names one, and one button opens the list.
const tip = (name) => page.getByRole('button', { name, exact: true }).first().getAttribute('title').catch(() => null)
const tips = [await tip(/^(Play|Pause)$/), await tip('Back 10 seconds'), await tip('Forward 10 seconds'), await tip(/^(Mute|Unmute)$/), await tip('Fullscreen')]
check(tips.join(' | ') && /\(K\)/.test(tips[0]) && /\(J\)/.test(tips[1]) && /\(L\)/.test(tips[2]) && /\(M\)/.test(tips[3]) && /\(F\)/.test(tips[4]),
  'player buttons name their key in the tooltip', tips.join(' | '))
check(/\(N\)/.test((await tip('Next episode')) ?? ''), 'the Next episode button names N', await tip('Next episode'))
await page.locator('video').first().hover()
await page.getByRole('button', { name: 'Keyboard shortcuts', exact: true }).click()
check(await help.waitFor({ timeout: 3000 }).then(() => true, () => false), 'the keyboard button in the player bar opens the list')
await page.keyboard.press('Escape')
await sleep(300)

// From the profile menu, for whoever does not know the key.
await page.getByRole('button', { name: 'Profile and settings' }).click()
await page.getByRole('menuitem', { name: /Keyboard shortcuts/ }).click()
check(await help.waitFor({ timeout: 3000 }).then(() => true, () => false), 'the profile menu opens the list as well')
await page.keyboard.press('Escape')

// Fullscreen shows only what is inside the fullscreen element, so the panel has to be drawn there.
await page.locator('body').click({ position: { x: 5, y: 400 } })
await page.keyboard.press('f')
const full = await page.waitForFunction(() => !!document.fullscreenElement, null, { timeout: 4000 }).then(() => true, () => false)
if (!full) {
  console.log('skip the fullscreen steps: this browser refused fullscreen')
} else {
  await page.keyboard.press('d')
  await panel.waitFor({ timeout: 3000 }).catch(() => {})
  const inside = await page.evaluate(() => !!document.fullscreenElement?.querySelector('[role=dialog][aria-label=Downloads]'))
  check(inside, 'in fullscreen the panel is drawn inside the player')
  await sleep(600)
  await page.screenshot({ path: `${SHOTS}/shortcuts-fullscreen.png` })
  await page.keyboard.press('d')
}

// Speeds in megabytes once Settings says so: 1.2 Mbps is 146 KB/s, counted as file sizes are.
const setSpeed = (value) => fetch(`${BASE}/api/prefs`, {
  method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ key: 'display.speed', value }),
})
await setSpeed('mbs')
await page.reload({ waitUntil: 'domcontentloaded' })
await video.waitFor({ timeout: 30_000 })
await page.locator('body').click({ position: { x: 5, y: 400 } })
await page.keyboard.press('d')
await panel.waitFor({ timeout: 3000 }).catch(() => {})
const inBytes = (await panel.innerText().catch(() => '')).replace(/\n/g, ' | ')
check(/146 KB\/s/.test(inBytes) && !/Mbps/.test(inBytes), 'with MB/s chosen the panel shows speeds in them', inBytes.slice(0, 200))
await setSpeed('mbps')
await page.keyboard.press('d')
await sleep(300)

// N presses the Next episode button: not while typing in a box, then on the player it goes to episode 2.
const searchBox = page.getByRole('combobox', { name: 'Search anime' })
await searchBox.click()
await page.keyboard.press('n')
await sleep(500)
check(page.url().endsWith(`/watch/${ANIME}/1`), 'N typed in the search box stays a letter', page.url())
await page.keyboard.press('Escape')
await page.locator('body').click({ position: { x: 5, y: 400 } })
await page.keyboard.press('n')
check(await until(() => page.url().endsWith(`/watch/${ANIME}/2`)), 'N goes to the next episode', page.url())

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
