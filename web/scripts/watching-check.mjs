// Watching flow: "Still watching?", the sleep timer, hidden spoilers, and the cards at a show's last aired
// episode. AniList's answers (episodes, seasons, recommendations) are fed where needed.
import { copyFileSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const LIB = process.env.YORU_LIB
const SHOTS = process.env.SHOTS ?? '.'
const ANIME = Number(process.env.YORU_ANIME ?? 127230)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 10_000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
  }
  return false
}
const api = async (path, body) =>
  (await fetch(`${BASE}${path}`, body ? { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(body) } : {})).json().catch(() => ({}))

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

// Five playable episodes from the test file.
await api('/api/local/paths', { paths: [LIB] })
for (let n = 2; n <= 5; n++) copyFileSync(join(LIB, 'Yoru Test Show - 01.mkv'), join(LIB, `copy-${n}.mkv`))
await api('/api/local/scan', {})
const files = async () => (await api('/api/local/files')).items ?? []
await until(async () => (await files()).filter((f) => /copy-\d|Test Show - 01/.test(f.path ?? '')).length >= 5, 40_000)
for (const f of await files()) {
  const n = /copy-(\d)/.exec(f.path ?? '')?.[1] ?? (/Test Show - 01/.test(f.path ?? '') ? '1' : null)
  if (n) await api('/api/local/assign', { id: f.id, animeId: ANIME, episode: Number(n) })
}
await api('/api/prefs', { key: 'playback.autoplay', value: 'true' })
await api('/api/prefs', { key: 'playback.autonext', value: 'true' })
await api('/api/prefs', { key: 'playback.still_watching', value: '2' })

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 860 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const video = page.locator('video')
const playing = () => until(() => video.evaluate((v) => !v.paused && v.currentTime > 0), 60_000)
// Run the episode out without a key or pointer: nobody is touching anything.
const runOut = () =>
  video.evaluate((v) => {
    v.currentTime = v.duration - 0.6
    void v.play()
  })
const at = (ep, anime = ANIME) => until(() => page.url().endsWith(`/watch/${anime}/${ep}`), 30_000)

// "Still watching?": two episodes auto next started untouched, then it asks instead of counting down.
await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
await playing()
// A progress report in the episode's last minutes readies the next one ahead.
const log = () => (process.env.YORU_LOG ? readFileSync(process.env.YORU_LOG, 'utf8') : '')
check(await until(() => /next episode readied/.test(log()), 30_000), 'episode 2 is readied near the end of 1')
await runOut()
check(await at(2), 'auto next plays episode 2')
await playing()
await runOut()
check(await at(3), 'and episode 3')
await playing()
await runOut()
const still = page.getByRole('dialog', { name: 'Still watching?' })
check(await until(() => still.isVisible(), 20_000), 'after two untouched episodes it asks if anyone is still watching')
await sleep(7000)
check(page.url().endsWith(`/watch/${ANIME}/3`), 'and does not count down to the next')
check(await still.getByText('Paused after 2 episodes').isVisible(), 'saying how many', await still.innerText())
await page.screenshot({ path: `${SHOTS}/watching-still.png` })
const unmarked = page.waitForRequest((r) => r.url().endsWith('/api/watched') && r.postDataJSON()?.watched === false)
await still.getByRole('button', { name: /Unmark episodes 2–3/ }).click()
check((await unmarked).postDataJSON().episode === 2, 'Unmark takes back the run from its first episode')
check(await until(() => still.getByText('Unmarked').isVisible()), 'and says so')
await still.getByRole('button', { name: 'Keep watching' }).click()
check(await at(4), 'Keep watching plays on')
await playing()

// The sleep timer, set to the end of this episode: it ends there, the card saying why there's no countdown.
const sleepButton = page.getByRole('button', { name: /^Player settings/ })
const sleepRow = page.getByRole('menuitem', { name: /^Sleep timer/ })
const pickSleep = async (name) => {
  await page.mouse.move(640, 400)
  await sleepButton.click()
  await sleepRow.click()
  await page.getByRole('menuitemradio', { name }).click()
}
await pickSleep('End of this episode')
check(await until(async () => /end of this episode/.test(await sleepButton.getAttribute('aria-label'))), 'the sleep timer is set')
await pickSleep('15 minutes')
check(await until(async () => /15 min left/.test(await sleepButton.getAttribute('aria-label'))), 'minutes left on its button')
await page.mouse.move(640, 400)
await sleepButton.click()
check(/15 min left/.test(await sleepRow.innerText()), 'and on its row in the menu', await sleepRow.innerText())
await page.screenshot({ path: `${SHOTS}/watching-menu.png` })
await page.keyboard.press('Escape')
await pickSleep('End of this episode')
await runOut()
const upNext = page.getByRole('dialog', { name: 'Up next' })
check(await until(() => upNext.getByText('The sleep timer stopped auto next.').isVisible(), 20_000), 'at the end the card says the timer stopped auto next')
await sleep(7000)
check(page.url().endsWith(`/watch/${ANIME}/4`), 'and stays on the episode')
await page.screenshot({ path: `${SHOTS}/watching-sleep.png` })
await upNext.getByRole('button', { name: 'Play now' }).click()
check(await at(5), 'Play now goes on by hand')

// Spoilers on the show page: past the next episode, no title, synopsis or clear still until the eye is pressed.
const ep = (n, more = {}) => ({
  animeId: ANIME, epKey: String(n), number: n, display: n, titleEn: `Title ${n}`, overview: `What happens in ${n}`,
  still: 'data:image/gif;base64,R0lGODlhAQABAIAAAP///wAAACH5BAEAAAAALAAAAAABAAEAAAICRAEAOw==',
  recap: false, watched: false, onDisk: false, local: true, ...more,
})
const episodes = [ep(1, { watched: true }), ep(2), ep(3, { spoiler: true }), ep(4, { spoiler: true }), ep(5, { spoiler: true })]
await page.route(`**/api/episodes?id=${ANIME}`, (r) => r.fulfill({ json: { items: episodes, count: episodes.length } }))
await page.goto(`${BASE}/anime/${ANIME}`, { waitUntil: 'domcontentloaded' })
const row = (n) => page.locator('li', { has: page.getByRole('link', { name: new RegExp(`^Episode ${n}\\b`) }) })
check(await until(() => row(2).getByText('Title 2').isVisible(), 20_000), 'the next episode shows its title')
check(!(await page.getByText('Title 3').isVisible()) && (await row(3).getByText('Title hidden').isVisible()), 'a later one says its title is hidden')
check(!(await page.getByText('What happens in 4').isVisible()), 'and no synopsis')
await page.screenshot({ path: `${SHOTS}/watching-spoilers.png` })
await row(3).getByRole('button', { name: 'More for episode 3' }).click()
await page.getByRole('menuitem', { name: 'Show title and synopsis' }).click()
check(await until(() => page.getByText('Title 3').isVisible()), 'its menu shows that one')
check(!(await page.getByText('Title 4').isVisible()), 'and only that one')
await row(3).getByRole('button', { name: 'More for episode 3' }).click()
await page.getByRole('menuitem', { name: 'Hide title and synopsis' }).click()
check(await until(async () => !(await page.getByText('Title 3').isVisible())), 'and hides it again')
const showAll = page.waitForRequest((r) => r.url().endsWith('/api/prefs') && r.postDataJSON()?.key === 'display.spoilers')
await page.getByRole('button', { name: 'Show all titles' }).click()
const sent = (await showAll).postDataJSON()
check(sent.value === 'off' && sent.animeId === ANIME, 'Show all titles turns it off for this show alone', JSON.stringify(sent))
await page.unroute(`**/api/episodes?id=${ANIME}`)

// The last aired episode: the cards for a season to come, one out already, a show still airing, and the end.
const lastAired = [ep(4, { watched: true }), ep(5)]
await page.route(`**/api/episodes?id=${ANIME}`, (r) => r.fulfill({ json: { items: lastAired, count: 2 } }))
const season = (status) => ({ id: 990001, ordinal: 2, romaji: 'Show Season 2', english: null, year: 2027, status, progress: 0, onList: false })
let franchise = [{ id: ANIME, ordinal: 1, romaji: 'Show', progress: 4, onList: true, status: 'FINISHED' }, season('NOT_YET_RELEASED')]
await page.route('**/api/franchise?*', (r) => r.fulfill({ json: { rootId: ANIME, seasons: franchise, related: [] } }))
await page.route('**/api/score', (r) => r.fulfill({ json: { animeId: ANIME, score: 80 } }))
// Episode 5 is the entry's last by its count, as the server's play order reads a finale.
await page.route(`**/api/anime/${ANIME}`, async (r) => {
  const res = await r.fetch()
  return r.fulfill({ response: res, json: { ...(await res.json()), episodes: 5 } })
})
await page.goto(`${BASE}/watch/${ANIME}/5`, { waitUntil: 'domcontentloaded' })
await playing()
await runOut()
const finale = page.getByRole('dialog', { name: 'Season finale' })
check(await until(() => finale.isVisible(), 20_000), 'the finale names the season to come')
check(await finale.getByText('Show Season 2 · coming 2027').isVisible(), 'with when', await finale.innerText().catch(() => ''))
check(await finale.getByRole('button', { name: 'Add to Planning' }).isVisible(), 'offering Plan to watch')
check(await finale.getByRole('button', { name: "Alert me when it's out" }).isVisible(), 'and an alert')
const scored = page.waitForRequest((r) => r.url().endsWith('/api/score'))
await finale.getByRole('button', { name: '8', exact: true }).click()
check((await scored).postDataJSON().score === 80, 'a score from the card')
check(await until(async () => (await finale.getByRole('button', { name: '8', exact: true }).getAttribute('aria-pressed')) === 'true'), 'shown as chosen')
await page.screenshot({ path: `${SHOTS}/watching-finale.png` })

franchise = [franchise[0], season('RELEASING')]
await page.goto(`${BASE}/watch/${ANIME}/5`, { waitUntil: 'domcontentloaded' })
await playing()
await runOut()
check(await until(() => upNext.getByText('Next season', { exact: true }).isVisible(), 20_000), 'a season out already is up next')
check(await page.getByText('Show Season 2 · Episode 1').isVisible(), 'its first episode')
await page.screenshot({ path: `${SHOTS}/watching-next-season.png` })
check(await at(1, 990001), 'counting down into it')

franchise = [franchise[0]]
await page.route('**/api/recommend?*', (r) =>
  r.fulfill({ json: { source: 'anilist', items: [1, 2, 3, 4].map((i) => ({ id: 880000 + i, title: `Like ${i}`, romaji: `Like ${i}`, cover: lastAired[0].still })) } }))
await page.goto(`${BASE}/watch/${ANIME}/5`, { waitUntil: 'domcontentloaded' })
await playing()
await runOut()
const end = page.getByRole('dialog', { name: 'Season finale' })
check(await until(() => end.getByText("You've seen every episode.").isVisible(), 20_000), 'with nothing to come, the end')
check(await end.getByText('More like this').isVisible() && (await end.getByRole('link').count()) === 3, 'and three shows like it')
await page.screenshot({ path: `${SHOTS}/watching-the-end.png` })

const due = Math.floor(Date.now() / 1000) + 3 * 86400
await page.unroute(`**/api/episodes?id=${ANIME}`)
await page.route(`**/api/episodes?id=${ANIME}`, (r) => r.fulfill({ json: { items: [...lastAired, ep(6, { airDate: due })], count: 3 } }))
await page.goto(`${BASE}/watch/${ANIME}/5`, { waitUntil: 'domcontentloaded' })
await playing()
await runOut()
const later = page.getByRole('dialog', { name: 'Next episode' })
check(await until(() => later.getByText(/Episode 6 airs in/).isVisible(), 20_000), 'still airing, it says when the next one airs', await later.innerText().catch(() => ''))
const outButton = page.getByRole('button', { name: /^Episode 6 · out / })
check(await outButton.isVisible(), 'the page names the day episode 6 is out', await outButton.innerText().catch(() => ''))
await outButton.click({ timeout: 5000 }).catch(() => {})
const notOut = page.getByRole('dialog', { name: 'Not out yet' })
check(
  (await until(() => notOut.getByRole('button', { name: "Download it when it's out" }).isVisible())) &&
    ((await notOut.getByRole('button', { name: "Tell me when it's out" }).isVisible()) ||
      (await notOut.getByText('Alerts are on for this show.').isVisible())),
  'and its popover offers an alert and a download for when it is out',
  await notOut.innerText().catch(() => ''),
)

check(errors.length === 0, 'no page errors', errors.join(' | '))
await browser.close()
console.log(failures === 0 ? '\nall passed' : `\n${failures} FAILED`)
process.exit(failures === 0 ? 0 : 1)
