/// <reference lib="webworker" />
import { clientsClaim } from 'workbox-core'
import { cleanupOutdatedCaches, precacheAndRoute } from 'workbox-precaching'

declare let self: ServiceWorkerGlobalScope

interface PushPayload {
  title?: string
  body?: string
  url?: string
  tag?: string
}

self.skipWaiting()
clientsClaim()
cleanupOutdatedCaches()
// App shell only. /api/* is never cached: it is not in the precache manifest and no runtime route matches it.
precacheAndRoute(self.__WB_MANIFEST)

self.addEventListener('push', (event) => {
  let payload: PushPayload = {}
  try {
    payload = (event.data?.json() as PushPayload) ?? {}
  } catch {
    payload = { body: event.data?.text() ?? '' }
  }
  const title = payload.title ?? 'Подписка'
  event.waitUntil(
    self.registration
      .showNotification(title, {
        body: payload.body ?? '',
        tag: payload.tag,
        icon: '/icons/icon-192.png',
        badge: '/icons/icon-192.png',
        data: { url: payload.url ?? '/' },
      })
      // Only after the notification is up, and never at its expense.
      .then(sendReceipt),
  )
})

/**
 * Tells the server this device really received a message. The push service
 * cannot: Apple answers "accepted" even for a web app that was removed from the
 * home screen, so without receipts dead subscriptions pile up and "delivered"
 * counts are wrong. Best effort: a failed receipt must never surface.
 */
async function sendReceipt(): Promise<void> {
  try {
    const sub = await self.registration.pushManager.getSubscription()
    if (!sub) return
    await fetch('/api/push/receipt', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ endpoint: sub.endpoint }),
      credentials: 'omit', // the endpoint is the credential; no session needed
      keepalive: true,
    })
  } catch {
    // Offline or server down: the next message will try again.
  }
}

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const raw = (event.notification.data as { url?: string } | undefined)?.url ?? '/'
  // The server sends full links, so a notification still opens the right place
  // after the site moves to another address.
  const target = new URL(raw, self.location.origin)
  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
      // Reuse an open window only for a link on this same site; a link to
      // another address gets a window of its own.
      if (target.origin === self.location.origin) {
        for (const client of list) {
          if ('focus' in client) {
            void client.navigate(target.href)
            return client.focus()
          }
        }
      }
      return self.clients.openWindow(target.href)
    }),
  )
})
