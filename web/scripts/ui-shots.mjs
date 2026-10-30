import { chromium } from 'playwright'
import { mkdirSync } from 'node:fs'
import { join } from 'node:path'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const OUT = process.env.UI_SHOTS ?? process.env.SHOTS ?? '.'
const PREFIX = process.env.PREFIX ?? 'shot'
mkdirSync(OUT, { recursive: true })

const api = async (path, init) => {
  const res = await fetch(BASE + path, { ...init, headers: { 'content-type': 'application/json' } })
  return res.json().catch(() => undefined)
}
const post = (path, body) => api(path, { method: 'POST', body: JSON.stringify(body) })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const trending = (await api('/api/discover?sort=trending&perPage=12'))?.items ?? []
const shows = trending.filter((s) => !s.isAdult)
let listed = 0
for (const show of shows) {
  if (listed === 3) break
  const episode = show.nextEpisode ? show.nextEpisode - 1 : Math.min(show.episodes ?? 1, 3)
  if (episode < 1) continue
  await api(`/api/anime/${show.id}`)
  await post('/api/upnext', { animeId: show.id, episode })
  await post('/api/status', { animeId: show.id, status: listed === 2 ? 'PLANNING' : 'CURRENT' })
  listed++
}
const show = shows.find((s) => s.nextEpisode > 3) ?? shows[0]

const watched = [
  [0, 1, 1420],
  [0, 2, 1420],
  [0, 3, 610],
  [1, 1, 1420],
  [2, 4, 0],
]
for (const [i, episode, position] of watched) {
  if (!shows[i]) continue
  await post('/api/progress', { animeId: shows[i].id, episode, position, duration: 1420, played: position, final: true })
  await sleep(1100)
}

const browser = await chromium.launch()
const sized = process.env.SIZES?.split(',').map((s) => {
  const [width, height] = s.split('x').map(Number)
  const touch = width < 1024
  return { name: `${width}`, viewport: { width, height }, deviceScaleFactor: 1, isMobile: width < 640, hasTouch: touch }
})
const views = sized ?? [
  { name: 'desktop', viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 },
  { name: 'phone', viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, isMobile: true, hasTouch: true },
  { name: 'wide', viewport: { width: 2000, height: 1000 }, deviceScaleFactor: 1, only: ['home'] },
]
const overflow = () =>
  page.evaluate(() => {
    const width = document.documentElement.clientWidth
    const scrolls = (el) => {
      for (let p = el.parentElement; p; p = p.parentElement) {
        const s = getComputedStyle(p)
        if (/(auto|scroll)/.test(s.overflowX) && p !== document.body && p !== document.documentElement) return true
      }
      return false
    }
    const out = []
    for (const el of document.querySelectorAll('body *')) {
      const box = el.getBoundingClientRect()
      if (box.width === 0 || box.height === 0) continue
      if (box.right > width + 1 || box.left < -1) {
        if (scrolls(el) || getComputedStyle(el).position === 'fixed') continue
        out.push(`${el.tagName.toLowerCase()}.${String(el.className).split(' ').slice(0, 3).join('.')} ${Math.round(box.left)}..${Math.round(box.right)}`)
      }
    }
    return { page: document.documentElement.scrollWidth - width, out: out.slice(0, 6) }
  })
let page
const all = [
  { name: 'home', path: '/', full: true },
  { name: 'schedule', path: '/schedule', full: false },
  { name: 'show', path: `/anime/${show?.id}`, full: true },
  { name: 'watch', path: `/watch/${show?.id}/${Math.max(1, (show?.nextEpisode ?? 2) - 1)}`, full: false },
  { name: 'browse', path: '/browse', full: false },
  { name: 'recent', path: '/recent', full: false },
  { name: 'library', path: '/library', full: false },
  { name: 'downloads', path: '/downloads', full: false },
  { name: 'saved', path: '/saved', full: false },
  { name: 'history', path: '/history', full: false },
  { name: 'settings', path: '/settings', full: false },
  { name: 'settings-playback', path: '/settings?tab=Playback', full: false },
  { name: 'notifications', path: '/notifications', full: false },
  { name: 'you', path: '/you', full: false },
  { name: 'search-typed', path: '/browse?q=frieren', full: false },
]
const only = process.env.PAGES?.split(',')
const pages = only ? all.filter((p) => only.includes(p.name)) : all
for (const { only: viewOnly, ...view } of views) {
  const context = await browser.newContext(view)
  page = await context.newPage()
  for (const p of pages.filter((p) => !viewOnly || viewOnly.includes(p.name))) {
    await page.goto(BASE + p.path, { waitUntil: 'domcontentloaded' })
    await page.waitForLoadState('networkidle', { timeout: 20_000 }).catch(() => {})
    await sleep(2500)
    const file = join(OUT, `${PREFIX}-${p.name}-${view.name}.png`)
    await page.screenshot({ path: file, fullPage: (p.full || process.env.FULL === '1') && !viewOnly })
    console.log(`ok   ${file}`)
    const spill = await overflow()
    if (spill.page > 0 || spill.out.length > 0) console.log(`WARN ${p.name} ${view.name} page +${spill.page}px: ${spill.out.join(' | ')}`)
    if (p.name === 'home') {
      const box = page.getByRole('combobox', { name: 'Search anime' })
      if (await box.isVisible().catch(() => false)) {
        await box.fill('frieren')
        await sleep(3000)
        const searched = join(OUT, `${PREFIX}-search-${view.name}.png`)
        await page.screenshot({ path: searched })
        console.log(`ok   ${searched}`)
        await box.fill('')
        await page.keyboard.press('Escape')
      }
    }
    const more = page.getByRole('button', { name: /^More for episode / }).first()
    if (p.name === 'show' && (await more.isVisible().catch(() => false))) {
      await more.scrollIntoViewIfNeeded()
      await more.click()
      await sleep(500)
      const menu = join(OUT, `${PREFIX}-show-menu-${view.name}.png`)
      await page.screenshot({ path: menu })
      console.log(`ok   ${menu}`)
      await page.keyboard.press('Escape')
    }
    const week = page.getByRole('tab', { name: 'Week', exact: true })
    if (p.name === 'schedule' && (await week.isVisible().catch(() => false))) {
      await week.click()
      await sleep(1500)
      const weekly = join(OUT, `${PREFIX}-schedule-week-${view.name}.png`)
      await page.screenshot({ path: weekly })
      console.log(`ok   ${weekly}`)
      await page.getByRole('tab', { name: 'Day', exact: true }).click()
    }
    const asList = page.getByRole('group', { name: 'Show as' }).getByRole('button', { name: 'List' })
    if (p.name === 'library' && (await asList.isVisible().catch(() => false))) {
      await asList.click()
      await sleep(1200)
      const listed = join(OUT, `${PREFIX}-library-list-${view.name}.png`)
      await page.screenshot({ path: listed })
      console.log(`ok   ${listed}`)
      const edit = page.getByRole('button', { name: /^Edit / }).first()
      if (await edit.isVisible().catch(() => false)) {
        await edit.click()
        await sleep(700)
        const editing = join(OUT, `${PREFIX}-library-edit-${view.name}.png`)
        await page.screenshot({ path: editing })
        console.log(`ok   ${editing}`)
        await page.keyboard.press('Escape')
      }
      await page.getByRole('group', { name: 'Show as' }).getByRole('button', { name: 'Grid' }).click()
    }
    const focus = page.getByRole('button', { name: 'Focus', exact: true })
    if (p.name === 'watch' && (await focus.isVisible().catch(() => false))) {
      await focus.click()
      await sleep(800)
      const focused = join(OUT, `${PREFIX}-focus-${view.name}.png`)
      await page.screenshot({ path: focused })
      console.log(`ok   ${focused}`)
    }
  }
  await context.close()
}
await browser.close()
console.log('\nall passed')
