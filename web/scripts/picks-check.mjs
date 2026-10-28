// AniList and discovery: Home's "Sequels you missed" and "For you", AniList and MyAnimeList links opening
// their show, episode scores on watched rows, and connecting AniList by pasting the code its page shows.
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 20_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(200)) if (await fn().catch(() => false)) return true
  return false
}
const get = async (p) => (await fetch(BASE + p)).json()

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

const browser = await chromium.launch()
const errors = []
const fresh = async () => {
  const page = await browser.newPage({ viewport: { width: 1400, height: 950 } })
  page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
  return page
}
const path = (page) => new URL(page.url()).pathname + new URL(page.url()).search

try {
  // Home: both rails answer on a fresh install, then show what they're given; empty, they aren't there.
  for (const kind of ['sequels', 'for-you']) {
    const res = await fetch(`${BASE}/api/home/${kind}`)
    const body = await res.json().catch(() => ({}))
    check(res.ok && Array.isArray(body.items), `/api/home/${kind} answers a list`, `${res.status} ${JSON.stringify(body).slice(0, 80)}`)
  }
  const shows = (await get('/api/discover?sort=trending&perPage=4')).items ?? []
  check(shows.length === 4, 'four shows to stand in for the picks', String(shows.length))

  let page = await fresh()
  let forYouFails = true
  // The list tags a card shows, as the server would after a status is set.
  const listed = {}
  await page.route('**/api/status', (route) => {
    const { animeId, status } = route.request().postDataJSON()
    listed[animeId] = status
    return route.fulfill({ json: { ok: true } })
  })
  await page.route('**/api/home/sequels', (route) =>
    route.fulfill({ json: { items: shows.slice(0, 2).map((s) => ({ ...s, listStatus: listed[s.id] ?? null })) } }))
  await page.route('**/api/home/for-you', (route) =>
    route.fulfill(forYouFails ? { status: 500, json: { error: 'down' } } : { json: { items: shows.slice(2) } }))
  await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
  const rail = (title) => page.locator('section', { has: page.getByRole('heading', { name: title, exact: true }) }).first()
  const cards = (title) => rail(title).locator('a[href^="/anime/"]')
  check(await until(async () => (await cards('Sequels you missed').count()) >= 2), '"Sequels you missed" shows its shows')
  check(await cards('Sequels you missed').first().getAttribute('href') === `/anime/${shows[0]?.id}`, 'in the order given, each opening its show',
    await cards('Sequels you missed').first().getAttribute('href'))
  check(await until(() => rail('For you').getByText("Couldn't load this.").isVisible()), 'a "For you" that failed says so, with its heading')
  forYouFails = false
  await rail('For you').getByRole('button', { name: 'Try again' }).click()
  check(await until(async () => (await cards('For you').count()) >= 2), 'and Try again fills it')
  // Folded by its heading, a row keeps only that, here and after a reload, until opened again.
  const heading = (title) => rail(title).getByRole('button', { name: title, exact: true })
  await heading('Sequels you missed').click()
  check(await until(async () => (await heading('Sequels you missed').getAttribute('aria-expanded')) === 'false' &&
    !(await cards('Sequels you missed').first().isVisible())), 'a row folds to its heading')
  await page.reload({ waitUntil: 'domcontentloaded' })
  check(await until(async () => (await heading('Sequels you missed').getAttribute('aria-expanded')) === 'false'),
    'and stays folded on this device')
  await heading('Sequels you missed').click()
  check(await until(() => cards('Sequels you missed').first().isVisible()), 'pressed again, it opens')
  // A card's bookmark: the tag picked shows at once, as anywhere else.
  await rail('Sequels you missed').getByRole('button', { name: 'Add to list' }).first().click()
  await page.getByRole('menuitem', { name: 'Plan to watch' }).click()
  check(await until(() => rail('Sequels you missed').getByRole('button', { name: 'Listed as Plan to watch' }).first().isVisible()),
    "a sequel's bookmark fills once it's on the list")
  await page.close()

  // Settings, Home: a row switched off leaves Home, asking AniList nothing; adult titles stay off by default.
  page = await fresh()
  let forYouAsked = 0
  await page.route('**/api/home/for-you', (route) => {
    forYouAsked++
    return route.fulfill({ json: { items: shows.slice(2) } })
  })
  await page.goto(`${BASE}/settings?tab=Language`, { waitUntil: 'domcontentloaded' })
  const forYouSwitch = page.getByRole('switch', { name: 'For you', exact: true })
  check(await until(() => forYouSwitch.isVisible()), 'Settings lists the rows on Home')
  check((await page.getByRole('switch', { name: 'Adult titles in For you' }).getAttribute('aria-checked')) === 'false',
    'adult titles in For you are off to begin with')
  await forYouSwitch.click()
  check(await until(async () => (await forYouSwitch.getAttribute('aria-checked')) === 'false'), 'For you switched off')
  forYouAsked = 0
  await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
  await until(() => page.getByRole('heading', { name: 'Trending now', exact: true }).isVisible())
  check((await page.getByRole('heading', { name: 'For you', exact: true }).count()) === 0 && forYouAsked === 0,
    'and it leaves Home, nothing asked for it', `asked ${forYouAsked}`)
  await fetch(`${BASE}/api/prefs`, { method: 'POST', body: JSON.stringify({ key: 'home.hidden', value: '[]' }) })
  await page.close()

  page = await fresh()
  await page.route('**/api/home/{sequels,for-you}', (route) => route.fulfill({ json: { items: [] } }))
  await page.goto(`${BASE}/`, { waitUntil: 'domcontentloaded' })
  await until(() => page.getByRole('heading', { name: 'Trending now', exact: true }).isVisible())
  check((await page.getByRole('heading', { name: /^(Sequels you missed|For you)$/ }).count()) === 0, 'with nothing to show, neither rail is there')
  await page.close()

  // Links: an AniList one opens its show straight away, a MyAnimeList one by asking which show it is.
  page = await fresh()
  const asked = []
  await page.route('**/api/anime/from-mal*', (route) => {
    const id = new URL(route.request().url()).searchParams.get('id')
    asked.push(id)
    return id === '99'
      ? route.fulfill({ json: { id: ANIME } })
      : route.fulfill({ status: 404, json: { error: "AniList doesn't know that MyAnimeList show." } })
  })
  await page.goto(`${BASE}/open?url=${encodeURIComponent(`https://anilist.co/anime/${ANIME}/Some-Show`)}`)
  check(await until(async () => path(page) === `/anime/${ANIME}`), 'an AniList link opens its show', path(page))
  await page.goto(`${BASE}/open?text=${encodeURIComponent('watch this https://myanimelist.net/anime/99/Show')}`)
  check(await until(async () => path(page) === `/anime/${ANIME}`), 'a shared MyAnimeList link, words and all, opens its show', path(page))
  check(asked[0] === '99', 'asking by its MyAnimeList id', asked.join(','))
  await page.goto(`${BASE}/open?url=${encodeURIComponent('https://myanimelist.net/anime/1/Unknown')}`)
  check(await until(() => page.getByText("AniList doesn't know that MyAnimeList show.").isVisible()), 'one nobody knows says so')
  check(await page.getByRole('link', { name: 'Search for it instead' }).isVisible(), 'with a search to try instead')
  await page.goto(`${BASE}/open?text=frieren`)
  check(await until(async () => path(page) === '/browse?q=frieren'), 'words without a link are searched for', path(page))

  const search = page.getByRole('combobox', { name: 'Search anime' })
  await search.fill(`https://anilist.co/anime/${ANIME}`)
  check(await until(() => page.getByRole('option', { name: 'Open this AniList link' }).isVisible()), 'a link typed in the search box offers to open it')
  await search.press('Enter')
  check(await until(async () => path(page) === `/anime/${ANIME}`), 'and Enter opens the show', path(page))
  await page.close()

  // Episode scores: the server sends them only for watched episodes; such a row shows its score and thread.
  page = await fresh()
  const forum = 'https://myanimelist.net/forum/?topicid=1'
  await page.route('**/api/episodes?*', async (route) => {
    const res = await route.fetch()
    const body = await res.json()
    if (body.items?.[0]) Object.assign(body.items[0], { watched: true, score: 4.31, forumUrl: forum })
    if (body.items?.[1]) Object.assign(body.items[1], { watched: false, score: undefined, forumUrl: undefined })
    return route.fulfill({ response: res, json: body })
  })
  await page.goto(`${BASE}/anime/${ANIME}`, { waitUntil: 'domcontentloaded' })
  const discuss = page.getByRole('link', { name: 'Discuss episode 1 on MyAnimeList' })
  check(await until(() => discuss.isVisible()), 'a watched episode has a Discussion button')
  check((await discuss.getAttribute('href')) === forum && (await discuss.getAttribute('target')) === '_blank',
    'opening its MyAnimeList thread in a new tab', await discuss.getAttribute('href'))
  check(await page.getByText('★ 4.3').first().isVisible(), 'and its score, out of 5')
  check(await page.getByRole('link', { name: /^Episode 1\b.*rated 4\.3 of 5/ }).count() === 1, 'read out with the row')
  check((await page.getByRole('link', { name: 'Discuss episode 2 on MyAnimeList' }).count()) === 0, 'an unwatched one has neither')
  await page.close()

  // AniList by pasting a code: Connect opens AniList's page, the code goes back, and a wrong one says why.
  page = await fresh()
  let connected = false
  const tokenUrl = `${BASE}/anilist-authorize-stand-in`
  await page.route('**/api/trackers', async (route) => {
    const res = await route.fetch()
    const body = await res.json()
    const anilist = body.trackers.find((t) => t.provider === 'anilist')
    Object.assign(anilist, { configured: false, clientId: '', tokenUrl, connected, user: connected ? 'tony' : undefined })
    return route.fulfill({ response: res, json: body })
  })
  const sent = []
  await page.route('**/api/auth/token', (route) => {
    sent.push(route.request().postDataJSON())
    if (sent.length === 1) {
      return route.fulfill({ status: 400, json: { error: "AniList didn't accept that code. Ask for a new one and paste it again." } })
    }
    connected = true
    return route.fulfill({ json: { connected: true, user: 'tony' } })
  })
  await page.goto(`${BASE}/settings?tab=Accounts`, { waitUntil: 'domcontentloaded' })
  const card = page.locator('section', { has: page.getByRole('heading', { name: 'AniList', exact: true }) })
  const connect = card.getByRole('link', { name: 'Connect', exact: true })
  check(await until(() => connect.isVisible()), 'AniList offers Connect with no keys of your own')
  check(await card.getByText('Not connected', { exact: true }).isVisible(), 'saying it is not connected, not "not set up"')
  check((await card.getByLabel('Client ID').count()) === 0, 'the keys form stays closed')
  check(await card.getByRole('button', { name: 'Use my own client' }).isVisible(), 'own keys are still a choice')

  const [popup] = await Promise.all([page.waitForEvent('popup'), connect.click()])
  check(popup.url().startsWith(tokenUrl), "Connect opens AniList's page in a new tab", popup.url())
  await popup.close()
  const code = card.getByLabel('Code from AniList')
  check(await until(() => code.isVisible()), 'and asks for the code it shows')
  const send = card.getByRole('button', { name: 'Connect', exact: true })
  check(await send.isDisabled(), 'Connect waits for a code')
  await code.fill('nope')
  await send.click()
  check(await until(() => card.getByText(/AniList didn't accept that code/).isVisible()), 'a code AniList refuses says what to do')
  check(sent[0]?.token === 'nope', 'the code goes as pasted', JSON.stringify(sent[0]))
  await code.fill('eyJ.good.code')
  await send.click()
  check(await until(() => card.getByText('Connected as tony').isVisible()), 'a right one connects, naming the account')
  check((await card.getByLabel('Code from AniList').count()) === 0, 'and the field goes')
  check(await card.getByRole('link', { name: 'Reconnect', exact: true }).isVisible(), 'Reconnect takes its place')
  await card.getByRole('button', { name: 'Use my own client' }).click()
  check(await until(() => card.getByLabel('Client ID').isVisible()), 'Use my own client opens the keys form')
  await page.close()

  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
