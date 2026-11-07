import { spawn } from 'node:child_process'
import { createWriteStream, mkdirSync, rmSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join, resolve } from 'node:path'
import { tmpdir } from 'node:os'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const webDir = resolve(scriptDir, '..')
const repo = resolve(webDir, '..')
const logs = join(tmpdir(), 'yoru-release-check')

const ALONE = true

const CHECKS = [
  ['player-check.mjs'],
  ['features-check.mjs', {}, ALONE],
  ['recovery-check.mjs'],
  ['thumbs-check.mjs'],
  ['prefs-scope-check.mjs'],
  ['profile-menu-check.mjs'],
  ['about-folders-check.mjs'],
  ['alerts-check.mjs'],
  ['anime4k-tiers-check.mjs', {}, ALONE],
  ['autodelete-check.mjs', { YORU_EXTRA_CONFIG: '' }, ALONE],
  ['autonext-seek-check.mjs'],
  ['backup-check.mjs'],
  ['browse-filters-check.mjs', {}, ALONE],
  ['browse-paging-check.mjs', {}, ALONE],
  ['cast-check.mjs'],
  ['change-release-check.mjs'],
  ['continue-next-check.mjs'],
  ['downloads-fresh-check.mjs'],
  ['downloads-groups-check.mjs'],
  ['downloads-pack-check.mjs'],
  ['downloads-order-check.mjs'],
  ['episode-download-check.mjs'],
  ['fs-autonext-check.mjs'],
  ['fullscreen-check.mjs'],
  ['home-rails-check.mjs', {}, ALONE],
  ['host-check.mjs', {}, ALONE],
  ['hover-panel-check.mjs', {}, ALONE],
  ['keys-a11y-check.mjs'],
  ['layout-bits-check.mjs', {}, ALONE],
  ['phone-check.mjs'],
  ['subtitles-check.mjs'],
  ['subs-start-check.mjs', { YORU_FONTS: '1' }],
  ['decode-check.mjs', { YORU_HEVC: '1' }],
  ['network-check.mjs'],
  ['list-state-check.mjs', {}, ALONE],
  ['lossless-audio-check.mjs'],
  ['no-transcoder-check.mjs', { BIN_ONLY: 'none' }],
  ['notices-check.mjs', {}, ALONE],
  ['pair-check.mjs'],
  ['airplay-check.mjs'],
  ['picks-check.mjs', {}, ALONE],
  ['people-check.mjs', {}, ALONE],
  ['miniplayer-check.mjs'],
  ['together-check.mjs', { YORU_TOGETHER_LOCAL: '1' }],
  ['offline-check.mjs'],
  ['over-budget-note-check.mjs'],
  ['pip-check.mjs'],
  ['prefetch-check.mjs', { YORU_EXTRA_CONFIG: '' }, ALONE],
  ['related-check.mjs', {}, ALONE],
  ['releases-check.mjs', {}, ALONE],
  ['saved-copy-check.mjs', {}, ALONE],
  ['seek-spinner-check.mjs'],
  ['segmented-scroll-check.mjs'],
  ['settings-check.mjs'],
  ['setup-transcoder-check.mjs', { BIN_ONLY: 'ffmpeg' }],
  ['shortcuts-check.mjs'],
  ['sites-check.mjs'],
  ['skips-check.mjs'],
  ['show-groups-check.mjs'],
  ['starting-note-check.mjs'],
  ['studio-check.mjs', {}, ALONE],
  ['subs-recovery-check.mjs', {}, ALONE],
  ['subtitle-look-check.mjs'],
  ['tour-check.mjs', {}, ALONE],
  ['ui-bits-check.mjs'],
  ['ui-shared-check.mjs'],
  ['unconvertible-check.mjs'],
  ['vlc-check.mjs'],
  ['watching-check.mjs'],
  ['wifi-drop-check.mjs'],
  ['exit-log-check.mjs'],
  ['sound-check.mjs'],
]

const only = process.argv.slice(2)
const checks = only.length ? only.map((name) => CHECKS.find(([c]) => c === name) ?? [name]) : CHECKS
const LANES = Number(process.env.LANES ?? 4)
const failed = []

const step = (name, cmd, cwd) =>
  new Promise((done) => {
    console.log(`\n▶ ${name}`)
    const child = spawn(cmd, [], { cwd, shell: true, stdio: ['ignore', 'pipe', 'pipe'] })
    const log = createWriteStream(join(logs, `${name.replaceAll(' ', '-')}.log`))
    for (const out of [child.stdout, child.stderr]) {
      out.on('data', (chunk) => {
        process.stdout.write(chunk)
        log.write(chunk)
      })
    }
    child.on('close', (code) => {
      log.end()
      if (code !== 0) failed.push(name)
      done(code === 0)
    })
  })

function runCheck([name, env = {}], port) {
  return new Promise((done) => {
    const started = Date.now()
    const child = spawn(process.execPath, [join(scriptDir, 'run-player-check.mjs'), name], {
      cwd: webDir,
      env: { ...process.env, ...env, PORT: String(port), SKIP_BUILD: '1' },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    const log = createWriteStream(join(logs, name.replace(/\.mjs$/, '.log')))
    child.stdout.pipe(log)
    child.stderr.pipe(log)
    child.on('close', (code) => {
      const secs = Math.round((Date.now() - started) / 1000)
      console.log(`  ${code === 0 ? '✓' : '✗'} ${name} (${secs}s)`)
      if (code !== 0) failed.push(name)
      done()
    })
  })
}

rmSync(logs, { recursive: true, force: true })
mkdirSync(logs, { recursive: true })

if (!only.length && process.env.SKIP_GO !== '1') (await step('go vet', 'go vet ./...', repo)) && (await step('go test', 'go test ./...', repo))
if (!only.length) await step('format helpers', 'node scripts/format-check.mjs', webDir)
if (!only.length) await step('release notes', 'node scripts/notes-check.mjs', webDir)

let built = process.env.SKIP_BUILD === '1'
if (!built) {
  const exe = join(tmpdir(), 'yoru-e2e-bin', process.platform === 'win32' ? 'yoru.exe' : 'yoru')
  const gui = process.platform === 'win32' ? '-ldflags "-H windowsgui" ' : ''
  built = (await step('web build', 'npm run build', webDir)) && (await step('server build', `go build ${gui}-o "${exe}" ./cmd/yoru`, repo))
}

if (built && !only.length) await step('self-update', 'node scripts/update-check.mjs', webDir)

if (built) {
  console.log(`\n▶ ${checks.length} checks (logs: ${logs})`)
  const alone = checks.filter(([, , a]) => a)
  const rest = checks.filter(([, , a]) => !a)
  const lane = async (queue, port) => {
    for (let next = queue.shift(); next; next = queue.shift()) await runCheck(next, port)
  }
  await Promise.all([lane(alone, 4400), ...Array.from({ length: LANES }, (_, i) => lane(rest, 4410 + 10 * i))])
}

console.log(failed.length ? `\n✗ failed: ${failed.join(', ')}` : '\n✓ all passed')
process.exitCode = failed.length ? 1 : 0
