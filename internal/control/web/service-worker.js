const CACHE_NAME = 'proxyfleet-shell-v5'
const APP_SHELL = [
  '/',
  '/styles.css',
  '/setup.css',
  '/app.js',
  '/i18n.js',
  '/manifest.webmanifest',
  '/logo.png',
  '/favicon.png',
  '/icon-192.png',
  '/icon-512.png',
]

self.addEventListener('install', event => {
  event.waitUntil(caches.open(CACHE_NAME).then(cache => cache.addAll(APP_SHELL)).then(() => self.skipWaiting()))
})

self.addEventListener('activate', event => {
  event.waitUntil(
    caches.keys()
      .then(keys => Promise.all(keys.filter(key => key !== CACHE_NAME).map(key => caches.delete(key))))
      .then(() => self.clients.claim()),
  )
})

self.addEventListener('fetch', event => {
  if (event.request.method !== 'GET' || new URL(event.request.url).origin !== self.location.origin) return
  if (event.request.mode === 'navigate') {
    event.respondWith(fetch(event.request).catch(() => caches.match('/')))
    return
  }
  event.respondWith(
    fetch(event.request).then(response => {
      if (response.ok && !event.request.url.includes('/api/')) {
        const copy = response.clone()
        caches.open(CACHE_NAME).then(cache => cache.put(event.request, copy))
      }
      return response
    }).catch(() => caches.match(event.request)),
  )
})

self.addEventListener('push', event => {
  let payload = { title: 'ProxyFleet', body: 'Состояние инфраструктуры изменилось', url: '/events' }
  try {
    if (event.data) payload = { ...payload, ...event.data.json() }
  } catch (_) {
    if (event.data) payload.body = event.data.text()
  }
  event.waitUntil(self.registration.showNotification(payload.title, {
    body: payload.body,
    icon: '/icon-192.png',
    badge: '/favicon.png',
    tag: payload.tag || 'proxyfleet',
    renotify: true,
    data: { url: payload.url || '/events' },
  }))
})

self.addEventListener('notificationclick', event => {
  event.notification.close()
  const target = new URL(event.notification.data?.url || '/', self.location.origin).href
  event.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(clients => {
    const existing = clients.find(client => client.url.startsWith(self.location.origin))
    if (existing) return existing.navigate(target).then(client => client.focus())
    return self.clients.openWindow(target)
  }))
})
