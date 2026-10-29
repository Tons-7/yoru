// Watch together, one scratch Yoru standing in for two: a friend joins by link, pause, seek and play reach
// them, chat reaches the host, and asking first holds a third at the door.
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const HASH = 'ab'.repeat(20)
const FRIEND = BASE.replace('127.0.0.1', 'localhost')
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 15_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(200)) if (await fn().catch(() => false)) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')
const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const errors = []

// The local test file stands in for a torrent: answers carry a release hash; asks for one play the file.
async function viewer(name) {
  const context = await browser.newContext({ viewport: { width: 1400, height: 1000 } })
  const page = await context.newPage()
  page.on('pageerror', (e) => errors.push(`${name}: ${e.message.slice(0, 160)}`))
  await page.route('**/api/play', async (route) => {
    const body = route.request().postDataJSON()
    delete body.infoHash
    const res = await route.fetch({ postData: JSON.stringify(body) })
    route.fulfill({ response: res, json: { ...(await res.json()), infoHash: HASH } })
  })
  return page
}
const video = (page) => page.locator('video').first()
const at = (page) => page.evaluate(() => document.querySelector('video')?.currentTime ?? -1)
const paused = (page) => page.evaluate(() => document.querySelector('video')?.paused ?? true)

try {
  const host = await viewer('host')
  await host.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
  await video(host).waitFor({ timeout: 30_000 })
  await host.evaluate(() => { const v = document.querySelector('video'); v.muted = true; v.play().catch(() => {}) })
  await until(async () => (await at(host)) > 1, 30_000)

  const opened = host.waitForResponse((r) => r.url().endsWith('/api/together') && r.request().method() === 'POST')
  await host.getByRole('button', { name: 'Watch together' }).click()
  check(await until(() => host.getByText(/Left empty, you're Host/).isVisible()), 'opening a room asks the host\'s name first')
  await host.getByRole('textbox', { name: 'Your name' }).fill('Tons')
  await host.getByRole('button', { name: 'Open room' }).click()
  const room = await (await opened).json()
  check(typeof room.link === 'string' && room.link.includes('/together/'), 'the host opens a room with a link to share', room.link)
  check(await until(() => host.getByRole('button', { name: 'Copy invite link' }).isVisible()), 'the room shows, with the invite')

  // The host reloading keeps the room and its invite.
  await host.reload({ waitUntil: 'domcontentloaded' })
  check(await until(() => host.getByRole('button', { name: 'Copy invite link' }).isVisible()), 'a reload keeps the room')
  await video(host).waitFor({ timeout: 30_000 })
  await host.evaluate(() => { const v = document.querySelector('video'); v.muted = true; v.play().catch(() => {}) })

  // The friend pastes the link into their Yoru's search, at another origin, so every ask crosses sites.
  const friend = await viewer('friend')
  await friend.goto(`${FRIEND}/`, { waitUntil: 'domcontentloaded' })
  const search = friend.getByRole('combobox', { name: 'Search anime' })
  await search.fill(room.link)
  check(await until(() => friend.getByRole('option', { name: 'Join this watch-together room' }).isVisible()),
    'a room link in search offers to join it')
  await search.press('Enter')
  check(await until(async () => new URL(friend.url()).pathname === `/watch/${ANIME}/1`), 'it opens the room\'s episode', friend.url())
  check(new URL(friend.url()).searchParams.get('hash') === HASH, 'with the release the room plays')
  const name = friend.getByRole('textbox', { name: 'Your name' })
  check(await until(() => name.isVisible()), 'and asks the friend\'s name')
  await name.fill('Alex')
  await friend.getByRole('button', { name: 'Join', exact: true }).click()
  check(await until(() => friend.getByText('· 2 here').isVisible()), 'the friend is in')
  check(await until(() => host.getByText('Alex', { exact: true }).isVisible()), 'and the host sees them')
  check(await until(() => friend.getByText('Tons', { exact: true }).isVisible()), 'and they see the host by the name given')
  await video(friend).waitFor({ timeout: 30_000 })

  // Everyone controls: the host's pause, seek and play reach the friend.
  await until(async () => !(await paused(friend)), 20_000)
  await host.evaluate(() => document.querySelector('video').pause())
  check(await until(() => paused(friend)), "the host's pause pauses the friend")
  check(await until(() => friend.locator('[aria-label="Room messages"]').getByText(/paused/).isVisible()), 'with who paused, in the feed')
  await host.evaluate(() => (document.querySelector('video').currentTime = 30))
  check(await until(async () => Math.abs((await at(friend)) - 30) < 1), "the host's jump moves the friend", String(await at(friend)))
  await friend.evaluate(() => { const v = document.querySelector('video'); v.muted = true; v.play().catch(() => {}) })
  check(await until(async () => !(await paused(host))), "the friend's play plays the host's too")
  await sleep(3000)
  const drift = Math.abs((await at(host)) - (await at(friend)))
  check(drift < 1, 'and they stay together', `${drift.toFixed(2)} s apart`)

  // Chat and a reaction, from the friend to the host.
  await friend.getByRole('textbox', { name: 'Message' }).fill('this opening')
  await friend.getByRole('button', { name: 'Send' }).click()
  check(await until(() => host.locator('[aria-label="Room messages"]').getByText('this opening').isVisible()), 'chat reaches the host')
  await friend.getByRole('button', { name: 'React 😂' }).click()
  check(await until(() => host.locator('[aria-label="Room messages"]').getByText('😂').isVisible()), 'and reactions')

  // Asking first: a third waits at the door until let in.
  await host.getByRole('switch', { name: 'Ask before someone joins' }).click()
  const third = await viewer('third')
  await third.goto(`${FRIEND}/open?url=${encodeURIComponent(room.link)}`, { waitUntil: 'domcontentloaded' })
  // Their name left empty: Friend.
  await third.getByRole('textbox', { name: 'Your name' }).waitFor()
  await third.getByRole('button', { name: 'Join', exact: true }).click()
  check(await until(() => third.getByText(/Waiting for the host to let you in/).isVisible()), 'with asking on, a third waits')
  check(await until(() => host.getByText('Friend wants to join').isVisible()), 'and the host is asked, a blank name as Friend')
  await host.getByRole('button', { name: 'Let in' }).click()
  check(await until(() => third.getByText('· 3 here').isVisible()), 'let in, they join')

  await friend.getByRole('button', { name: 'Leave' }).click()
  check(await until(() => host.getByText('· 2 here').isVisible()), 'someone leaving leaves the rest watching')
  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
