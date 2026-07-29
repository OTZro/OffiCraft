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
        requestVersion.current += 1;
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
