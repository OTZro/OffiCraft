import { useEffect, useState } from "react";
import { api } from "../api";
import type { PushSubscriptionInput } from "../api/adapter";
import { useI18n } from "../i18n";
import "./push-notifications.css";

type PushState = "unsupported" | "default" | "enabled" | "denied" | "error";

function base64Url(bytes: ArrayBuffer): string {
  const raw = String.fromCharCode(...new Uint8Array(bytes));
  return btoa(raw).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function vapidKey(value: string): ArrayBuffer {
  const padded = value.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(value.length / 4) * 4, "=");
  const raw = atob(padded);
  const bytes = Uint8Array.from(raw, (char) => char.charCodeAt(0));
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
}

function subscriptionInput(subscription: PushSubscription): PushSubscriptionInput {
  const p256dh = subscription.getKey("p256dh");
  const auth = subscription.getKey("auth");
  if (!p256dh || !auth) throw new Error("subscription keys missing");
  return {
    endpoint: subscription.endpoint,
    expirationTime: subscription.expirationTime,
    keys: { p256dh: base64Url(p256dh), auth: base64Url(auth) },
  };
}

/** Registers the worker once and only prompts after the owner's explicit tap.
 * This also covers installed PWAs on iOS 16.4+ where Web Push is available. */
export function PushNotifications() {
  const { t } = useI18n();
  const [state, setState] = useState<PushState>("unsupported");
  const [registration, setRegistration] = useState<ServiceWorkerRegistration | null>(null);

  useEffect(() => {
    if (!("serviceWorker" in navigator) || !("PushManager" in window) || !("Notification" in window)) return;
    navigator.serviceWorker.register("/sw.js").then((worker) => {
      setRegistration(worker);
      // Permission and a server-side delivery target are different things. A
      // database restore can lose the latter while the browser keeps the
      // subscription, so reconcile it idempotently before claiming success.
      void worker.pushManager.getSubscription().then(async (subscription) => {
        if (Notification.permission === "denied") return setState("denied");
        if (Notification.permission !== "granted" || !subscription) return setState("default");
        await api.savePushSubscription(subscriptionInput(subscription));
        setState("enabled");
      }).catch(() => setState("error"));
    }).catch(() => setState("unsupported"));
  }, []);

  async function enable() {
    if (!registration) return;
    try {
      const permission = await Notification.requestPermission();
      if (permission !== "granted") {
        setState(permission === "denied" ? "denied" : "default");
        return;
      }
      const key = await api.getPushPublicKey();
      const subscription = await registration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: vapidKey(key) });
      await api.savePushSubscription(subscriptionInput(subscription));
      setState("enabled");
    } catch {
      setState("error");
    }
  }

  async function disable() {
    if (!registration) return;
    try {
      const subscription = await registration.pushManager.getSubscription();
      if (subscription) {
        await api.removePushSubscription(subscription.endpoint);
        await subscription.unsubscribe();
      }
      setState(Notification.permission === "denied" ? "denied" : "default");
    } catch {
      setState("error");
    }
  }

  if (state === "unsupported") return null;
  const message = state === "enabled" ? t.notifications.enabled : state === "denied" ? t.notifications.denied : state === "error" ? t.notifications.failed : t.notifications.description;
  return <section className="push-notice" aria-live="polite"><div className="push-notice__copy"><strong>{t.notifications.title}</strong><span>{message}</span></div>{state === "enabled" ? <button type="button" className="push-notice__button push-notice__button--quiet" onClick={disable}>{t.notifications.disable}</button> : state !== "denied" ? <button type="button" className="push-notice__button" onClick={enable}>{t.notifications.enable}</button> : null}</section>;
}
