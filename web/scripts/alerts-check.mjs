// Alerts with Yoru closed: on subscribes, the worker shows a pushed alert for the episode, off unsubscribes.
// Only the browser's subscribe is faked (a test browser has no push service).
import { chromium } from 'playwright'

const BASE = process.env.YORU_URL ?? 'http://127.0.0.1:4399'
const SHOTS = process.env.SHOTS ?? '.'
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

const sw = await fetch(`${BASE}/sw.js`)
check(sw.ok && /javascript/.test(sw.headers.get('content-type') ?? '') && sw.headers.get('cache-control') === 'no-cache',
  'the alerts worker is served as script, never cached long', `${sw.status} ${sw.headers.get('content-type')} ${sw.headers.get('cache-control')}`)

// Full Chromium, not the headless shell, which reports notifications as blocked whatever is granted.
const browser = await chromium.launch({ channel: 'chromium' })
const context = await browser.newContext({ viewport: { width: 1280, height: 900 } })
await context.grantPermissions(['notifications'], { origin: BASE })
// A subscription as Chrome's would look, kept per page load in sessionStorage so "on" survives a reload.
await context.addInitScript(() => {
  const key = 'fake-push'
  const make = (raw) => ({
    endpoint: raw.endpoint,
    options: { applicationServerKey: Uint8Array.from(raw.key).buffer },
    toJSON: () => ({ endpoint: raw.endpoint, keys: raw.keys }),
    unsubscribe: async () => (sessionStorage.removeItem(key), true),
  })
  PushManager.prototype.getSubscription = async function () {
    const raw = sessionStorage.getItem(key)
    return raw ? make(JSON.parse(raw)) : null
  }
  PushManager.prototype.subscribe = async function (opts) {
    const b64 = (bytes) => btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
    const raw = {
      endpoint: 'https://fcm.googleapis.com/fcm/send/alerts-check',
      key: Array.from(new Uint8Array(opts.applicationServerKey)),
      keys: { p256dh: b64(new Uint8Array([4, ...new Array(64).fill(7)])), auth: b64(new Uint8Array(16).fill(9)) },
    }
    sessionStorage.setItem(key, JSON.stringify(raw))
    return make(raw)
  }
})
const page = await context.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(e.message.slice(0, 200)))
const calls = []
page.on('request', (r) => {
  if (r.url().includes('/api/push/')) calls.push(new URL(r.url()).pathname)
})

try {
  await page.goto(`${BASE}/settings?tab=Notifications`, { waitUntil: 'domcontentloaded' })
  const toggle = page.getByRole('switch', { name: 'Alerts on this device' })
  check(await until(() => toggle.isVisible()), 'Notifications has one switch for alerts on this device')
  const seen = await page.evaluate(() => ({
    secure: window.isSecureContext, worker: 'serviceWorker' in navigator, push: 'PushManager' in window,
    permission: typeof Notification === 'undefined' ? 'none' : Notification.permission,
  }))
  const row = page.locator('[data-row]', { hasText: 'Alerts on this device' })
  check(await until(() => row.getByText(/even with Yoru closed/).isVisible()), 'it says alerts come with Yoru closed',
    `${JSON.stringify(seen)} ${(await row.innerText()).replace(/\n/g, ' | ')}`)
  await toggle.click()
  check(await until(async () => calls.includes('/api/push/subscribe')), 'turning it on subscribes this device with Yoru')
  check(await until(() => toggle.isChecked()), 'and the switch reads on')
  check(await until(() => page.getByRole('button', { name: 'Send a test' }).isVisible()), 'a test can be sent to this device')
  const worker = await page.evaluate(async () => (await navigator.serviceWorker.getRegistration())?.active?.scriptURL ?? '')
  check(worker.endsWith('/sw.js'), 'the alerts worker is running', worker)
  await page.screenshot({ path: `${SHOTS}/alerts-settings.png` })

  // A push as Yoru sends it, delivered to the real worker: it shows the alert with the episode to open.
  const cdp = await context.newCDPSession(page)
  let registrationId
  cdp.on('ServiceWorker.workerRegistrationUpdated', (e) => {
    for (const reg of e.registrations) if (!reg.isDeleted) registrationId = reg.registrationId
  })
  await cdp.send('ServiceWorker.enable')
  await until(async () => !!registrationId)
  await cdp.send('ServiceWorker.deliverPushMessage', {
    origin: new URL(BASE).origin,
    registrationId,
    data: JSON.stringify({
      web_push: 8030,
      notification: { title: 'Alerts Check Show', body: 'Episode 3 is available', tag: 'yoru-1', navigate: `${BASE}/watch/1/3` },
    }),
  })
  let shown = []
  await until(async () => {
    shown = await page.evaluate(async () =>
      (await (await navigator.serviceWorker.getRegistration()).getNotifications()).map((n) => ({ title: n.title, body: n.body, tag: n.tag, url: n.data?.url })))
    return shown.length > 0
  })
  check(shown[0]?.title === 'Alerts Check Show' && shown[0]?.tag === 'yoru-1' && shown[0]?.url === `${BASE}/watch/1/3`,
    'a pushed alert shows the show, and opens the episode when tapped', JSON.stringify(shown))

  await toggle.click()
  check(await until(async () => calls.includes('/api/push/unsubscribe')), 'turning it off unsubscribes this device')
  check(await until(async () => !(await toggle.isChecked())), 'and the switch reads off')
  check(errors.length === 0, 'no page errors', errors.join(' | '))
} finally {
  await browser.close()
}
console.log(failures === 0 ? '\nall checks passed' : `\n${failures} check(s) failed`)
process.exit(failures === 0 ? 0 : 1)
