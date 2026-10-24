// Yoru's worker: shows pushed alerts, opens the episode they name, and says why when a page can't load at all.
// Nothing is served from a copy: every request goes to the network; only a failed page load is answered.
const OFFLINE = 'yoru-offline-1'

self.addEventListener('install', (event) => {
  self.skipWaiting()
  // Without the copy the worker still does alerts; the browser's own error page shows instead.
  event.waitUntil(caches.open(OFFLINE).then((c) => c.add('/offline.html')).catch(() => {}))
})
self.addEventListener('activate', (event) =>
  event.waitUntil(
    caches
      .keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== OFFLINE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim()),
  ),
)

// Page loads only: the installed app has no address bar or reload, so a dead network gets words and a retry.
self.addEventListener('fetch', (event) => {
  if (event.request.mode !== 'navigate') return
  event.respondWith(
    fetch(event.request).catch(() => caches.match('/offline.html', { cacheName: OFFLINE }).then((r) => r || Response.error())),
  )
})

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
