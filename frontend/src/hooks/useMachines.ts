// hooks/useMachines.ts — load the machine registry (GET /api/machines) through
// the api client + keep it fresh. Mirrors useMonitoring: reconcile-by-refetch on
// any SSE topic mentioning machines or monitoring. The machine picker + the
// Monitor machines panel read this; `online` is honest (never fabricated).

import { useCallback, useEffect, useRef, useState } from "react";
import type { MachineView } from "../types";
import { api } from "../api";

interface UseMachines {
  machines: MachineView[];
  loading: boolean;
  /** True when the mount fetch REJECTED (non-401; 401 bounces to login at the
   * http layer). Lets the UI distinguish a failed load from honest-empty. */
  error: boolean;
  refetch: () => Promise<void>;
}

export function useMachines(opts?: { refreshSeconds?: number }): UseMachines {
  const refreshSeconds = opts?.refreshSeconds ?? 5;
  const [machines, setMachines] = useState<MachineView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);
  const requestVersion = useRef(0);

  const refetch = useCallback(async () => {
    // Manual refreshes share the same generation as event-driven refreshes.
    // They may overlap a request already in flight, but only the newest result
    // is allowed to update the view.
    const version = ++requestVersion.current;
    const next = await api.listMachines();
    if (version === requestVersion.current) {
      setMachines(next);
      setError(false);
    }
  }, []);

  useEffect(() => {
    let alive = true;

    let timer: ReturnType<typeof setTimeout> | null = null;
    let lastStarted = 0;
    let trailing = false;
    let inFlight = false;
    const schedule = () => {
      if (timer || !trailing) return;
      const delay = Math.max(0, refreshSeconds * 1000 - (Date.now() - lastStarted));
      timer = setTimeout(() => {
        timer = null;
        if (!alive || !trailing || inFlight) return;
        refresh().catch((e) => console.warn("useMachines: SSE refetch failed", e));
      }, delay);
    };
    const refresh = () => {
      if (inFlight) return Promise.resolve();
      timer = null;
      trailing = false;
      inFlight = true;
      lastStarted = Date.now();
      const version = ++requestVersion.current;
      return api.listMachines()
      .then((next) => {
        if (alive && version === requestVersion.current) {
          setMachines(next);
          setError(false);
        }
      })
      .catch((e) => {
        console.warn("useMachines: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => {
        inFlight = false;
        if (alive) setLoading(false);
        schedule();
      });
    };

    refresh()
      .catch((e) => {
        console.warn("useMachines: initial load failed", e);
        if (alive) setError(true);
      })
      .finally(() => { if (alive) setLoading(false); });

    // A registry mutation must reconcile immediately: it is normally the
    // consequence of a user action (install, uninstall, upgrade) and drives
    // the action-row state. Telemetry/member chatter, on the other hand, is
    // bursty and uses the coalesced trailing path below.
    const unsubscribe = api.subscribeEvents((topic) => {
      if (topic.includes("machine")) {
        refetch().catch((e) => console.warn("useMachines: registry refetch failed", e));
      } else if (topic.includes("monitor") || topic.includes("member")) {
        // T-10. This branch used to open with `requestVersion.current += 1`.
        // That bump CANCELLED every request already in flight while issuing
        // NONE of its own, so the only thing left to repair the view was the
        // trailing refresh below — up to `refreshSeconds` (5s) later.
        //
        // It fired on its own action. POST /api/machines publishes a `member`
        // frame (api_machines.go → putMember → hub.Publish), and the stream
        // loop drains the hub on a 250ms tick (api_infra.go ssePoll), so that
        // frame lands uniformly at random within 250ms of the POST — often
        // right on top of the reconciling GET that MonitorPage's onboard is
        // awaiting. Measured in a browser with the GET held 400ms: frame at
        // +127ms, the GET's own (correct, 12-machine) answer DROPPED at
        // +404ms, the row only surfacing 3.9s later off the trailing poll.
        // MonitorPage collapsed the inline row anyway, because a discarded
        // refetch still RESOLVES.
        //
        // An event frame does not make an in-flight response wrong — it only
        // means a NEWER one may exist. Discarding it strands the view on older
        // data with nothing outstanding to fix it. So: do not invalidate.
        // Let the in-flight request land on its own version, and schedule the
        // coalesced follow-up (bursty telemetry still collapses into one).
        //
        // Ordering between real requests is UNAFFECTED: every actual request —
        // manual `refetch` and the effect's `refresh` alike — still takes a
        // version from this same counter, so whichever was ISSUED last is the
        // only one allowed to call setMachines, whatever order they resolve in.
        // That guarantee is what `does not let an older manual refresh
        // overwrite a later event refresh` in useMachines.test.ts exists for,
        // and it survives here; what does not survive is cancelling a request
        // on behalf of an event that issues no replacement.
        trailing = true;
        schedule();
      }
    });

    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
      unsubscribe();
    };
  }, [refreshSeconds, refetch]);

  return { machines, loading, error, refetch };
}
