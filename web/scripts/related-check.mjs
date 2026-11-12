import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
const ANIME = Number(process.env.YORU_RELATED ?? 235)

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const until = async (fn, ms = 30000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (await Promise.resolve().then(fn).catch(() => false)) return true
    await new Promise((r) => setTimeout(r, 500))
  }
  return false
}

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 160)))

await page.goto(`${BASE}/anime/${ANIME}`, { waitUntil: 'domcontentloaded' })
check(await until(() => page.getByRole('heading', { level: 1 }).isVisible()), 'the series page loads')

const api = await (await fetch(`${BASE}/api/franchise?id=${ANIME}`)).json()
const total = (api.related ?? []).length
const movies = (api.related ?? []).filter((e) => e.format === 'MOVIE').length
check(total > 10 && movies > 1, 'the API returns the franchise films', `${total} related, ${movies} movies`)

const related = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Related', exact: true }) })
check(await until(() => related.isVisible()), 'the related material has one rail')
const episodes = page.locator('section', { has: page.getByRole('heading', { name: 'Episodes' }) })
const [relBox, epBox] = [await related.boundingBox(), await episodes.boundingBox().catch(() => null)]
check(!!relBox && (!epBox || relBox.y < epBox.y), 'it sits above the episodes, where it is seen', `${relBox?.y} vs ${epBox?.y}`)

const chips = related.locator('a[href^="/anime/"]')
check(await until(async () => (await chips.count()) === total), 'All lists every entry, none hidden behind a button', `${await chips.count()} of ${total}`)
const texts = await chips.allInnerTexts()
const undated = texts.filter((t) => !/\b(19|20)\d{2}\b/.test(t))
check(undated.length === 0, 'every entry shows its year', undated.slice(0, 3).map((t) => t.replace(/\s+/g, ' ')).join(' | '))
const noEpisodes = texts.filter((t) => !/\d+ eps?\b/.test(t))
check(noEpisodes.length === 0, 'every entry shows its episode count', noEpisodes.slice(0, 3).map((t) => t.replace(/\s+/g, ' ')).join(' | '))

const filters = related.getByRole('group', { name: 'Show only' })
check(await filters.isVisible(), 'its kinds are buttons in the heading')
await filters.getByRole('button', { name: /^Movies/ }).click()
check(await until(async () => (await chips.count()) === movies), 'Movies narrows it to the films', `${await chips.count()} of ${movies}`)
await page.screenshot({ path: `${SHOTS}/series-related-movies.png` })
await filters.getByRole('button', { name: /^All/ }).click()
check(await until(async () => (await chips.count()) === total), 'All brings everything back')

const pager = page.getByRole('navigation', { name: 'Episode pages', exact: true })
const find = page.getByRole('searchbox', { name: 'Search episodes' }).or(page.getByRole('textbox', { name: 'Search episodes' })).first()
if (await until(() => pager.isVisible(), 20000)) {
  const [p, f, first] = [await pager.boundingBox(), await find.boundingBox().catch(() => null), await episodes.locator('li').first().boundingBox()]
  check(!!p && !!f && Math.abs(p.y + p.height / 2 - (f.y + f.height / 2)) < 8, 'the page buttons share a row with Find an episode', `${p?.y} vs ${f?.y}`)
  check(!!p && !!first && p.y < first.y, 'above the list')
  await pager.scrollIntoViewIfNeeded()
  await page.screenshot({ path: `${SHOTS}/series-episode-toolbar.png` })
}

const seasons = page.locator('section', { has: page.getByRole('heading', { name: /^Seasons/ }) })
if (await seasons.isVisible().catch(() => false)) {
  check(!/movie|side story/i.test(await seasons.innerText()), 'the seasons rail holds no films')
}

const track = related.locator('[data-rail-track]')
const far = chips.nth(Math.min(total - 1, 14))
await far.scrollIntoViewIfNeeded()
await page.waitForTimeout(800)
const left = await track.evaluate((el) => el.scrollLeft)
const farHref = await far.getAttribute('href')
check(left > 0, 'the related rail scrolls to a film further in', String(left))
await far.click()
check(await until(() => page.url().endsWith(farHref)), 'that film opens', page.url())
const here = page.locator('[data-rail-track] [aria-current="page"]')
if (await until(() => here.isVisible(), 10000)) {
  const inView = await here.evaluate((el) => {
    const rail = el.closest('[data-rail-track]').getBoundingClientRect()
    const chip = el.getBoundingClientRect()
    return chip.left >= rail.left - 1 && chip.right <= rail.right + 1
  })
  check(inView, 'the row of its own series opens on the film you are on')
}
await page.goBack()
let back = -1
check(await until(async () => Math.abs((back = await track.evaluate((el) => el.scrollLeft)) - left) < 4, 15000), 'coming back, the rail is where it was left', `${back} vs ${left}`)

const LONG = Number(process.env.YORU_SEASONS ?? 918)
const long = await (await fetch(`${BASE}/api/franchise?id=${LONG}`)).json()
const last = (long.seasons ?? []).at(-1)
check((long.seasons ?? []).length > 5, 'a show with many seasons', String((long.seasons ?? []).length))
if (last) {
  await page.goto(`${BASE}/anime/${last.id}`, { waitUntil: 'domcontentloaded' })
  const mine = page.locator('[data-rail-track] [aria-current="page"]')
  check(await until(() => mine.isVisible(), 90000), 'its last season marks itself in the seasons rail')
  const placed = await mine.evaluate((el) => {
    const rail = el.closest('[data-rail-track]')
    const [box, chip] = [rail.getBoundingClientRect(), el.getBoundingClientRect()]
    return { scrolls: rail.scrollWidth > rail.clientWidth + 1, left: rail.scrollLeft, inView: chip.left >= box.left - 1 && chip.right <= box.right + 1 }
  })
  check(placed.scrolls && placed.left > 0 && placed.inView, 'and the rail opens on it, not at the start', JSON.stringify(placed))
  await page.screenshot({ path: `${SHOTS}/series-seasons-here.png` })
}

const cast = page.locator('section').filter({ has: page.getByRole('heading', { name: 'Cast' }) })
if (await until(() => cast.isVisible(), 25000)) {
  check(!/SUPPORTING/.test(await cast.innerText()), 'no shouted SUPPORTING label')
  const toggle = cast.getByRole('button', { name: /Show (all|more)/ })
  if (await toggle.isVisible().catch(() => false)) {
    await toggle.click()
    const less = cast.getByRole('button', { name: 'Show less' })
    check(await until(() => less.isVisible()), 'the cast toggle becomes Show less')
    check((await cast.getByRole('button', { name: /Show (all|more)/ }).count()) === 0, 'and does not sit beside its own opposite')
  }
  const [castBox, eps] = [await cast.boundingBox(), await episodes.boundingBox()]
  if (castBox && eps) check(castBox.y > eps.y, 'the cast sits below the episodes')
}

check(errors.length === 0, 'no page errors', errors.join(' | '))

await related.scrollIntoViewIfNeeded()
await page.screenshot({ path: `${SHOTS}/series-rails.png` })

await browser.close()
console.log(failures === 0 ? '\nall checks passed' : `\n${failures} check(s) failed`)
process.exit(failures === 0 ? 0 : 1)
