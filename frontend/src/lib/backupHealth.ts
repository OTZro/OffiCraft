// lib/backupHealth.ts — the ONE derivation from a backup-health answer to what
// the cockpit says about it (T-da06).
//
// Two surfaces read `GET /api/backup-health`: the always-mounted topbar
// indicator and the monitor page's 備份健康 card. They must never disagree, so
// the mapping "(verdict, did the fetch fail?) → visual state / wording" lives
// here exactly once — the same discipline as `presenceVisual` for the presence
// dot.
//
// 🔴 The floor is `unknown`, and `unknown` is NOT a quieter `healthy`. A load
// that failed, a load still in flight, and a watchdog that has not evaluated
// are all "we cannot tell". This whole ticket exists because a studio with no
// retreat point looked exactly like one that had it — so nothing here is ever
// allowed to fall through to the green state.

import type { BackupHealthView, BackupHealthStatus } from "../types";
import type { Dict } from "../i18n/locales/zh";

/** The i18n subtree both surfaces read. Typed off the canonical dictionary so
 * a renamed key is a compile error, not a blank string at runtime. */
type BackupDict = Dict["backupHealth"];

/**
 * The visual state to render: the server's verdict, or `unknown` whenever we do
 * not have one (still loading, or the fetch rejected). Never `healthy` by
 * default.
 */
export function backupIndicatorState(
  health: BackupHealthView | null,
  error: boolean,
): BackupHealthStatus {
  if (error || !health) return "unknown";
  return health.status;
}

/** The short status headline (also the indicator's accessible name). */
export function backupStatusLabel(
  d: BackupDict,
  state: BackupHealthStatus,
): string {
  if (state === "healthy") return d.statusHealthy;
  if (state === "unhealthy") return d.statusUnhealthy;
  return d.statusUnknown;
}

/**
 * WHY it reads the way it does — the PRIMARY user-facing sentence, derived from
 * the closed `code` vocabulary via i18n.
 *
 * The server's `detail` is deliberately not used here: it is an English
 * engineer-facing diagnostic string, so making it the sentence the owner reads
 * would leave one of the two shipped languages untranslated and would let a
 * server-side wording change silently rewrite the UI. `detail` is still shown,
 * as clearly-labelled SECONDARY text.
 *
 * Returns "" when healthy — a healthy backup has no reason to explain, and an
 * empty string is the signal to render nothing rather than a filler sentence.
 */
export function backupReasonText(
  d: BackupDict,
  health: BackupHealthView | null,
  error: boolean,
): string {
  // The cockpit could not even ask — distinct from the server telling us it has
  // not evaluated, and the owner can act on the difference.
  if (error || !health) return d.reasonUnavailable;
  if (health.status === "unknown") return d.reasonUnknown;
  if (health.status === "healthy") return "";
  if (health.code === "never_ran") return d.reasonNeverRan;
  if (health.code === "stale") return d.reasonStale;
  if (health.code === "failed") return d.reasonFailed;
  // Unhealthy with no code the cockpit recognises: still red (the status
  // carries the alarm), and honest that we cannot name the failure. The
  // server's `detail` alongside is what says more.
  return d.reasonUnknown;
}
