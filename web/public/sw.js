// Yoru's alerts worker: shows pushed alerts and opens the episode they name. It has no fetch handler, so it
// never stands between the app and the network.
self.addEventListener('install', () => self.skipWaiting())
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()))

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
  event.waitUntil(
    self.registration.showNotification(n.title || 'Yoru', {
      body: n.body,
      tag: n.tag,
      lang: n.lang,
      icon: '/icon-192.png',
      data: { url: n.navigate || '/' },
    }),
  )
})

// A tap opens the episode: in a Yoru window already open on this address, or a new one.
self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const url = new URL((event.notification.data && event.notification.data.url) || '/', self.location.origin).href
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((windows) => {
      const open = windows.find((w) => new URL(w.url).origin === new URL(url).origin)
      if (!open) return self.clients.openWindow(url)
      return open.navigate(url).then((w) => (w || open).focus(), () => open.focus())
    }),
  )
})
