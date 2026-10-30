// The player's toggles change the value they show: the per-show one, not the global.
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const LIB = process.env.YORU_LIB
const ANIME = Number(process.env.YORU_ANIME ?? 127230)

const api = async (path, init) => {
  const res = await fetch(BASE + path, {
    ...init,
    headers: { 'content-type': 'application/json', ...(init?.headers ?? {}) },
  })
  let body
  try {
    body = await res.json()
  } catch {
    body = undefined
  }
  return { ok: res.ok, status: res.status, body }
}
const post = (path, body) => api(path, { method: 'POST', body: JSON.stringify(body) })

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 8000) => {
  const end = Date.now() + ms
  while (Date.now() < end) {
    if (await fn()) return true
    await sleep(150)
  }
  return false
}

for (let i = 0; i < 60; i++) {
  try {
    if ((await api('/api/setup')).ok) break
  } catch {}
  await sleep(1000)
}
await post('/api/local/paths', { paths: [LIB] })
await post('/api/local/scan', {})
let file
for (let i = 0; i < 30 && !file; i++) {
  await sleep(1000)
  const files = await api('/api/local/files')
  file = (files.body?.items ?? []).find((f) => String(f.path ?? '').includes('Yoru Test Show'))
}
check(!!file, 'test file listed')
if (!file) process.exit(1)
await post('/api/local/assign', { id: file.id, animeId: ANIME, episode: 1 })

// What the user set globally, in Settings.
await post('/api/prefs', { key: 'playback.autoplay', value: 'true' })
await post('/api/prefs', { key: 'playback.autonext', value: 'true' })
await post('/api/prefs', { key: 'playback.autoskip_op', value: 'false' })
// A per-show override, which only this show carries.
await post('/api/prefs', { key: 'playback.autoskip_op', value: 'true', animeId: ANIME })
const served = (await api(`/api/prefs?anime=${ANIME}`)).body
check(
  served?.effective?.['playback.autoplay'] === 'true' && served?.effective?.['playback.autonext'] === 'true',
  'the server applies the global settings to this show',
  JSON.stringify({ effective: served?.effective?.['playback.autoplay'], overrides: served?.overrides }),
)

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
await page.goto(`${BASE}/watch/${ANIME}/1`, { waitUntil: 'domcontentloaded' })
check(await until(() => page.getByRole('region', { name: 'Video player' }).isVisible(), 30_000), 'player rendered')
await page.getByRole('button', { name: 'Audio & subtitles', exact: true }).last().click()
check(await until(() => page.getByRole('switch', { name: 'Start playing when opened' }).isVisible()), 'player controls rendered')

const state = async (name) =>
  page.getByRole('switch', { name }).getAttribute('aria-checked')
const skipOp = page.getByRole('radiogroup', { name: 'Skip openings' })
const skipState = async () => skipOp.getByRole('radio', { checked: true }).textContent()

const shown = {
  'Start playing when opened': await state('Start playing when opened'),
  'Play the next episode by itself': await state('Play the next episode by itself'),
  'Skip openings': await skipState(),
}
console.log('   shown:', JSON.stringify(shown))

check(shown['Start playing when opened'] === 'true', 'Start playing when opened shows the setting the user turned on', shown['Start playing when opened'])
check(shown['Play the next episode by itself'] === 'true', 'Play the next episode by itself shows the setting the user turned on', shown['Play the next episode by itself'])
// What is in effect for this show, which is what the episode will actually do.
check(shown['Skip openings'] === 'Auto', 'Skip openings shows the override in effect', shown['Skip openings'])

await skipOp.getByRole('radio', { name: 'Off' }).click()
check(
  await until(async () => (await skipState()) === 'Off'),
  'choosing Off for a setting backed by an override shows Off',
  await skipState(),
)
check(
  (await api(`/api/prefs${'?anime=' + ANIME}`)).body?.effective?.['playback.autoskip_op'] === 'off',
  'the override is what changed',
)
check(
  (await api('/api/prefs')).body?.effective?.['playback.autoskip_op'] === 'false',
  'the global setting was left alone',
)

// A key with no override still edits the global, the default for every other show.
await page.getByRole('switch', { name: 'Start playing when opened' }).click()
check(
  await until(async () => (await state('Start playing when opened')) === 'false'),
  'a switch with no override still flips',
)
check(
  (await api('/api/prefs')).body?.effective?.['playback.autoplay'] === 'false',
  'with no override the global setting is what changed',
)

await browser.close()
console.log(failures === 0 ? '\nall checks passed' : `\n${failures} check(s) failed`)
process.exit(failures === 0 ? 0 : 1)
