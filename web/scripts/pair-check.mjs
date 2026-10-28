// Pairing a TV with a code, from Settings, Devices: the address it opens (no token), the code typed any way,
// what a wrong one says, and the device a right one let in. The TV's own page is in Go's pairing_test.
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const until = async (fn, ms = 8000) => {
  for (const end = Date.now() + ms; Date.now() < end; await sleep(150)) if (await fn().catch(() => false)) return true
  return false
}

let failures = 0
const check = (ok, label, detail = '') => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`)
  if (!ok) failures++
}

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))

try {
  // Reachable from the home network, as the row needs.
  await page.route('**/api/access', (route) => route.fulfill({ json: {
    reachable: true, canSwitch: true, host: true, listening: '0.0.0.0:4410', urls: ['http://192.168.1.10:4410/?token=abc123'],
  } }))
  const asked = []
  let answer = { status: 404, json: { error: 'No device is showing that code. Codes last 10 minutes; reload the TV\'s page for a new one.' } }
  await page.route('**/api/pair', (route) => {
    asked.push(route.request().postDataJSON())
    return route.fulfill(answer)
  })
  await page.goto(`${BASE}/settings?tab=Devices`, { waitUntil: 'domcontentloaded' })

  const field = page.getByRole('textbox', { name: 'The code the device shows' })
  const pair = page.getByRole('button', { name: 'Pair', exact: true })
  check(await until(() => field.isVisible(), 15_000), 'Devices has a field for the code a TV shows')
  check(await page.getByText('On a TV or console, open http://192.168.1.10:4410: it shows a code to type here.').isVisible(),
    'naming the address to open there, without the token')
  await field.fill('k7q')
  check(await pair.isDisabled(), 'Pair waits for all six characters')
  await field.fill('k7q 2mx')
  check((await field.inputValue()) === 'K7Q 2MX' && (await pair.isEnabled()), 'typed in small letters, it reads in capitals',
    await field.inputValue())

  await pair.click()
  check(await until(() => page.getByText(/No device is showing that code/).isVisible()), 'a code nobody shows says so')
  check(asked[0]?.code === 'K7Q 2MX', 'the code goes as typed; the server reads it without the space', JSON.stringify(asked[0]))

  answer = { status: 200, json: { paired: 'Chrome on Android' } }
  await pair.click()
  check(await until(() => page.getByText('Paired Chrome on Android.').isVisible()), 'a right one names the device it let in')
  check((await field.inputValue()) === '', 'and the field is empty for the next')
  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures ? `\n${failures} FAILED` : '\nall passed')
process.exit(failures ? 1 : 0)
