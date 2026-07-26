import { useEffect, useState } from "react";
import { api } from "../api";
import type { PushSubscriptionInput } from "../api/adapter";
import { useI18n } from "../i18n";
import { BellIcon, BellOffIcon } from "./icons";

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
  const [errorMessage, setErrorMessage] = useState("");

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
        const settings = await api.getServerSettings();
        // An old browser subscription alone is not a delivery target: without
        // a configured VAPID contact email the server deliberately skips it.
        if (!settings.pushContactEmail) return setState("default");
        await api.savePushSubscription(subscriptionInput(subscription));
        setState("enabled");
      }).catch(() => setState("error"));
    }).catch(() => setState("unsupported"));
  }, []);

  async function enable() {
    if (!registration) return;
    try {
      // A valid VAPID contact identity is required for iPhone delivery. Check
      // before opening the browser permission prompt so the owner knows what
      // to fix instead of granting a permission that cannot receive anything.
      const settings = await api.getServerSettings();
      if (!settings.pushContactEmail) {
        setErrorMessage(t.notifications.contactRequired);
        setState("error");
        window.alert(t.notifications.contactRequired);
        return;
      }
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
      setErrorMessage(t.notifications.failed);
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

  // This belongs with the other compact global controls, not as a page-wide
  // banner. The icon shows the current state: a crossed bell means this device
  // is not subscribed, while a plain bell means notifications are enabled.
  if (state === "unsupported" || state === "denied") return null;
  const enabled = state === "enabled";
  const label = enabled ? t.notifications.disable : t.notifications.enable;
  const title = state === "error" ? errorMessage || t.notifications.failed : label;
  return (
    <button
      className={`icon-btn${enabled ? " icon-btn--active" : ""}`}
      type="button"
      aria-label={label}
      title={title}
      aria-pressed={enabled}
      onClick={enabled ? disable : enable}
    >
      {enabled ? <BellIcon size={16} /> : <BellOffIcon size={16} />}
    </button>
  );
}
