// Yoru's worker: pushed alerts, a page for when Yoru can't be reached, and episodes saved on this device.
const OFFLINE = 'yoru-offline-1'
// Kept across updates: saved episodes, and a copy of the app per build to play them in.
const SAVED = 'yoru-saved'
const SHELL = 'yoru-shell-'

self.addEventListener('install', (event) => {
  self.skipWaiting()
  // Without the copy the worker still does alerts; the browser's own error page shows instead.
  event.waitUntil(caches.open(OFFLINE).then((c) => c.add('/offline.html')).catch(() => {}))
})
self.addEventListener('activate', (event) =>
  event.waitUntil(
    caches
      .keys()
      .then((keys) => keys.filter((k) => k !== OFFLINE && k !== SAVED && !k.startsWith(SHELL)))
      .then((old) => Promise.all(old.map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  ),
)

self.addEventListener('fetch', (event) => {
  const req = event.request
  const url = new URL(req.url)
  if (url.origin !== self.location.origin || req.method !== 'GET') return
  if (url.pathname.startsWith('/api/stream/saved-')) return event.respondWith(saved(req))
  if (req.mode === 'navigate') return event.respondWith(page(req, url))
  if (url.pathname.startsWith('/assets/')) event.respondWith(asset(req))
})

async function saved(req) {
  const hit = await caches.match(req, { cacheName: SAVED, ignoreSearch: true })
  return hit || new Response('{"error":"not saved on this device"}', { status: 404, headers: { 'Content-Type': 'application/json' } })
}

async function shell() {
  for (const name of (await caches.keys()).filter((k) => k.startsWith(SHELL)).reverse()) {
    const cache = await caches.open(name)
    if (await cache.match('/__ready')) return cache
  }
  return null
}

// Named by their contents, so the kept file is the same one; a PC asleep never holds it up.
async function asset(req) {
  const cache = await shell()
  return (cache && (await cache.match(req, { ignoreSearch: true }))) || fetch(req)
}

async function page(req, url) {
  const app = await shell()
  if (app && url.searchParams.has('offline')) return (await app.match('/')) || Response.error()
  const ctl = new AbortController()
  const timer = app ? setTimeout(() => ctl.abort(), 3000) : 0
  try {
    return await (app ? fetch(req, { signal: ctl.signal }) : fetch(req))
  } catch {
    if (!app) return (await caches.match('/offline.html', { cacheName: OFFLINE })) || Response.error()
    return Response.redirect(`${url.pathname.startsWith('/saved') ? url.pathname : '/saved'}?offline=1`, 302)
  } finally {
    clearTimeout(timer)
  }
}

// The server sends the declarative form ({ web_push: 8030, notification: {...} }), which Safari can show by
// itself; here it is shown for the browsers that hand it to the worker.
self.addEventListener('push', (event) => {
  let data = {}
  try {
    data = event.data ? event.data.json() : {}
  } catch {
    data = { notification: { title: 'Yoru', body: event.data ? event.data.text() : '' } }
  }
  const n = data.notification || {}
  // Yoru's own part: the show's picture, and the buttons acting on its show or episode.
  const y = data.yoru || {}
  const shown = self.registration.showNotification(n.title || 'Yoru', {
    body: n.body,
    tag: n.tag,
    lang: n.lang,
    icon: '/icon-192.png',
    image: y.image || undefined,
    actions: (y.actions || []).filter((a) => ACTIONS[a]).map((a) => ({ action: a, title: ACTIONS[a].title })),
    data: { url: n.navigate || '/', animeId: y.animeId, episode: y.episode },
  })
  const badge = Number(n.app_badge)
  const badged = badge > 0 && self.navigator.setAppBadge ? self.navigator.setAppBadge(badge).catch(() => {}) : null
  event.waitUntil(Promise.all([shown, badged]))
})

// An alert's buttons: what each asks of Yoru, and how the result is said.
const ACTIONS = {
  download: {
    title: 'Download',
    path: '/api/download',
    body: (d) => ({ animeId: d.animeId, episode: d.episode }),
    done: (d) => `Episode ${d.episode} is downloading`,
  },
  upnext: {
    title: 'Up next',
    path: '/api/upnext',
    body: (d) => ({ animeId: d.animeId, episode: d.episode }),
    done: (d) => `Episode ${d.episode} is in Up next`,
  },
  follow: {
    title: 'Follow',
    // Not /api/follow: this also lifts alerts turned off for the show, as the button promises.
    path: (d) => `/api/anime/${d.animeId}/notify`,
    body: () => ({ on: true }),
    done: () => 'Following: new episodes will alert you',
  },
  plan: {
    title: 'Add to Planning',
    path: '/api/status',
    body: (d) => ({ animeId: d.animeId, status: 'PLANNING' }),
    done: () => 'Added to Planning',
  },
}

// A button pressed: done without opening Yoru, then said in a short alert of its own.
async function act(name, d, tag) {
  const a = ACTIONS[name]
  let text
  try {
    const res = await fetch(typeof a.path === 'function' ? a.path(d) : a.path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(a.body(d)),
    })
    if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error || `HTTP ${res.status}`)
    text = a.done(d)
  } catch (e) {
    text = `Couldn't ${a.title.toLowerCase()}: ${e.message}`
  }
  return self.registration.showNotification('Yoru', { body: text, tag, icon: '/icon-192.png' })
}

// A tap opens the episode: in a Yoru window already open on this address, or a new one.
self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const d = event.notification.data || {}
  if (ACTIONS[event.action] && d.animeId) {
    event.waitUntil(act(event.action, d, event.notification.tag))
    return
  }
  const url = new URL(d.url || '/', self.location.origin).href
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windows) => {
      const open = windows.find((w) => new URL(w.url).origin === new URL(url).origin)
      if (!open) return self.clients.openWindow(url)
      return open.navigate(url).then((w) => (w || open).focus(), () => open.focus())
    }),
  )
})
