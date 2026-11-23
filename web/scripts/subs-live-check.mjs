import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
const SHOW = process.env.YORU_SHOW ?? ''
const EPISODES = (process.env.EPISODES ?? '1').split(',').map(Number)

const get = async (path) => {
  const res = await fetch(`${BASE}${path}`)
  return res.ok ? res.json() : { error: `${res.status} ${await res.text()}` }
}

let id = Number(process.env.YORU_SHOW_ID ?? 0)
if (!id) {
  const found = await get(`/api/browse?q=${encodeURIComponent(SHOW)}`)
  const first = found.items?.[0]
  console.log('search:', (found.items ?? []).slice(0, 5).map((i) => `${i.id} ${i.title} (${i.format})`))
  id = first?.id
}
if (!id) {
  console.log('FAIL show not found')
  process.exit(1)
}

for (const ep of EPISODES) {
  const sources = await get(`/api/episode/sources?id=${id}&episode=${ep}`)
  console.log(`\n=== episode ${ep}: ${(sources.results ?? []).length} releases`)
  for (const r of (sources.results ?? []).slice(0, 6)) {
    console.log(
      `  ${r.autoPick ? '*' : ' '} ${r.Torrent.title}\n      subs=${JSON.stringify(r.Release.Subtitles ?? [])} dual=${!!r.Release.DualAudio} blocked=${r.blocked ?? ''} seeders=${r.Torrent.seeders}`,
    )
  }
}

await fetch(`${BASE}/api/prefs`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ key: 'playback.autoplay', value: 'true' }),
})

const dialogueOf = async (streamId, track) => {
  const res = await fetch(`${BASE}/api/stream/${streamId}/subtitle/${track}`)
  const body = res.ok ? await res.text() : ''
  return {
    status: res.status,
    complete: res.headers.get('x-yoru-complete'),
    dialogue: (body.match(/^Dialogue:/gm) ?? []).length,
    sample: (body.match(/^Dialogue:.*$/m)?.[0] ?? '').slice(0, 120),
  }
}

const browser = await chromium.launch({ args: ['--autoplay-policy=no-user-gesture-required'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

for (const ep of EPISODES) {
  let stream
  let play
  let opens = 0
  const subtitleFetches = []
  const onResponse = async (res) => {
    const url = res.url()
    try {
      if (url.includes('/api/stream/open') && url.includes(`episode=${ep}&`)) {
        opens++
        stream = await res.json()
      }
      else if (url.endsWith('/api/play')) play = await res.json()
      else if (url.includes('/subtitle/')) {
        const body = await res.text()
        const nearPlayhead = (body.match(/^Dialogue: \d+,0:0[345]:\d\d/gm) ?? []).length
        subtitleFetches.push({
          status: res.status(),
          complete: res.headers()['x-yoru-complete'],
          dialogue: (body.match(/^Dialogue:/gm) ?? []).length,
          nearPlayhead,
        })
      }
    } catch {
    }
  }
  page.on('response', onResponse)
  await page.goto(`${BASE}/watch/${id}/${ep}`, { waitUntil: 'domcontentloaded' })

  const deadline = Date.now() + 8 * 60_000
  while (!stream && Date.now() < deadline) await page.waitForTimeout(2000)
  console.log(`\n=== playing episode ${ep}`)
  console.log('  release:', play?.title ?? play?.error ?? '(none)')
  console.log('  stream:', stream?.id, 'duration', stream?.duration, 'opens seen', opens)
  console.log('  audio:', JSON.stringify(stream?.audio?.map((a) => `${a.index}:${a.language ?? '?'}:${a.title ?? ''}`)))
  console.log('  subtitle tracks:', JSON.stringify(stream?.subtitles?.map((s) => `${s.index}:${s.language ?? '?'}:${s.title ?? ''}`) ?? []))
  if (!stream) console.log('  page says:', (await page.locator('body').innerText()).slice(0, 400))

  if (stream) {
    const english = stream.subtitles?.find((s) => /^(en|eng)$/i.test(s.language ?? '')) ?? stream.subtitles?.[0]
    if (english) {
      let extracted = await dialogueOf(stream.id, english.index)
      for (let i = 0; i < 24 && extracted.dialogue === 0; i++) {
        await page.waitForTimeout(5000)
        extracted = await dialogueOf(stream.id, english.index)
      }
      console.log(`  extracted track ${english.index}:`, JSON.stringify(extracted))
    }
  }

  const started = Date.now()
  while (Date.now() - started < 4 * 60_000) {
    const t = await page.evaluate(() => document.querySelector('video')?.currentTime ?? 0)
    if (t > 3) break
    await page.waitForTimeout(2000)
  }
  await page.evaluate(() => {
    const v = document.querySelector('video')
    if (v && v.duration > 300) v.currentTime = 240
  })
  await page.waitForTimeout(40_000)
  const state = await page.evaluate(() => {
    const v = document.querySelector('video')
    return {
      t: v?.currentTime,
      paused: v?.paused,
      canvases: document.querySelectorAll('canvas').length,
      error: v?.error?.code ?? null,
    }
  })
  console.log('  player:', JSON.stringify(state))
  console.log('  subtitle fetches:', JSON.stringify(subtitleFetches.slice(-3)))
  await page.screenshot({ path: `${SHOTS}/subs-live-ep${ep}.png` })
  page.off('response', onResponse)
}

console.log('\npage errors:', errors.length ? errors.join(' | ') : 'none')
await browser.close()
