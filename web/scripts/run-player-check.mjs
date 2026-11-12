import { spawn, spawnSync, execFileSync } from 'node:child_process'
import { mkdirSync, rmSync, readFileSync, writeFileSync, existsSync, openSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { basename, delimiter, dirname, join, resolve } from 'node:path'
import { tmpdir } from 'node:os'
import { linkBin } from './harness-lib.mjs'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const webDir = resolve(scriptDir, '..')
const repo = resolve(webDir, '..')

const PORT = process.env.PORT ?? '4399'
const CHECK = process.argv[2] ?? process.env.CHECK ?? 'player-check.mjs'
const ANIME = process.env.YORU_ANIME ?? '127230'
const SKIP_BUILD = process.env.SKIP_BUILD === '1'
const KEEP = process.env.KEEP === '1'
const URL = `http://127.0.0.1:${PORT}`

const exe = (name) => (process.platform === 'win32' ? `${name}.exe` : name)
const ffmpeg = join(repo, 'bin', exe('ffmpeg'))
const DEFAULT_PORT = PORT === '4399'
const usual = join(tmpdir(), DEFAULT_PORT ? 'yoru-e2e' : `yoru-e2e-${PORT}`)
const scratch = (() => {
  for (const old of readdirSync(tmpdir()).filter((n) => n.startsWith(`${basename(usual)}-run-`))) {
    try {
      rmSync(join(tmpdir(), old), { recursive: true, force: true })
    } catch {
    }
  }
  try {
    rmSync(usual, { recursive: true, force: true, maxRetries: 6, retryDelay: 500 })
    return usual
  } catch {
    return `${usual}-run-${Date.now()}`
  }
})()
const PEER_PORT = DEFAULT_PORT ? 4341 : Number(PORT) - 60
const root = join(scratch, 'root')
const appdata = join(scratch, 'appdata')
const isolated = {
  YORU_ROOT: root,
  LOCALAPPDATA: appdata,
  APPDATA: appdata,
  YORU_NO_WINDOW: '1',
  YORU_UPDATE_API: process.env.YORU_UPDATE_API ?? 'http://127.0.0.1:9',
}
const lib = join(scratch, 'lib')
const shots = join(scratch, 'shots')
const serverLog = join(scratch, 'server.log')
const binCache = process.env.YORU_BIN_DIR ?? join(tmpdir(), 'yoru-e2e-bin')
const yoruExe = join(binCache, exe('yoru'))

let server
let anilist
const ANILIST_PORT = Number(PORT) + 1000

const stage = (msg) => console.log(`\n▶ ${msg}`)
const run = (cmd, args, opts = {}) => {
  const r = spawnSync(cmd, args, { stdio: 'inherit', ...opts })
  if (r.status !== 0) throw new Error(`${cmd} ${args.join(' ')} exited ${r.status ?? r.signal}`)
}

function binOnlyPath() {
  if (!process.env.BIN_ONLY) return {}
  const keep = process.env.BIN_ONLY.split(',')
  const hidden = ['ffmpeg', 'ffprobe', 'mpv', 'vlc'].filter((n) => !keep.includes(n))
  const key = Object.keys(process.env).find((k) => k.toLowerCase() === 'path') ?? 'PATH'
  const dirs = (process.env[key] ?? '').split(delimiter)
  return { [key]: dirs.filter((d) => !hidden.some((n) => existsSync(join(d, exe(n))))).join(delimiter) }
}

function killTree(child) {
  if (!child || child.exitCode !== null) return
  try {
    if (process.platform === 'win32') {
      execFileSync('taskkill', ['/PID', String(child.pid), '/T', '/F'], { stdio: 'ignore' })
    } else {
      process.kill(-child.pid, 'SIGKILL')
    }
  } catch {
  }
}

function makeEpisode() {
  const cues = []
  for (let t = 2; t < 178; t += 5) {
    const a = new Date(t * 1000).toISOString().substr(11, 8)
    const b = new Date((t + 4) * 1000).toISOString().substr(11, 8)
    cues.push(`Dialogue: 0,${a}.00,${b}.00,Default,,0,0,0,,cue at ${t}s`)
  }
  const ass = `[Script Info]
ScriptType: v4.00+
PlayResX: 1280
PlayResY: 720

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,48,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,2,2,2,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
${cues.join('\n')}
`
  const assPath = join(scratch, 'subs.ass')
  writeFileSync(assPath, ass)
  const chapters = join(scratch, 'chapters.ini')
  writeFileSync(
    chapters,
    ';FFMETADATA1\n' +
      [[0, 10000, 'Scene 1'], [10000, 40000, 'Intro'], [40000, 160000, 'Scene 3'], [160000, 180000, 'Credits']]
        .map(([s, e, t]) => `[CHAPTER]\nTIMEBASE=1/1000\nSTART=${s}\nEND=${e}\ntitle=${t}\n`)
        .join(''),
  )

  const out = join(lib, 'Yoru Test Show - 01.mkv')
  run(ffmpeg, [
    '-y', '-hide_banner', '-loglevel', 'error',
    '-f', 'lavfi', '-i', 'testsrc2=size=1280x720:rate=24:duration=180',
    '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=180',
    '-f', 'lavfi', '-i', 'sine=frequency=880:sample_rate=48000:duration=180',
    '-i', assPath,
    '-i', chapters,
    '-map_metadata', '4',
    '-map', '0:v', '-map', '1:a', '-map', '2:a', '-map', '3:s',
    '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p', '-g', '48',
    '-c:a', 'aac', '-c:s', 'ass',
    '-metadata:s:a:0', 'language=jpn', '-disposition:a:0', 'default',
    '-metadata:s:a:1', 'language=eng',
    '-metadata:s:s:0', 'language=eng', '-disposition:s:0', 'default',
    out,
  ])
  makeMarkerEpisode()
  makePictureEpisode()
  if (process.env.YORU_HEVC === '1') makeHevcEpisode()
  if (process.env.YORU_FONTS === '1') makeFontEpisode()
  return out
}

function makeFontEpisode() {
  const dir = process.env.WINDIR ? join(process.env.WINDIR, 'Fonts') : ''
  const fonts = ['arial', 'arialbd', 'ariali', 'arialbi', 'verdana', 'verdanab', 'verdanai', 'verdanaz', 'trebuc',
    'trebucbd', 'trebucit', 'trebucbi', 'times', 'timesbd', 'timesi', 'timesbi', 'georgia', 'georgiab', 'georgiai',
    'georgiaz', 'tahoma', 'tahomabd', 'comic'].map((f) => join(dir, `${f}.ttf`))
  if (!dir || !fonts.every((f) => existsSync(f))) throw new Error('subs-start-check needs the Windows fonts to attach')
  const ass = readFileSync(join(scratch, 'markers.ass'), 'utf8')
    .replace(/^(Style: Default.*)$/m, '$1\nStyle: Words,Comic Sans MS,56,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,8,0,0,40,1')
    .concat('Dialogue: 0,0:00:00.00,0:03:00.00,Words,,0,0,0,,Yoru font check\n')
  const assPath = join(scratch, 'fonts.ass')
  writeFileSync(assPath, ass)
  run(ffmpeg, [
    '-y', '-hide_banner', '-loglevel', 'error',
    '-i', join(lib, 'Yoru Test Show - 03.mkv'),
    '-i', assPath,
    '-map', '0:v', '-map', '0:a', '-map', '1:s', '-c:v', 'copy', '-c:a', 'copy', '-c:s', 'ass',
    '-metadata:s:s:0', 'language=eng', '-disposition:s:0', 'default',
    ...fonts.flatMap((f, i) => ['-attach', f, `-metadata:s:t:${i}`, 'mimetype=application/x-truetype-font']),
    join(lib, 'Yoru Test Show - 07.mkv'),
  ])
}

function makeHevcEpisode() {
  const source = ['-y', '-hide_banner', '-loglevel', 'error',
    '-f', 'lavfi', '-i', 'testsrc2=size=1280x720:rate=24:duration=180',
    '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=180']
  run(ffmpeg, [...source,
    '-c:v', 'libx265', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p10le', '-b:v', '1200k',
    '-x265-params', 'log-level=error:keyint=48',
    '-c:a', 'ac3', '-ac', '6', '-b:a', '384k',
    join(lib, 'Yoru Test Show - 05.mkv'),
  ])
  run(ffmpeg, [...source,
    '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p10le', '-b:v', '1200k', '-g', '48',
    '-c:a', 'aac',
    join(lib, 'Yoru Test Show - 06.mkv'),
  ])
}

function makePictureEpisode() {
  const seg = (type, seconds, data) => {
    const head = Buffer.alloc(13)
    head.write('PG', 0, 'latin1')
    head.writeUInt32BE(Math.round(seconds * 90000), 2)
    head.writeUInt8(type, 10)
    head.writeUInt16BE(data.length, 11)
    return Buffer.concat([head, data])
  }
  const [W, H, x, y, w, h] = [1280, 720, 490, 600, 300, 60]
  const u16 = (n) => [n >> 8, n & 255]
  const display = (seconds, number, show) => {
    const pcs = Buffer.from([...u16(W), ...u16(H), 0x10, ...u16(number), show ? 0x80 : 0, 0, 0, show ? 1 : 0,
      ...(show ? [0, 0, 0, 0, ...u16(x), ...u16(y)] : [])])
    const wds = Buffer.from([1, 0, ...u16(x), ...u16(y), ...u16(w), ...u16(h)])
    const parts = [seg(0x16, seconds, pcs), seg(0x17, seconds, wds)]
    if (show) {
      parts.push(seg(0x14, seconds, Buffer.from([0, 0, 1, 235, 128, 128, 255])))
      const line = [0, 0xc0 | (w >> 8), w & 255, 1, 0, 0]
      const rle = Buffer.from(Array.from({ length: h }, () => line).flat())
      const size = rle.length + 4
      parts.push(seg(0x15, seconds, Buffer.concat([Buffer.from([0, 0, 0, 0xc0, size >> 16, (size >> 8) & 255, size & 255, ...u16(w), ...u16(h)]), rle])))
    }
    parts.push(seg(0x80, seconds, Buffer.alloc(0)))
    return Buffer.concat(parts)
  }
  const sup = join(scratch, 'boxes.sup')
  writeFileSync(sup, Buffer.concat([display(5, 0, true), display(175, 1, false)]))
  run(ffmpeg, [
    '-y', '-hide_banner', '-loglevel', 'error',
    '-f', 'lavfi', '-i', 'testsrc2=size=1280x720:rate=24:duration=180',
    '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=180',
    '-i', sup,
    '-map', '0:v', '-map', '1:a', '-map', '2:s',
    '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p', '-g', '48',
    '-c:a', 'aac', '-c:s', 'copy',
    '-metadata:s:s:0', 'language=eng', '-disposition:s:0', 'default',
    join(lib, 'Yoru Test Show - 04.mkv'),
  ])
}

function makeMarkerEpisode() {
  const cues = []
  for (let i = 0; i < 90; i++) {
    const at = (s) => new Date(s * 1000).toISOString().substr(11, 11).replace(/0$/, '')
    cues.push(
      `Dialogue: 0,${at(2 * i)},${at(2 * i + 1.9)},Default,,0,0,0,,{\\an7\\pos(${40 + 12 * i},300)\\p1}m 0 0 l 10 0 l 10 40 l 0 40{\\p0}`,
    )
  }
  const assPath = join(scratch, 'markers.ass')
  writeFileSync(
    assPath,
    `[Script Info]
ScriptType: v4.00+
PlayResX: 1280
PlayResY: 720

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,48,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,7,0,0,0,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
${cues.join('\n')}
`,
  )
  run(ffmpeg, [
    '-y', '-hide_banner', '-loglevel', 'error',
    '-f', 'lavfi', '-i', 'testsrc2=size=1280x720:rate=24:duration=180',
    '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=180',
    '-i', assPath,
    '-map', '0:v', '-map', '1:a', '-map', '2:s',
    '-c:v', 'libx264', '-preset', 'ultrafast', '-pix_fmt', 'yuv420p', '-g', '48',
    '-c:a', 'aac', '-c:s', 'ass',
    '-metadata:s:s:0', 'language=eng', '-disposition:s:0', 'default',
    join(lib, 'Yoru Test Show - 03.mkv'),
  ])
}

async function waitForServer() {
  for (let i = 0; i < 90; i++) {
    try {
      const res = await fetch(`${URL}/api/setup`)
      if (res.ok) return
    } catch {
    }
    if (server.exitCode !== null) {
      throw new Error(`server exited early (code ${server.exitCode}); see ${serverLog}`)
    }
    await new Promise((r) => setTimeout(r, 1000))
  }
  throw new Error(`server did not answer on ${URL} in time; see ${serverLog}`)
}

try {
  if (!existsSync(ffmpeg)) throw new Error(`ffmpeg not found at ${ffmpeg}; build/download deps first`)

  stage(`scratch instance at ${scratch}`)
  for (const d of [scratch, root, appdata, lib, shots]) mkdirSync(d, { recursive: true })
  linkBin(join(repo, 'bin'), join(root, 'bin'), process.env.BIN_ONLY ? process.env.BIN_ONLY.split(',') : null)
  writeFileSync(
    join(root, 'config.toml'),
    `addr = "127.0.0.1:${PORT}"\n${process.env.YORU_EXTRA_CONFIG ?? ''}\n[torrent]\nlisten_port = ${PEER_PORT}\nupnp = false\n\n[[indexer]]\ntype = "nyaa"\nurl = "http://127.0.0.1:${Number(PORT) + 2000}"\n`,
  )

  if (!SKIP_BUILD) {
    stage('building web (vite)')
    run('npm run build', [], { cwd: webDir, shell: true })
    stage('building server (go)')
    mkdirSync(binCache, { recursive: true })
    const gui = process.platform === 'win32' ? ['-ldflags', '-H windowsgui'] : []
    run('go', ['build', ...gui, '-o', yoruExe, './cmd/yoru'], { cwd: repo })
  }
  if (!existsSync(yoruExe)) throw new Error(`no Yoru binary at ${yoruExe}; run once without SKIP_BUILD`)

  stage('generating test episode')
  makeEpisode()

  const upstream = {}
  if (process.env.ANILIST_LIVE !== '1') {
    anilist = spawn(process.execPath, [join(scriptDir, 'anilist-cache.mjs'), String(ANILIST_PORT)], { stdio: 'ignore' })
    upstream.YORU_ANILIST_API = `http://127.0.0.1:${ANILIST_PORT}`
  }

  stage(`starting server on ${URL}`)
  const logFd = openSync(serverLog, 'w')
  server = spawn(yoruExe, [], {
    cwd: root,
    env: { ...process.env, ...isolated, ...upstream, ...binOnlyPath() },
    stdio: ['ignore', logFd, logFd],
    detached: process.platform !== 'win32',
  })
  await waitForServer()
  console.log('  server is up')

  stage(`running ${CHECK}`)
  const check = spawnSync(process.execPath, [join(scriptDir, CHECK)], {
    cwd: webDir,
    stdio: 'inherit',
    env: {
      ...process.env,
      YORU_URL: URL, YORU_LIB: lib, YORU_ANIME: ANIME, SHOTS: shots,
      YORU_DB: join(appdata, 'yoru', 'yoru.db'), YORU_REPO: repo, YORU_LOG: serverLog,
    },
  })
  process.exitCode = check.status ?? 1
} catch (err) {
  console.error(`\n✗ ${err.message}`)
  process.exitCode = 1
} finally {
  killTree(server)
  killTree(anilist)
  if (KEEP) {
    console.log(`\nscratch kept at ${scratch} (server log: ${serverLog})`)
  } else {
    try {
      rmSync(scratch, { recursive: true, force: true, maxRetries: 20, retryDelay: 250 })
    } catch (err) {
      console.warn(`\ncould not remove ${scratch}: ${err.message}`)
    }
  }
}
