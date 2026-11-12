import { spawn, spawnSync } from 'node:child_process'
import { createHash, generateKeyPairSync, sign } from 'node:crypto'
import { createServer } from 'node:http'
import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { chromium } from 'playwright'

const REPO = process.env.YORU_REPO ?? join(process.cwd(), '..')
const RELEASED_TAG = process.env.YORU_TAG ?? '13c57fb9da3c328895d7b40e60c33a0da17b0dfa'
const PORT = 4398
const BASE = `http://127.0.0.1:${PORT}`
const OLD = '1.0.0'
const MID = '1.0.0.1'
const NEW = '1.0.1'
const NEWER = '1.0.2'
const NEWEST = '1.0.3'
const NOTES = {
  [NEWEST]: '- the newest one',
  [NEWER]: '- the next one',
  [NEW]: '## The big one\n- **Keep all** keeps every episode\n- a second line',
  [MID]: '- a fix in between',
  [OLD]: '- what the old one brought',
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const api = async (path, init) => {
  const res = await fetch(BASE + path, { ...init, headers: { 'content-type': 'application/json' } })
  const text = await res.text()
  let body
  try {
    body = JSON.parse(text)
  } catch {
    body = text
  }
  return { ok: res.ok, status: res.status, body }
}
const post = (path, body) => api(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined })

const scratch = join(tmpdir(), 'yoru-update-check')
rmSync(scratch, { recursive: true, force: true })
const root = join(scratch, 'app')
const data = join(scratch, 'data')
const release = join(scratch, 'release')
const oldTree = join(scratch, 'released-src')
for (const d of [root, data, release]) mkdirSync(d, { recursive: true })

const { publicKey, privateKey } = generateKeyPairSync('ed25519')
const trusted = Buffer.from(publicKey.export({ format: 'jwk' }).x, 'base64url').toString('hex')
const build = (dir, version, out) =>
  spawnSync(
    'go',
    ['build', '-trimpath', '-ldflags', `-s -w -H windowsgui -X yoru/internal/update.Version=${version} -X yoru/internal/update.signingKeyHex=${trusted}`,
      '-o', out, './cmd/yoru'],
    { cwd: dir, stdio: 'inherit', env: { ...process.env, CGO_ENABLED: '0' } },
  )

const added = spawnSync('git', ['worktree', 'add', '--detach', oldTree, RELEASED_TAG], { cwd: REPO, stdio: 'inherit' })
check(added.status === 0, `checked out Yoru ${RELEASED_TAG}`)
cpSync(join(REPO, 'web', 'dist'), join(oldTree, 'web', 'dist'), { recursive: true })
check(build(oldTree, OLD, join(root, 'yoru.exe')).status === 0, `built Yoru ${OLD} (the release)`)
check(build(REPO, NEW, join(release, 'new.exe')).status === 0, `built Yoru ${NEW}`)
check(build(REPO, NEWER, join(release, 'newer.exe')).status === 0, `built Yoru ${NEWER}`)
check(build(REPO, NEWEST, join(release, 'newest.exe')).status === 0, `built Yoru ${NEWEST}`)

const tar = join(process.env.SystemRoot ?? 'C:\\Windows', 'System32', 'tar.exe')
const pack = (version, binary) => {
  const dir = join(release, `yoru-${version}`)
  mkdirSync(dir, { recursive: true })
  cpSync(join(release, binary), join(dir, 'yoru.exe'))
  const asset = `yoru-${version}-update.zip`
  const zipped = spawnSync(tar, ['-a', '-c', '-f', join(release, asset), 'yoru.exe'], { cwd: dir, stdio: 'inherit' })
  check(zipped.status === 0, `${asset} packed`)
  const assets = { [asset]: readFileSync(join(release, asset)) }
  const sums = Object.entries(assets)
    .map(([name, zip]) => `${createHash('sha256').update(zip).digest('hex')}  ${name}\n`)
    .join('')
  return { version, assets, sums, signature: sign(null, Buffer.from(sums), privateKey) }
}
const releases = { [NEW]: pack(NEW, 'new.exe'), [NEWER]: pack(NEWER, 'newer.exe'), [NEWEST]: pack(NEWEST, 'newest.exe') }
let latest = releases[NEW]
let listed = [NEW, MID, OLD]
let limited = false

const gh = createServer((req, res) => {
  const url = req.url ?? ''
  const dl = (name) => `${ghBase()}/dl/${latest.version}/${name}`
  const file = (version, name) => {
    const rel = releases[version]
    const body = name === 'SHA256SUMS.txt' ? rel?.sums : name === 'SHA256SUMS.txt.sig' ? rel?.signature : rel?.assets[name]
    if (!body) {
      res.statusCode = 404
      return res.end()
    }
    res.setHeader('content-length', Buffer.byteLength(body))
    res.end(body)
  }
  if (limited && url.startsWith('/repos/')) {
    res.statusCode = 403
    res.setHeader('x-ratelimit-remaining', '0')
    res.setHeader('x-ratelimit-reset', String(Math.floor(Date.now() / 1000) + 1800))
    res.end('{"message":"API rate limit exceeded"}')
  } else if (/^\/repos\/Tons-7\/yoru\/releases\/latest/.test(url)) {
    res.setHeader('content-type', 'application/json')
    res.end(
      JSON.stringify({
        tag_name: `v${latest.version}`,
        body: NOTES[latest.version],
        assets: [
          ...Object.keys(latest.assets).map((name) => ({ name, browser_download_url: dl(name) })),
          { name: 'SHA256SUMS.txt', browser_download_url: dl('SHA256SUMS.txt') },
          { name: 'SHA256SUMS.txt.sig', browser_download_url: dl('SHA256SUMS.txt.sig') },
        ],
      }),
    )
  } else if (/^\/repos\/Tons-7\/yoru\/releases\?/.test(url)) {
    res.setHeader('content-type', 'application/json')
    res.end(
      JSON.stringify([
        { tag_name: 'v9.0.0', body: 'a draft', draft: true },
        { tag_name: 'v1.0.9', body: 'a beta', prerelease: true },
        ...listed.map((v) => ({ tag_name: `v${v}`, body: NOTES[v], published_at: '2026-10-16T10:00:00Z' })),
      ]),
    )
  } else if (url === '/Tons-7/yoru/releases.atom') {
    const html = (md) => md.split('\n').map((l) => (l.startsWith('## ') ? `<h2>${l.slice(3)}</h2>` : `<ul><li>${l.slice(2)}</li></ul>`))
      .join('\n').replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    const xml = (s) => s.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
    const entries = [...new Set([NEWER, ...listed])].map((v) =>
      `<entry><link rel="alternate" type="text/html" href="${ghBase()}/Tons-7/yoru/releases/tag/v${v}"/>` +
      `<content type="html">${xml(html(NOTES[v]))}</content></entry>`)
    res.setHeader('content-type', 'application/atom+xml')
    res.end(`<?xml version="1.0" encoding="UTF-8"?><feed xmlns="http://www.w3.org/2005/Atom">${entries.join('')}</feed>`)
  } else if (url === '/Tons-7/yoru/releases/latest') {
    res.statusCode = 302
    res.setHeader('location', `${ghBase()}/Tons-7/yoru/releases/tag/v${latest.version}`)
    res.end()
  } else if (url.startsWith('/Tons-7/yoru/releases/download/')) {
    const [tag, name] = url.split('/').slice(-2)
    file(tag.replace(/^v/, ''), name)
  } else if (url.startsWith('/dl/')) {
    const [, , version, name] = url.split('/')
    file(version, name)
  } else {
    res.statusCode = 404
    res.end()
  }
})
await new Promise((r) => gh.listen(0, '127.0.0.1', r))
const ghBase = () => `http://127.0.0.1:${gh.address().port}`

writeFileSync(join(root, 'config.toml'), `addr = "127.0.0.1:${PORT}"\n\n[torrent]\nlisten_port = 4343\nupnp = false\n`)
const env = {
  ...process.env,
  YORU_ROOT: root,
  LOCALAPPDATA: data,
  APPDATA: data,
  YORU_UPDATE_API: ghBase(),
  YORU_TOGETHER_LOCAL: '1',
  YORU_NO_WINDOW: '1',
}
const old = spawn(join(root, 'yoru.exe'), [], { env, cwd: root })
let oldLog = ''
old.stdout.on('data', (d) => (oldLog += d))
old.stderr.on('data', (d) => (oldLog += d))
let oldExit = null
old.on('exit', (code) => (oldExit = code))

const waitVersion = async (want, seconds) => {
  for (let i = 0; i < seconds * 4; i++) {
    try {
      const h = await api('/api/health')
      if (h.ok && h.body.version === want) return true
    } catch {}
    await sleep(250)
  }
  return false
}

check(await waitVersion(OLD, 30), `Yoru ${OLD} answering`)
const kept = await post('/api/prefs', { key: 'playback.autonext', value: 'true' })
check(kept.ok, 'a setting saved')

await sleep(500)
const checked = await post('/api/update/check')
check(checked.ok && checked.body.available && checked.body.latest?.version === NEW, 'the release sees the update', JSON.stringify(checked.body))
const versions = (list) => (list ?? []).map((n) => n.version).join(',')
check(versions(checked.body.pending) === `${NEW},${MID}`, 'every release since this one is offered, newest first', versions(checked.body.pending))

const applied = await post('/api/update/apply')
check(applied.status === 202, 'apply accepted', JSON.stringify(applied.body))

check(await waitVersion(NEW, 60), `Yoru ${NEW} answering on the same port`)
for (let i = 0; i < 40 && oldExit === null; i++) await sleep(250)
check(oldExit === 0, 'the release exited cleanly', `exit=${oldExit}`)

const yoruExe = join(root, 'yoru.exe')
for (let i = 0; i < 40 && existsSync(yoruExe + '.old'); i++) await sleep(250)
check(readFileSync(yoruExe).equals(readFileSync(join(release, 'new.exe'))), 'yoru.exe is the new program')
check(!existsSync(yoruExe + '.old') && !existsSync(yoruExe + '.new'), 'no update leftovers')
check(existsSync(join(data, 'yoru', 'yoru.db')), 'the database stayed where it was')
const prefs = (await api('/api/prefs')).body?.effective ?? {}
check(prefs['playback.autonext'] === 'true', 'the setting is still there')

const status = await api('/api/update')
check(status.ok && status.body.current === NEW && !status.body.available, 'Yoru sees itself up to date', JSON.stringify(status.body))
let after = status.body
for (let i = 0; i < 40 && !after.whatsNew?.length; i++) {
  await sleep(250)
  after = (await api('/api/update')).body
}
check(after.updatedFrom === OLD, 'Yoru knows what it replaced', after.updatedFrom)
check(versions(after.whatsNew) === `${NEW},${MID}`, "what's new covers both releases since the old one", versions(after.whatsNew))
const updated = ((await api('/api/notifications')).body.items ?? []).filter((n) => n.title === `Updated to Yoru ${NEW}`)
check(updated.length === 1, 'an "Updated to Yoru" notification is raised once')

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
const openAbout = async () => {
  await page.goto(`${BASE}/settings?tab=About`, { waitUntil: 'domcontentloaded' })
  await page.waitForURL(/\/setup/, { timeout: 5000 }).catch(() => {})
  await page.evaluate(() => {
    history.pushState(null, '', '/settings?tab=About')
    dispatchEvent(new PopStateEvent('popstate'))
  })
}
await openAbout()
const whatsNew = page.getByRole('heading', { name: `What's new since ${OLD}` })
const shown = await whatsNew.waitFor({ timeout: 15000 }).then(() => true, () => false)
check(shown, "the About tab says what's new since the old version")
if (shown) {
  const section = page.locator('section', { has: whatsNew })
  check((await section.locator('details').count()) === 2, 'one fold per release')
  check((await section.locator('details[open]').count()) === 1, 'the newest open, the older folded')
  check(await section.getByRole('heading', { name: 'The big one' }).isVisible(), 'a markdown heading is a heading')
  check((await section.locator('details[open] li').count()) === 2, 'bullets are a list, not raw dashes')
  check((await section.locator('details[open] strong', { hasText: 'Keep all' }).count()) === 1, 'bold is bold')
  check(!(await section.innerText()).includes('**'), 'no markdown left showing')
  check((await page.title()).includes('Yoru'), 'the window is called Yoru', await page.title())
}

latest = releases[NEWER]
listed = [NEWER, NEW, MID, OLD]
limited = true
const again = await post('/api/update/check')
check(again.ok && again.body.available && again.body.latest?.version === NEWER && !again.body.checkError,
  `with the API limited, Yoru still sees ${NEWER} from github.com's pages`, JSON.stringify(again.body))
check(again.body.auto === true, 'automatic updates are on from the start')

const waitStage = async (want, seconds) => {
  let body = {}
  for (let i = 0; i < seconds * 2; i++) {
    body = (await api('/api/update')).body ?? {}
    if (body.stage === want) break
    await sleep(500)
  }
  return body
}
const brief = (s) => JSON.stringify({ stage: s.stage, staged: s.staged, error: s.error, busy: s.busy })
const reloaded = async (seconds) => {
  for (let i = 0; i < seconds * 4; i++) {
    const still = await page.evaluate(() => window.__before === true).catch(() => true)
    if (!still) return true
    await sleep(250)
  }
  return false
}

const ready = await waitStage('ready', 60)
check(ready.stage === 'ready' && ready.staged === NEWER, `Yoru downloads ${NEWER} by itself`, brief(ready))
await sleep(3000)
check((await api('/api/health')).body.version === NEW, 'and waits for the restart to be asked for')
check(readFileSync(yoruExe).equals(readFileSync(join(release, 'new.exe'))), 'yoru.exe is untouched until then')

const room = await post('/api/together', { animeId: 7, episode: 1, infoHash: 'abc123' })
check(room.ok, 'a watch-together room opened', JSON.stringify(room.body))
check(((await api('/api/update')).body.busy ?? []).some((line) => line.includes('room')), 'Yoru knows a restart would close the room')

await openAbout()
const pill = page.locator('[data-update="ready"]')
check(await pill.waitFor({ timeout: 15000 }).then(() => true, () => false), 'the top bar offers the restart')
await pill.click()
const dialog = page.getByRole('alertdialog', { name: 'Restart Yoru to update?' })
check(await dialog.waitFor({ timeout: 5000 }).then(() => true, () => false), 'with a room open, the restart asks first')
check((await dialog.innerText().catch(() => '')).includes('room closes'), 'and says what it interrupts')
await dialog.getByRole('button', { name: 'Not now' }).click()
await sleep(1500)
check((await dialog.count()) === 0 && (await api('/api/health')).body.version === NEW, '"Not now" leaves Yoru running')
await pill.click()
await dialog.waitFor({ timeout: 5000 }).catch(() => {})
await page.mouse.click(12, 880)
await sleep(1200)
check((await dialog.count()) === 0 && (await api('/api/health')).body.version === NEW, 'a click outside closes it the same way')
check(await pill.isVisible(), 'and the button stays for later')
await page.evaluate(() => (window.__before = true))
await pill.click()
await dialog.getByRole('button', { name: 'Restart now' }).click()
check(await page.getByRole('alertdialog', { name: 'Updating Yoru' }).waitFor({ timeout: 10000 }).then(() => true, () => false),
  'the window says Yoru is updating')
check(await waitVersion(NEWER, 60), `Yoru ${NEWER} answering after the restart`)
check(await reloaded(30), 'the window reloads into the new version')
for (let i = 0; i < 40 && existsSync(yoruExe + '.old'); i++) await sleep(250)
check(readFileSync(yoruExe).equals(readFileSync(join(release, 'newer.exe'))), 'yoru.exe is the newer program')
check(!existsSync(yoruExe + '.old') && !existsSync(yoruExe + '.new'), 'no leftovers from the second update')
let second = {}
for (let i = 0; i < 40 && !second.whatsNew?.length; i++) {
  await sleep(250)
  second = (await api('/api/update')).body ?? {}
}
check(second.whatsNew?.[0]?.version === NEWER && second.whatsNew[0].notes === NOTES[NEWER],
  "with the API still limited, what's new reads the notes from the release feed", JSON.stringify(second.whatsNew))

latest = releases[NEWEST]
listed = [NEWEST, NEWER, NEW, MID, OLD]
const third = await post('/api/update/check')
check(third.ok && third.body.latest?.version === NEWEST, `Yoru sees ${NEWEST}`, JSON.stringify(third.body.latest))
const ready2 = await waitStage('ready', 60)
check(ready2.stage === 'ready' && ready2.staged === NEWEST && !ready2.busy?.length, `${NEWEST} is downloaded with nothing in the way`, brief(ready2))
await openAbout()
const restartHere = page.getByRole('button', { name: 'Restart to update', exact: true })
check(await restartHere.waitFor({ timeout: 15000 }).then(() => true, () => false), 'the About tab offers the restart too')
await restartHere.click()
check(!(await dialog.waitFor({ timeout: 1500 }).then(() => true, () => false)), 'with nothing running, there is no question')
check(await waitVersion(NEWEST, 60), `one click restarts into ${NEWEST}`)
for (let i = 0; i < 40 && existsSync(yoruExe + '.old'); i++) await sleep(250)
check(readFileSync(yoruExe).equals(readFileSync(join(release, 'newest.exe'))), 'yoru.exe is the newest program')
check(!existsSync(yoruExe + '.old') && !existsSync(yoruExe + '.new'), 'no leftovers from the third update')
await browser.close()

if (failures) console.log(oldLog.slice(-2000))

spawnSync('powershell', [
  '-NoProfile', '-Command',
  `Get-Process yoru -ErrorAction SilentlyContinue | Where-Object { $_.Path -like '${root}*' } | Stop-Process -Force`,
])
gh.close()
await sleep(500)
spawnSync('git', ['worktree', 'remove', '--force', oldTree], { cwd: REPO, stdio: 'inherit' })
rmSync(scratch, { recursive: true, force: true })

console.log(failures ? `\n${failures} check(s) failed` : '\nall checks passed')
process.exit(failures ? 1 : 0)
