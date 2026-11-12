import { mkdirSync } from 'node:fs'
import { join } from 'node:path'
import { chromium } from 'playwright'
import { prepareLocalEpisode } from './harness-lib.mjs'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const HASH = 'ab'.repeat(20)
const FRIEND = BASE.replace('127.0.0.1', 'localhost')
const SHOTS = process.env.SHOTS
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

if (SHOTS) mkdirSync(SHOTS, { recursive: true })
const shot = async (page, name) => {
  if (!SHOTS) return
  const panel = page.locator('section[aria-label="Watch together"], section[aria-label="Open a room"], section[aria-label="Join the room"]').first()
  await panel.scrollIntoViewIfNeeded().catch(() => {})
  await sleep(300)
  await panel.screenshot({ path: join(SHOTS, `${name}.png`) }).catch(() => {})
}

check(await prepareLocalEpisode({ base: BASE, lib: process.env.YORU_LIB, anime: ANIME }), 'test episode ready')
const PICTURE = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='
const pictured = await fetch(`${BASE}/api/profile/avatar`, {
  method: 'PUT',
  headers: { 'content-type': 'application/json' },
  body: JSON.stringify({ image: PICTURE }),
})
check(pictured.ok, 'the profile takes a picture', String(pictured.status))
const loaded = (page, selector) =>
  until(() => page.locator(selector).first().evaluate((img) => img.complete && img.naturalWidth > 0))
const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const errors = []

async function viewer(name, width = 1400) {
  const context = await browser.newContext({ viewport: { width, height: 1000 } })
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
  await shot(host, 'host-name')
  const nameBox = host.getByRole('textbox', { name: 'Your name' })
  check((await nameBox.getAttribute('autocomplete')) === 'off', "the name box keeps the browser's old entries out")
  await nameBox.fill('Tons')
  await host.getByRole('button', { name: 'Open room' }).click()
  const room = await (await opened).json()
  check(await until(async () => (await (await fetch(`${BASE}/api/profile`)).json()).name === 'Tons'), 'the name given is remembered as the Yoru name')

  check(typeof room.link === 'string' && room.link.includes('/together/'), 'the host opens a room with a link to share', room.link)
  check(await until(() => host.getByRole('button', { name: 'Copy link' }).isVisible()), 'the room shows, with the invite')
  await shot(host, 'host-alone')

  await host.reload({ waitUntil: 'domcontentloaded' })
  check(await until(() => host.getByRole('button', { name: 'Copy link' }).isVisible()), 'a reload keeps the room')
  await video(host).waitFor({ timeout: 30_000 })
  await host.evaluate(() => { const v = document.querySelector('video'); v.muted = true; v.play().catch(() => {}) })

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
  await shot(friend, 'friend-name')
  await name.fill('Alex')
  await friend.getByRole('button', { name: 'Join', exact: true }).click()
  check(await until(() => friend.getByText('2 watching').isVisible()), 'the friend is in')
  check(await until(() => host.getByText('Alex', { exact: true }).isVisible()), 'and the host sees them')
  check(await until(() => friend.getByText('Tons', { exact: true }).isVisible()), 'and they see the host by the name given')
  check(await loaded(friend, 'section[aria-label="Watch together"] img'), "with the host's picture")
  check(await loaded(host, 'header img[src^="/api/profile/avatar"]'), "which is in the host's own top bar too")
  await video(friend).waitFor({ timeout: 30_000 })

  await until(async () => !(await paused(friend)), 20_000)
  await host.evaluate(() => document.querySelector('video').pause())
  check(await until(() => paused(friend)), "the host's pause pauses the friend")
  check(await until(() => friend.locator('[aria-label="Room messages"]').getByText(/paused/).isVisible()), 'with who paused, in the feed')
  const key = async (page, k) => {
    await page.evaluate(() => document.activeElement instanceof HTMLElement && document.activeElement.blur())
    await page.keyboard.press(k)
  }
  await key(host, '2')
  check(await until(async () => Math.abs((await at(friend)) - 36) < 1), "the host's jump moves the friend", String(await at(friend)))
  await friend.evaluate(() => (document.querySelector('video').currentTime = 100))
  await sleep(2000)
  check(Math.abs((await at(host)) - 36) < 1, "a player moving by itself does not move the room", `host at ${(await at(host)).toFixed(1)}`)
  check(await until(async () => Math.abs((await at(friend)) - 36) < 1.5), 'and it comes back to the room', `friend at ${(await at(friend)).toFixed(1)}`)
  check(!(await host.locator('[aria-label="Room messages"]').getByText('Alex jumped').count()), 'with no "jumped" line for it')
  await friend.evaluate(() => { const v = document.querySelector('video'); v.muted = true; v.play().catch(() => {}) })
  check(await until(async () => !(await paused(host))), "the friend's play plays the host's too")
  await sleep(3000)
  const drift = Math.abs((await at(host)) - (await at(friend)))
  check(drift < 1, 'and they stay together', `${drift.toFixed(2)} s apart`)

  const statusOf = (page, who) =>
    page.locator('section[aria-label="Watch together"] li', { hasText: who }).first().locator('[data-status]').textContent()
  const turns = async (page, who, word, ms = 4000) => {
    const from = Date.now()
    return (await until(async () => (await statusOf(page, who)) === word, ms)) ? Date.now() - from : -1
  }
  let took = await turns(host, 'Alex', 'Ready')
  check(took >= 0, 'the host sees the friend as Ready while both play', `${took} ms`)
  await friend.evaluate(() => document.querySelector('video').dispatchEvent(new Event('waiting')))
  took = await turns(host, 'Alex', 'Loading')
  check(took >= 0 && took < 1500, 'a friend who stalls turns to Loading for the host at once', `${took} ms`)
  await friend.evaluate(() => document.querySelector('video').dispatchEvent(new Event('canplay')))
  took = await turns(host, 'Alex', 'Ready')
  check(took >= 0 && took < 1500, 'and back to Ready once they can play', `${took} ms`)
  check(await until(async () => !(await paused(host)) && !(await paused(friend)), 8000), 'and the room plays on')
  await key(host, '4')
  check(await until(async () => (await at(friend)) >= 72 && (await at(host)) >= 72), 'a jump while playing moves everyone, and is not snapped back', `host ${(await at(host)).toFixed(1)}, friend ${(await at(friend)).toFixed(1)}`)
  took = await turns(friend, 'Tons', 'Ready', 8000)
  const both = await turns(host, 'Alex', 'Ready', 8000)
  check(took >= 0 && both >= 0, 'after a jump, both show Ready again on both screens', `${took} ms, ${both} ms`)
  await friend.getByRole('textbox', { name: 'Message' }).fill('this opening')
  await friend.getByRole('button', { name: 'Send' }).click()
  check(await until(() => host.locator('[aria-label="Room messages"]').getByText('this opening').isVisible()), 'chat reaches the host')
  if (process.env.MESSAGE_SHOT) await host.screenshot({ path: process.env.MESSAGE_SHOT }).catch(() => {})
  await friend.getByRole('button', { name: 'React 😂' }).click()
  check(await until(() => host.locator('[aria-label="Room messages"]').getByText('😂').isVisible()), 'and reactions')
  await shot(friend, 'friend-room')

  const hostPanel = host.locator('section[aria-label="Watch together"]')
  const friendPanel = friend.locator('section[aria-label="Watch together"]')
  check(await until(() => hostPanel.getByText(/Episode 1 · Japanese audio/).isVisible()), 'the room says what is playing, and in which audio')
  check(await until(() => hostPanel.getByRole('button', { name: 'Change what we watch' }).isVisible()), 'the host may change what the room watches')
  check(await until(() => friendPanel.getByRole('button', { name: 'Suggest something' }).isVisible()), 'a friend may suggest instead')

  await friend.getByRole('link', { name: /^Episode 2\b/ }).first().click()
  check(await until(() => friend.getByRole('heading', { name: "You're in a room" }).isVisible()), "a friend opening another episode is told they're in a room")
  check(new URL(friend.url()).pathname === `/watch/${ANIME}/2` && (await at(host)) > 0, 'without moving the room')
  await friend.screenshot({ path: SHOTS ? join(SHOTS, 'friend-stray.png') : undefined }).catch(() => {})
  await friend.getByRole('button', { name: /^Suggest this/ }).click()
  check(await until(async () => new URL(friend.url()).pathname === `/watch/${ANIME}/1`), 'suggesting it brings them back to the room', friend.url())
  check(await until(() => hostPanel.getByText(/Suggested by Alex/).isVisible()), 'and the host sees the suggestion')
  check(await until(() => hostPanel.getByRole('button', { name: 'Play for everyone' }).isVisible()), 'with a way to play it for everyone')
  check(await until(() => host.locator('[aria-label="Room messages"]').getByText(/Alex suggests .*episode 2/).isVisible()), 'and it is in the feed')
  check(await until(() => friendPanel.getByRole('button', { name: 'Take back' }).isVisible()), 'the friend can take it back')
  await shot(host, 'host-suggestion')
  await hostPanel.getByRole('button', { name: 'Dismiss' }).click()
  check(await until(async () => !(await friendPanel.getByText(/Suggested by/).isVisible())), 'dismissed by the host, it goes for everyone')

  await host.getByRole('switch', { name: "Anyone can change what's playing" }).click()
  check(await until(() => friendPanel.getByRole('button', { name: 'Change what we watch' }).isVisible()), 'with anyone allowed, the friend may change it too')
  await host.getByRole('switch', { name: "Anyone can change what's playing" }).click()
  check(await until(() => friendPanel.getByRole('button', { name: 'Suggest something' }).isVisible()), 'and not once it is off again')

  await key(friend, 't')
  await host.getByRole('textbox', { name: 'Message' }).fill('read in focus mode')
  await host.getByRole('button', { name: 'Send' }).click()
  await sleep(1500)
  await key(friend, 't')
  await friend.getByRole('link', { name: 'Home', exact: true }).first().click()
  const bubble = friend.getByRole('button', { name: /^Watch together room/ })
  check(await until(() => bubble.isVisible()), 'leaving the episode page, a room bubble stays on screen')
  const place = async () => {
    const b = await bubble.boundingBox()
    const view = friend.viewportSize()
    return `${b.y + b.height / 2 < view.height / 2 ? 't' : 'b'}${b.x + b.width / 2 < view.width / 2 ? 'l' : 'r'}`
  }
  check((await place()) === 'tl', 'at the top left to begin with', await place())
  const mini = friend.getByRole('button', { name: 'Close the player' })
  if (await until(() => mini.isVisible(), 4000)) await mini.click()
  check(await until(async () => !(await friend.locator('video').count())), 'closing the miniplayer closes the video')
  check(await bubble.isVisible(), 'and keeps the room')
  check(await until(() => hostPanel.getByText('Browsing', { exact: true }).isVisible()), 'the host sees the friend as browsing')

  await hostPanel.getByRole('textbox', { name: 'Message' }).fill('still there?')
  await hostPanel.getByRole('button', { name: 'Send' }).click()
  const popped = friend.getByText('still there?')
  check(await until(() => popped.isVisible()), 'a message pops up next to the bubble')
  const beside = await until(async () => {
    const b = await bubble.boundingBox()
    const m = await popped.boundingBox()
    return m.x > b.x + b.width - 4 && Math.abs(m.y - b.y) < 80
  })
  check(beside, 'to its right, level with it')
  check(await until(async () => /1 new message$/.test((await bubble.getAttribute('aria-label')) ?? '')), 'and the bubble counts it as unread', (await bubble.getAttribute('aria-label')) ?? '')
  if (process.env.BUBBLE_SHOT) await friend.screenshot({ path: process.env.BUBBLE_SHOT }).catch(() => {})

  await bubble.click()
  check(await until(async () => /watching$/.test((await bubble.getAttribute('aria-label')) ?? '')), 'opening the card clears the count')
  const card = friend.getByRole('region', { name: 'Watch together room' })
  check(await until(() => card.getByRole('button', { name: 'Open room' }).isVisible()), 'the bubble opens a small room card')
  await card.getByRole('textbox', { name: 'Message' }).fill('browsing, back soon')
  await card.getByRole('button', { name: 'Send' }).click()
  check(await until(() => host.locator('[aria-label="Room messages"]').getByText('browsing, back soon').isVisible()), 'chat from the card reaches the room')
  if (process.env.BUBBLE_SHOT) await friend.screenshot({ path: process.env.BUBBLE_SHOT.replace('.png', '-card.png') }).catch(() => {})
  await friend.keyboard.press('Escape')

  const from = await bubble.boundingBox()
  await friend.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await friend.mouse.down()
  await friend.mouse.move(120, friend.viewportSize().height - 160, { steps: 8 })
  await friend.mouse.up()
  check(await until(async () => (await place()) === 'bl'), 'dragged, it settles in the nearest corner', await place())
  await friend.reload({ waitUntil: 'domcontentloaded' })
  check(await until(() => bubble.isVisible()), 'a reload keeps the room and its bubble')
  check(await until(async () => (await place()) === 'bl'), 'where it was put', await place())

  await bubble.click()
  await card.getByRole('button', { name: 'Open room' }).click()
  check(await until(async () => new URL(friend.url()).pathname === `/watch/${ANIME}/1`), 'Open room goes back to the episode', friend.url())
  check(await until(() => friendPanel.getByText('3 watching').or(friendPanel.getByText('2 watching')).first().isVisible()), 'with the full room panel')
  check(await until(async () => !(await bubble.isVisible())), 'where the bubble steps aside')
  check(await until(async () => !(await hostPanel.getByText('Browsing', { exact: true }).isVisible())), 'and the friend is no longer browsing')

  const files = (await (await fetch(`${BASE}/api/local/files`)).json()).items ?? []
  const other = files.find((f) => / - 03\./.test(String(f.path ?? '')))
  const assigned =
    !!other &&
    (
      await fetch(`${BASE}/api/local/assign`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ id: other.id, animeId: ANIME, episode: 3 }),
      })
    ).ok
  check(assigned, 'a second episode is ready')
  await friend.goto(`${FRIEND}/watch/${ANIME}/3`, { waitUntil: 'domcontentloaded' })
  check(
    await until(() => friend.getByRole('heading', { name: "You're in a room" }).isVisible()),
    "a friend opening another episode from a fresh page is told they're in a room",
  )
  await sleep(2500)
  check(new URL(friend.url()).pathname === `/watch/${ANIME}/3`, 'and is not pulled back to the room', friend.url())
  await friend.getByRole('button', { name: 'Back to the room' }).click()
  check(await until(async () => new URL(friend.url()).pathname === `/watch/${ANIME}/1`), 'Back to the room returns them', friend.url())
  await video(friend).waitFor({ timeout: 30_000 })

  await host.goto(`${BASE}/watch/${ANIME}/3`, { waitUntil: 'domcontentloaded' })
  check(
    await until(() => host.locator('section[aria-label="Watch together"]').getByText(/Episode 3 ·/).isVisible(), 40_000),
    'the host opening another episode from a fresh page moves the room to it',
  )
  check(new URL(host.url()).pathname === `/watch/${ANIME}/3`, 'and the host stays on it', host.url())
  check(await until(async () => new URL(friend.url()).pathname === `/watch/${ANIME}/3`, 20_000), 'and the friend follows', friend.url())
  check(await until(() => friend.locator('[aria-label="Room messages"]').getByText(/Tons switched to episode 3/).isVisible()), 'with who switched, in the feed')
  await video(friend).waitFor({ timeout: 30_000 })

  await host.getByRole('switch', { name: 'Ask before someone joins' }).click()
  const third = await viewer('third', 390)
  await third.goto(`${FRIEND}/open?url=${encodeURIComponent(room.link)}`, { waitUntil: 'domcontentloaded' })
  const thirdName = third.getByRole('textbox', { name: 'Your name' })
  await thirdName.waitFor()
  check(await until(async () => (await thirdName.inputValue()) === 'Alex'), 'the name box starts with the name used last time on that Yoru', await thirdName.inputValue())
  await thirdName.fill('')
  await third.getByRole('button', { name: 'Join', exact: true }).click()
  check(await until(() => third.getByText(/Waiting for the host to let you in/).isVisible()), 'with asking on, a third waits')
  check(await until(() => host.getByText('Friend wants to join').isVisible()), 'and the host is asked, a blank name as Friend')
  await shot(host, 'host-knock')
  await host.getByRole('button', { name: 'Let in' }).click()
  check(await until(() => third.getByText('3 watching').isVisible()), 'let in, they join')
  await shot(host, 'host-room')
  await shot(third, 'phone-room')

  for (const page of [host, third]) {
    const rows = await page.evaluate(() =>
      [...document.querySelectorAll('section[aria-label="Watch together"] [role="switch"]')].map((s) => {
        const sw = s.getBoundingClientRect()
        const label = [...document.querySelectorAll('section[aria-label="Watch together"] span')].find(
          (el) => el.textContent === s.getAttribute('aria-label') && el.children.length === 0,
        )
        const l = label?.getBoundingClientRect()
        return { name: s.getAttribute('aria-label'), top: sw.top, bottom: sw.bottom, labelMid: l ? (l.top + l.bottom) / 2 : -1, labelRight: l?.right ?? -1, left: sw.left }
      }),
    )
    const alone = rows.every((r, i) => rows.every((o, j) => i === j || r.bottom <= o.top || o.bottom <= r.top))
    const own = rows.every((r) => r.labelMid >= r.top - 12 && r.labelMid <= r.bottom + 12 && r.labelRight <= r.left)
    check(rows.length > 0 && alone && own, `each switch sits on its own row beside its own label (${page === host ? 'host' : 'phone'})`, JSON.stringify(rows.map((r) => r.name)))
  }

  await friend.getByRole('button', { name: 'Leave room' }).click()
  check(await until(() => host.getByText('2 watching').isVisible()), 'someone leaving leaves the rest watching')
  const gone = Date.now()
  await third.close()
  check(await until(async () => (await statusOf(host, 'Friend')) === 'Away', 5000), 'a closed tab shows as Away for the host', `${Date.now() - gone} ms`)
  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
