// Real builds update themselves: the release to this build, then this build again with GitHub's API limited.
//   YORU_REPO=D:\yoru node scripts/update-check.mjs
import { spawn, spawnSync } from 'node:child_process'
import { createHash, generateKeyPairSync, sign } from 'node:crypto'
import { createServer } from 'node:http'
import { cpSync, mkdirSync, readFileSync, rmSync, writeFileSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { chromium } from 'playwright'

const REPO = process.env.YORU_REPO ?? join(process.cwd(), '..')
// The last release: what people have installed.
const RELEASED_TAG = process.env.YORU_TAG ?? 'v2026.10.17'
const PORT = 4398
const BASE = `http://127.0.0.1:${PORT}`
const OLD = '1.0.0'
const MID = '1.0.0.1'
const NEW = '1.0.1'
const NEWER = '1.0.2'
const NOTES = {
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

// ---------------------------------------------------------------- build
// One fixed folder: Windows Firewall adds rules for each new program path that listens, and alerts each time.
const scratch = join(tmpdir(), 'yoru-update-check')
rmSync(scratch, { recursive: true, force: true })
const root = join(scratch, 'app')
const data = join(scratch, 'data')
const release = join(scratch, 'release')
const oldTree = join(scratch, 'released-src')
for (const d of [root, data, release]) mkdirSync(d, { recursive: true })

// The updater refuses an unsigned release, so these are signed by a key only these test builds trust.
const { publicKey, privateKey } = generateKeyPairSync('ed25519')
const trusted = Buffer.from(publicKey.export({ format: 'jwk' }).x, 'base64url').toString('hex')
const build = (dir, version, out) =>
  spawnSync(
    'go',
    // -H windowsgui as package.ps1 builds them: a windowed app updating itself, with no console.
    ['build', '-trimpath', '-ldflags', `-s -w -H windowsgui -X yoru/internal/update.Version=${version} -X yoru/internal/update.signingKeyHex=${trusted}`,
      '-o', out, './cmd/yoru'],
    { cwd: dir, stdio: 'inherit', env: { ...process.env, CGO_ENABLED: '0' } },
  )

// The released Yoru, from its tag; it embeds web/dist, which the tag doesn't carry, so this tree's is lent.
const added = spawnSync('git', ['worktree', 'add', '--detach', oldTree, RELEASED_TAG], { cwd: REPO, stdio: 'inherit' })
check(added.status === 0, `checked out Yoru ${RELEASED_TAG}`)
cpSync(join(REPO, 'web', 'dist'), join(oldTree, 'web', 'dist'), { recursive: true })
check(build(oldTree, OLD, join(root, 'yoru.exe')).status === 0, `built Yoru ${OLD} (the release)`)
check(build(REPO, NEW, join(release, 'new.exe')).status === 0, `built Yoru ${NEW}`)
check(build(REPO, NEWER, join(release, 'newer.exe')).status === 0, `built Yoru ${NEWER}`)

// As package.ps1 zips it.
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
const releases = { [NEW]: pack(NEW, 'new.exe'), [NEWER]: pack(NEWER, 'newer.exe') }
let latest = releases[NEW]
let listed = [NEW, MID, OLD]
// Spent: the API answers 403 for the hour, and only github.com's pages say what's out.
let limited = false

// ---------------------------------------------------------------- fake github
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
    // Every release, so one several versions behind reads all of them; drafts and betas are nobody's.
    res.setHeader('content-type', 'application/json')
    res.end(
      JSON.stringify([
        { tag_name: 'v9.0.0', body: 'a draft', draft: true },
        { tag_name: 'v1.0.9', body: 'a beta', prerelease: true },
        ...listed.map((v) => ({ tag_name: `v${v}`, body: NOTES[v], published_at: '2026-10-16T10:00:00Z' })),
      ]),
    )
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

// ---------------------------------------------------------------- run the released Yoru
// Its own peer port: the default is the real app's.
writeFileSync(join(root, 'config.toml'), `addr = "127.0.0.1:${PORT}"\n\n[torrent]\nlisten_port = 4343\nupnp = false\n`)
const env = {
  ...process.env,
  YORU_ROOT: root,
  // APPDATA too, so nothing of this user's own is read.
  LOCALAPPDATA: data,
  APPDATA: data,
  YORU_UPDATE_API: ghBase(),
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
// Something of the user's, to find again after the update.
const kept = await post('/api/prefs', { key: 'playback.autonext', value: 'true' })
check(kept.ok, 'a setting saved')

// The startup check runs as a job; give it a moment, then force one anyway.
await sleep(500)
const checked = await post('/api/update/check')
check(checked.ok && checked.body.available && checked.body.latest?.version === NEW, 'the release sees the update', JSON.stringify(checked.body))
const versions = (list) => (list ?? []).map((n) => n.version).join(',')
check(versions(checked.body.pending) === `${NEW},${MID}`, 'every release since this one is offered, newest first', versions(checked.body.pending))

// ---------------------------------------------------------------- release -> this build
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
const page = await browser.newPage()
await page.goto(`${BASE}/settings?tab=About`, { waitUntil: 'domcontentloaded' })
// This bare install has no ffmpeg, so the first load is sent to Setup once; then About within the app.
await page.waitForURL(/\/setup/, { timeout: 5000 }).catch(() => {})
await page.evaluate(() => {
  history.pushState(null, '', '/settings?tab=About')
  dispatchEvent(new PopStateEvent('popstate'))
})
const whatsNew = page.getByRole('heading', { name: `What's new since ${OLD}` })
const shown = await whatsNew.waitFor({ timeout: 15000 }).then(() => true, () => false)
check(shown, "the About tab says what's new since the old version")
// Skipped when absent, so the teardown below still runs.
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
await browser.close()

// ---------------------------------------------------------------- this build -> newer, API limited
latest = releases[NEWER]
listed = [NEWER, NEW, MID, OLD]
limited = true
const again = await post('/api/update/check')
check(again.ok && again.body.available && again.body.latest?.version === NEWER && !again.body.checkError,
  `with the API limited, Yoru still sees ${NEWER} from github.com's pages`, JSON.stringify(again.body))
const second = await post('/api/update/apply')
check(second.status === 202, 'second apply accepted', JSON.stringify(second.body))
check(await waitVersion(NEWER, 60), `Yoru ${NEWER} answering`)
for (let i = 0; i < 40 && existsSync(yoruExe + '.old'); i++) await sleep(250)
check(readFileSync(yoruExe).equals(readFileSync(join(release, 'newer.exe'))), 'yoru.exe is the newer program')
check(!existsSync(yoruExe + '.old') && !existsSync(yoruExe + '.new'), 'no leftovers from the second update')

if (failures) console.log(oldLog.slice(-2000))

// ---------------------------------------------------------------- teardown
// The new process broke away from ours; stop it by path so a real one survives.
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
