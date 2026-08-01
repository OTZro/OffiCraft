import { useEffect, useState } from "react";
import { useI18n } from "../i18n";
import { type OnboardingReportView } from "../api";
import {
  loadServerSettings,
  refreshServerSettings,
} from "../hooks/sharedServerSettings";
import "./onboarding.css";

/**
 * OnboardingBanner (T-ba62) — the ONE place a fresh install can read WHY it is
 * not working.
 *
 * After the owner sets the initial password the server automatically installs
 * this host's warden and wakes the seeded assistant. When that succeeds the
 * cockpit needs no banner: a live assistant IS the signal. When it does NOT,
 * everything the owner would otherwise see is a grey member and an offline
 * machine — the exact silence this ticket exists to remove. So the banner
 * renders ONLY for a non-ok report, and it leads with the step's REASON.
 *
 * Deliberately NOT rendered for `state === "running"`: a run in progress is not
 * a problem, and a banner that appears and then disappears on its own trains
 * the owner to ignore it.
 *
 * Dismissal is per-session (sessionStorage): the underlying condition is
 * durable, so a permanent dismissal would hide a still-broken studio forever,
 * while re-nagging on every render would be noise.
 */
const DISMISS_KEY = "oc.onboarding.dismissed";

/** Poll cadence + ceiling for the non-terminal states (see the effect below). */
export const ONBOARDING_POLL_MS = 3000;
export const ONBOARDING_POLL_CEILING_MS = 180000;

/** Terminal states: once the report reads one of these it will never change.
 *
 * 🔴 `null` IS TERMINAL — and that is HALF OF A PAIRED CONTRACT with the server
 * (T-8115). Do not change one side without the other.
 *
 *   THIS SIDE  a settings read whose `onboarding` is null means onboarding never
 *              ran and never will, so there is nothing to wait for.
 *   THAT SIDE  server/ocserverd/onboarding.go `kickFirstRunOnboardingWith`
 *              PERSISTS the `running` report SYNCHRONOUSLY — before it returns,
 *              therefore before the POST /api/auth/set-password handler returns,
 *              therefore before that 200 can reach any client. Every one of its
 *              early returns (no DAL / OC_NO_ONBOARDING=1 / a GetSetting error /
 *              a failed claim write) means the run does not happen AT ALL, and
 *              it is never retried: set-password is one-shot. So a null a client
 *              can observe is always the permanent kind.
 *              Pinned by TestSetPasswordLeavesNoNullOnboardingWindow and
 *              TestOnboardingClaimIsPersistedBeforeKickReturns.
 *
 * WHY IT MATTERS: the DTO declares null NORMAL ("Null on the settings read when
 * onboarding never ran (an install that predates it, or a database that already
 * had a password)") and the production install reads null forever. Treating it
 * as "not written yet" made every cockpit open poll a 639 kB payload ~61 times
 * over three minutes for a row nothing will ever write.
 *
 * ⚠️ `running` is STILL non-terminal, which is the whole point: the fresh-install
 * verdict lands ~30 s in (wardenOnlineWait) and the poll must be there for it.
 * This is not a one-shot fetch. */
function isTerminal(report: OnboardingReportView | null): boolean {
  if (report === null) return true;
  return report.state === "ok" || report.state === "failed";
}

export function OnboardingBanner() {
  const { t } = useI18n();
  const [report, setReport] = useState<OnboardingReportView | null>(null);
  const [dismissed, setDismissed] = useState(
    () => sessionStorage.getItem(DISMISS_KEY) === "1"
  );
  const [showDetail, setShowDetail] = useState(false);

  // POLL until the report reaches a terminal state.
  //
  // 🔴 WHY THIS IS NOT A ONE-SHOT FETCH (it was, and that made the banner
  // useless in the only situation it exists for). The real timeline is:
  //
  //   t=0     owner submits the password → 200 → cockpit mounts → this fetch
  //           reads state="running" → correctly draws nothing.
  //           (It CANNOT read null here: the server claims the report row
  //           before that 200 is written — see isTerminal above. The comment
  //           that used to say "or null: the report row may not be written yet"
  //           was wrong, and T-8115 removed the 60 wasted polls it justified.)
  //   t=0..30 the server installs the warden and waits for its SSE connect
  //   t≈30    the report lands as "failed"
  //
  // A mount-only fetch never learns about that last line. The one loud channel
  // this whole change adds would stay silent in exactly the case it was built
  // for, and the owner — who has just set a password and is staring at an empty
  // cockpit — would have to guess that a page reload might reveal something.
  //
  // The poll stops on a terminal state, and gives up after a ceiling so a
  // wedged report cannot leave a tab polling forever.
  useEffect(() => {
    let live = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const started = Date.now();
    let first = true;

    const tick = async () => {
      try {
        // The FIRST read joins the shared cockpit-load snapshot (one
        // /api/settings for the whole page); every later poll must be a real
        // read — a poll answered from a cache would watch its own memory.
        const wasFirst = first;
        first = false;
        const settings = wasFirst
          ? await loadServerSettings()
          : await refreshServerSettings();
        if (!live) return;
        setReport(settings.onboarding);
        if (isTerminal(settings.onboarding)) return; // done — stop polling
      } catch {
        // A settings read that fails is NOT evidence about onboarding — stay
        // silent rather than assert anything we do not know, and keep polling:
        // a transient blip during first-run boot is expected.
      }
      if (!live || Date.now() - started >= ONBOARDING_POLL_CEILING_MS) return;
      timer = setTimeout(() => void tick(), ONBOARDING_POLL_MS);
    };
    void tick();

    return () => {
      live = false;
      if (timer !== undefined) clearTimeout(timer);
    };
  }, []);

  if (dismissed || !report || report.state !== "failed") return null;
  const failed = report.steps.filter((s) => !s.ok);
  if (failed.length === 0) return null;

  const stepLabel = (name: string) =>
    name === "install_warden"
      ? t.onboarding.stepInstallWarden
      : name === "wake_assistant"
        ? t.onboarding.stepWakeAssistant
        : name;

  return (
    <div className="onboarding-banner" role="status" data-testid="onboarding-banner">
      <div className="onboarding-banner__head">
        <strong>{t.onboarding.titleFailed}</strong>
        <button
          type="button"
          className="onboarding-banner__dismiss"
          data-testid="onboarding-dismiss"
          onClick={() => {
            sessionStorage.setItem(DISMISS_KEY, "1");
            setDismissed(true);
          }}
        >
          {t.onboarding.dismiss}
        </button>
      </div>
      <p className="onboarding-banner__intro">{t.onboarding.intro}</p>
      <ul className="onboarding-banner__steps">
        {failed.map((s) => (
          <li key={s.name} data-testid={`onboarding-step-${s.name}`}>
            <span className="onboarding-banner__step">{stepLabel(s.name)}</span>
            {/* The REASON is the payload — a step name alone is the same
                silence with a label on it. */}
            <span className="onboarding-banner__reason">{s.reason}</span>
          </li>
        ))}
      </ul>
      {failed.some((s) => s.detail !== "") && (
        <>
          <button
            type="button"
            className="onboarding-banner__toggle"
            data-testid="onboarding-detail-toggle"
            onClick={() => setShowDetail((v) => !v)}
          >
            {showDetail ? t.onboarding.detailHide : t.onboarding.detailShow}
          </button>
          {showDetail && (
            <pre className="onboarding-banner__detail" data-testid="onboarding-detail">
              {failed
                .filter((s) => s.detail !== "")
                .map((s) => s.detail)
                .join("\n\n")}
            </pre>
          )}
        </>
      )}
    </div>
  );
}
