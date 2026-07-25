/* Web Push is deliberately network-only: the cockpit contains live owner data
 * and must not become a stale offline cache.  The worker only receives push
 * payloads and routes the owner to the relevant hash view after a tap. */
self.addEventListener("push", (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch {
    data = {};
  }
  const title = data.title || "OffiCraft";
  const options = {
    body: data.body || "你有新的通知。",
    icon: "/icon-192.png",
    badge: "/icon-192.png",
    tag: data.reply_card_id || data.chat_id || "officraft",
    data,
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const data = event.notification.data || {};
  const hash = data.reply_card_id
    ? `#replies/card/${encodeURIComponent(data.reply_card_id)}`
    : data.chat_id && data.chat_peer_id
      ? `#office/chat/${encodeURIComponent(data.chat_peer_id)}/msg/${encodeURIComponent(data.chat_id)}`
      : "#office";
  event.waitUntil(
    clients.matchAll({ type: "window", includeUncontrolled: true }).then((windows) => {
      const existing = windows[0];
      if (existing) {
        existing.focus();
        return existing.navigate(`/${hash}`);
      }
      return clients.openWindow(`/${hash}`);
    }),
  );
});
